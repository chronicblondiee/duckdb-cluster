# Feature Plan: Elasticsearch-Inspired Features for duckdb-cluster

## Executive Summary

Based on research into [Elasticsearch's search API](https://www.elastic.co/guide/en/elasticsearch/reference/current/search.html), the [go-elasticsearch client](https://github.com/elastic/go-elasticsearch), and [Grafana Loki's microservices architecture](https://grafana.com/docs/loki/latest/get-started/components/), this document recommends features to evolve duckdb-cluster from an MVP into a production-ready distributed OLAP system.

**Key Insights:**
- **Current bugs** in result merging (aggregations, ORDER BY, LIMIT) must be fixed first
- **Elasticsearch patterns** provide excellent API ergonomics: bulk operations, pagination, multi-query, introspection
- **Loki's architecture** offers a proven Go microservices blueprint: single-binary multi-mode deployment, gRPC transport, memberlist gossip, module system

---

## Current System Analysis

### What Works
- Hash-based write routing (FNV-1a) to target shards
- Fan-out read execution across all shards
- DDL broadcast to all shards
- Clean HTTP API with stdlib only
- 11 tests, all passing

### Critical Bugs
```57:73:internal/router/router.go
func (r *Router) handleRead(ctx context.Context, sqlStr string) (*QueryResult, error) {
	results, err := r.Manager.QueryAll(ctx, sqlStr)
	if err != nil {
		return nil, err
	}
	merged := MergeResults(results)
```

```1:9:internal/router/merger.go
func MergeResults(results [][]map[string]any) []map[string]any {
	var merged []map[string]any
	for _, r := range results {
		merged = append(merged, r...)
	}
	return merged
}
```

**Problems:**
1. **Aggregations return wrong results** — `SELECT COUNT(*) FROM users` on 3 shards returns 3 rows instead of 1
2. **ORDER BY ignored** — results concatenated without global sorting
3. **LIMIT/OFFSET per-shard** — `LIMIT 10` returns up to 30 rows (10 per shard)
4. **DISTINCT incomplete** — duplicates across shards not removed

---

## Recommended Features (Prioritized)

## Phase 1: Correctness First ⚠️ CRITICAL

### Fix Result Merging with DuckDB

**Problem:** Simple concatenation breaks SQL semantics for aggregations, ordering, and limits.

**Solution:** Use an in-memory DuckDB instance as a merge engine. After fan-out:
1. Create temp table with correct schema
2. Insert all shard results
3. Re-execute original SQL against merged data
4. Drop temp table

**Implementation:**
```go
// internal/router/merger.go
type MergeEngine struct {
    db *sql.DB  // In-memory DuckDB: ":memory:"
}

func (m *MergeEngine) Merge(ctx context.Context, sql string, shardResults []ShardResult) ([]map[string]any, error) {
    // 1. CREATE TEMP TABLE with schema from first result
    // 2. INSERT all rows from all shards
    // 3. Execute original SQL against temp table
    // 4. DROP TABLE
}
```

**Tests to add:**
- `TestMergeAggregations` — COUNT, SUM, AVG, MIN, MAX
- `TestMergeGroupBy` — GROUP BY with aggregations
- `TestMergeOrderBy` — ORDER BY single/multiple columns ASC/DESC
- `TestMergeLimit` — LIMIT, OFFSET, combined
- `TestMergeDistinct` — SELECT DISTINCT across shards

**Files modified:**
- `internal/router/merger.go` — complete rewrite
- `internal/router/router.go` — add merge engine to Router struct
- `internal/shard/shard.go` — extend Query() to return column types

**Estimated complexity:** Medium (2-3 hours)

---

## Phase 2: Elasticsearch-Inspired API Features

Based on [Elasticsearch's API patterns](https://www.elastic.co/docs/reference/elasticsearch/clients/go) and [pagination methods](https://opster.com/guides/elasticsearch/how-tos/elasticsearch-pagination-techniques/).

### 2.1 Bulk Operations

**Motivation:** Reduce HTTP round-trips for batch inserts. Elasticsearch's [`_bulk` API](https://www.elastic.co/guide/en/elasticsearch/reference/current/docs-bulk.html) is one of its most-used features.

**Endpoint:** `POST /bulk`

**Request:**
```json
{
  "statements": [
    {"sql": "INSERT INTO users VALUES (1, 'alice')", "partition_key": "1"},
    {"sql": "INSERT INTO users VALUES (2, 'bob')", "partition_key": "2"},
    {"sql": "INSERT INTO products VALUES (100, 'widget')", "partition_key": "100"}
  ]
}
```

**Response:**
```json
{
  "succeeded": 3,
  "failed": 0,
  "took_ms": 45,
  "results": [
    {"shard": 0, "rows_affected": 1, "success": true},
    {"shard": 1, "rows_affected": 1, "success": true},
    {"shard": 2, "rows_affected": 1, "success": true}
  ]
}
```

**Implementation notes:**
- Group statements by target shard (using HashRoute)
- Execute each group in a transaction for atomicity
- Parallelize across shards with goroutines

**Files:**
- `internal/api/handlers.go` — add `handleBulk`
- `internal/api/server.go` — register route
- `internal/router/router.go` — add `RouteBulk([]Statement) → BulkResult`

**Tests:** `TestBulkInsert`, `TestBulkMixedTables`, `TestBulkPartialFailure`

---

### 2.2 Pagination

**Motivation:** [From/size pagination](https://www.luigisbox.com/blog/elasticsearch-pagination/) is the simplest form of result control.

**Enhancement to existing `/query`:**
```json
{
  "sql": "SELECT * FROM users",
  "offset": 20,
  "limit": 10
}
```

**Implementation:**
- Apply AFTER merge (not injected into SQL to avoid injection risks)
- Simple slice operation: `results[offset : offset+limit]`

**Files:**
- `internal/api/handlers.go` — extend `queryRequest` struct, apply slicing

**Tests:** `TestPaginationOffset`, `TestPaginationLimit`, `TestPaginationBeyondResults`

---

### 2.3 Multi-Query

**Motivation:** Dashboard-style UIs often need multiple metrics simultaneously. Elasticsearch's [`_msearch`](https://www.elastic.co/guide/en/elasticsearch/reference/8.15/search-multi-search.html) reduces latency vs sequential requests.

**Endpoint:** `POST /multi-query`

**Request:**
```json
{
  "queries": [
    {"sql": "SELECT COUNT(*) FROM users"},
    {"sql": "SELECT AVG(price) FROM products"},
    {"sql": "SELECT * FROM orders WHERE user_id = $1", "partition_key": "user-123"}
  ]
}
```

**Response:**
```json
{
  "results": [
    {"success": true, "columns": ["count"], "rows": [{"count": 1523}]},
    {"success": true, "columns": ["avg"], "rows": [{"avg": 45.67}]},
    {"success": true, "columns": ["id", "user_id", "total"], "rows": [...]}
  ],
  "took_ms": 89
}
```

**Implementation:**
- Execute all queries concurrently with goroutines
- Use `sync.WaitGroup` + error collection
- Each query uses existing `Router.Route()`

**Files:**
- `internal/api/handlers.go` — add `handleMultiQuery`

**Tests:** `TestMultiQuery`, `TestMultiQueryPartialFailure`

---

### 2.4 Table Introspection

**Motivation:** Elasticsearch exposes [index mappings](https://www.elastic.co/guide/en/elasticsearch/reference/current/indices-get-mapping.html). Users need to discover schema.

**Endpoints:**
- `GET /admin/tables` — list all tables
- `GET /admin/tables/{name}` — get schema for specific table

**Response for `/admin/tables`:**
```json
{
  "tables": ["users", "products", "orders", "logs"]
}
```

**Response for `/admin/tables/users`:**
```json
{
  "name": "users",
  "columns": [
    {"name": "id", "type": "INTEGER", "nullable": false},
    {"name": "email", "type": "VARCHAR", "nullable": false},
    {"name": "created_at", "type": "TIMESTAMP", "nullable": true}
  ]
}
```

**Implementation:**
- Query `information_schema.tables` and `information_schema.columns` on shard 0
- DuckDB's information_schema is standard across all shards

**Files:**
- `internal/api/handlers.go` — add `handleListTables`, `handleTableSchema`
- `internal/shard/manager.go` — add `GetTableSchema(tableName) → Schema`

**Tests:** `TestListTables`, `TestTableSchema`, `TestTableSchemaNotFound`

---

### 2.5 Enhanced Health/Stats

**Motivation:** Observability. Elasticsearch provides [detailed cluster health](https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-health.html).

**Endpoints:**
- `GET /health` — improved with per-shard status
- `GET /admin/stats` — detailed statistics

**Response for `/health`:**
```json
{
  "status": "healthy",
  "shards": [
    {"id": 0, "status": "open", "responsive": true, "latency_ms": 2},
    {"id": 1, "status": "open", "responsive": true, "latency_ms": 3},
    {"id": 2, "status": "open", "responsive": true, "latency_ms": 1}
  ]
}
```

**Response for `/admin/stats`:**
```json
{
  "cluster": {
    "total_shards": 3,
    "total_tables": 4,
    "total_rows": 1523000
  },
  "shards": [
    {"id": 0, "path": "./data/shard-0.duckdb", "size_mb": 245, "table_count": 4, "row_count": 508000},
    {"id": 1, "path": "./data/shard-1.duckdb", "size_mb": 238, "table_count": 4, "row_count": 502000},
    {"id": 2, "path": "./data/shard-2.duckdb", "size_mb": 250, "table_count": 4, "row_count": 513000}
  ]
}
```

**Implementation:**
- Health: ping each shard with `SELECT 1`, measure latency
- Stats: query `SELECT COUNT(*) FROM table_name` per table, `os.Stat()` for file size

**Files:**
- `internal/api/handlers.go` — enhance `handleHealth`, add `handleStats`
- `internal/shard/manager.go` — add `GetStats() → ClusterStats`

**Tests:** `TestHealthWithShardDown`, `TestStats`

---

## Phase 3: Loki-Inspired Microservices Architecture

Based on [Loki's component architecture](https://grafana.com/docs/loki/latest/get-started/components/) and [configuration system](https://grafana.com/docs/loki/latest/configure/).

### 3.1 Module System

**Goal:** Same binary runs different components based on config, like Loki's `-target` flag.

**Module interface:**
```go
// internal/module/module.go
type Module interface {
    Name() string
    Dependencies() []string
    Init(cfg *config.Config) error
    Start(ctx context.Context) error
    Stop() error
}

type Manager struct {
    modules map[string]Module
    running map[string]bool
}

func (m *Manager) Register(mod Module) error
func (m *Manager) Start(target string) error  // "all", "write", "read", "backend"
```

**Modules:**
- `server` — HTTP/gRPC listeners
- `ring` — Hash ring + memberlist
- `distributor` — Write path entry point
- `ingester` — Shard owner (wraps current Manager)
- `querier` — Read path executor (wraps current Router)
- `query-frontend` — Query splitting, caching
- `compactor` — Future: merges small shard files
- `admin` — Admin APIs (shards, tables, stats)

**Target composition:**
- `all` → all modules (monolithic, current behavior)
- `write` → server, ring, distributor, ingester
- `read` → server, ring, query-frontend, querier
- `backend` → server, compactor, admin

**Files:**
- `internal/module/` — new package
- `cmd/duckdb-cluster/main.go` — refactor to use module system

**Tests:** `TestModuleDependencyResolution`, `TestModuleStartOrder`

---

### 3.2 Configuration with YAML + Common Section

**Goal:** Single YAML file with per-component config, like [Loki's config](https://grafana.com/docs/loki/latest/configure/).

**Example config.yaml:**
```yaml
target: all  # all | write | read | backend

common:
  data_dir: ./data
  num_shards: 3
  ring:
    memberlist:
      join_peers: []  # empty = single-node

server:
  http_listen_addr: :8080
  grpc_listen_addr: :9095
  shutdown_timeout: 30s

distributor:
  validation:
    max_query_length: 1048576  # 1MB

ingester:
  max_shards_per_instance: 10

querier:
  merge_strategy: duckdb  # duckdb | simple
  max_concurrent_queries: 100

query_frontend:
  query_timeout: 60s
```

**Implementation:**
- Add `gopkg.in/yaml.v3` dependency
- `internal/config/config.go` — load, validate, apply defaults
- `common` section values inherited by components unless overridden

**Files:**
- `internal/config/` — new package
- `internal/cluster/config.go` — delete (replaced)

**Tests:** `TestConfigLoad`, `TestConfigCommonOverride`, `TestConfigValidation`

---

### 3.3 Read/Write Path Separation

**Goal:** Extract current monolithic router into distinct components that can run separately.

**New structure:**
```
internal/
├── distributor/
│   └── distributor.go    # Validates writes, hashes partition key, routes to ingester
├── ingester/
│   └── ingester.go       # Owns shards, handles Push RPC
├── querier/
│   └── querier.go        # Executes queries, merges results
├── frontend/
│   └── frontend.go       # Splits queries, caches, schedules to queriers
├── ring/
│   └── ring.go           # Consistent hash ring with memberlist
└── transport/
    ├── proto/            # gRPC service definitions
    ├── ingester.proto    # service Ingester { rpc Push(...) }
    └── querier.proto     # service Querier { rpc Query(...) }
```

**Write path flow:**
```
POST /query → Distributor → hash(partition_key) → Ingester.Push(gRPC) → Shard
```

**Read path flow:**
```
POST /query → QueryFrontend → split → Querier.Query(gRPC) → fan-out shards → merge
```

**In monolithic mode:** Components call each other directly (no gRPC overhead)
**In distributed mode:** Components discover each other via [memberlist gossip](https://grafana.com/blog/2020/03/25/how-were-using-gossip-to-improve-cortex-and-loki-availability/), communicate via gRPC

**Files:**
- Refactor existing code into new packages
- Add gRPC proto definitions

**Tests:** All existing tests must pass, add component isolation tests

---

### 3.4 Ring with Memberlist

**Goal:** Distributed node discovery and consistent hashing, like [Loki's hash rings](https://grafana.com/docs/loki/latest/get-started/hash-rings/).

**Features:**
- Consistent hash ring with virtual nodes (128 per node)
- Memberlist gossip for peer discovery
- `GetIngester(partitionKey) → NodeAddr`
- `GetAllQuer iers() → []NodeAddr`

**Implementation:**
- Add `github.com/hashicorp/memberlist` dependency
- In single-node mode: ring contains only local node
- In multi-node mode: nodes join via `join_peers` config

**Files:**
- `internal/ring/` — new package
- `internal/distributor/` — use ring to find target ingester
- `internal/frontend/` — use ring to find queriers

**Tests:** `TestRingJoin`, `TestRingConsistentHashing`, `TestRingMemberLeave`

---

### 3.5 gRPC Transport

**Goal:** Inter-component communication for distributed deployments.

**Proto definitions:**
```protobuf
// internal/transport/proto/ingester.proto
service Ingester {
  rpc Push(PushRequest) returns (PushResponse);
}

message PushRequest {
  string sql = 1;
  string partition_key = 2;
  string tenant_id = 3;  // future: multi-tenancy
}

message PushResponse {
  int64 rows_affected = 1;
  int32 shard_id = 2;
  string error = 3;
}
```

```protobuf
// internal/transport/proto/querier.proto
service Querier {
  rpc Query(QueryRequest) returns (QueryResponse);
}

message QueryRequest {
  string sql = 1;
  repeated int32 shard_ids = 2;  // which shards to query
}

message QueryResponse {
  repeated string columns = 1;
  repeated Row rows = 2;
  string error = 3;
}
```

**Implementation:**
- Add `google.golang.org/grpc` and `google.golang.org/protobuf` dependencies
- Each component exposes gRPC server when running in distributed mode
- Use interface pattern: components depend on `IngesterClient` interface, which can be local or gRPC

**Files:**
- `internal/transport/proto/` — proto files
- `internal/transport/` — generated code + client/server wrappers

**Tests:** `TestIngesterGRPC`, `TestQuerierGRPC`

---

## Phase 4: Go Client Library

Modeled after [go-elasticsearch](https://github.com/elastic/go-elasticsearch), providing both low-level and high-level APIs.

### Package Structure
```
pkg/
└── duckdbcluster/
    ├── client.go       # Client, Config, NewClient
    ├── query.go        # Query, MultiQuery methods
    ├── bulk.go         # BulkIndexer (async batch inserts)
    ├── admin.go        # Health, Stats, Tables, Shards
    ├── types.go        # Request/response types
    └── transport.go    # HTTP transport layer
```

### Client API

```go
package main

import (
    "context"
    "log"
    "time"
    
    duckdb "github.com/brown/duckdb-cluster/pkg/duckdbcluster"
)

func main() {
    // Initialize client
    client, err := duckdb.NewClient(duckdb.Config{
        Addresses: []string{"http://localhost:8080"},
        Timeout:   30 * time.Second,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    ctx := context.Background()
    
    // Query with options
    res, err := client.Query(ctx, "SELECT * FROM users WHERE age > 25",
        client.Query.WithPartitionKey(""),
        client.Query.WithLimit(10),
        client.Query.WithOffset(0),
    )
    if err != nil {
        log.Fatal(err)
    }
    log.Printf("Found %d rows", len(res.Rows))
    
    // Bulk insert
    bi, err := client.NewBulkIndexer(duckdb.BulkConfig{
        BatchSize:     1000,
        FlushInterval: time.Second,
        NumWorkers:    4,
    })
    if err != nil {
        log.Fatal(err)
    }
    
    for i := 0; i < 10000; i++ {
        err = bi.Add(ctx, duckdb.BulkItem{
            SQL:          "INSERT INTO users VALUES (?, ?)",
            PartitionKey: fmt.Sprintf("user-%d", i),
            Args:         []any{i, fmt.Sprintf("user%d@example.com", i)},
        })
        if err != nil {
            log.Fatal(err)
        }
    }
    
    stats := bi.Close(ctx)
    log.Printf("Indexed %d documents, %d failed", stats.Succeeded, stats.Failed)
    
    // Multi-query
    queries := []duckdb.Query{
        {SQL: "SELECT COUNT(*) FROM users"},
        {SQL: "SELECT AVG(age) FROM users"},
        {SQL: "SELECT * FROM orders WHERE user_id = ?", PartitionKey: "user-123", Args: []any{123}},
    }
    results, err := client.MultiQuery(ctx, queries)
    if err != nil {
        log.Fatal(err)
    }
    for i, res := range results {
        log.Printf("Query %d: %d rows", i, len(res.Rows))
    }
    
    // Admin operations
    health, err := client.Health(ctx)
    log.Printf("Cluster health: %s (%d shards)", health.Status, len(health.Shards))
    
    tables, err := client.Tables(ctx)
    log.Printf("Tables: %v", tables)
    
    schema, err := client.TableSchema(ctx, "users")
    log.Printf("Users table: %d columns", len(schema.Columns))
    
    stats, err := client.Stats(ctx)
    log.Printf("Total rows: %d", stats.Cluster.TotalRows)
}
```

### Implementation Notes

- **Transport layer:** Uses `net/http` with connection pooling, retries, and timeouts
- **Functional options pattern:** Clean API like `client.Query.WithLimit(10)`
- **BulkIndexer:** Background workers batching inserts, modeled after [esutil.BulkIndexer](https://github.com/elastic/go-elasticsearch/blob/main/_examples/bulk/indexer.go)
- **Error handling:** Wraps HTTP errors with context

**Files:**
- `pkg/duckdbcluster/` — new package (exported, outside `internal/`)
- `examples/client/` — example programs

**Tests:** `TestClientQuery`, `TestClientBulk`, `TestClientMultiQuery`, `TestClientAdmin`

---

## Features to SKIP

| Feature | Reason to Skip |
|---------|----------------|
| **JSON Query DSL** | SQL is already more powerful and expressive than Elasticsearch's Query DSL. Building a second query language (term, match, range, bool queries) would be massive scope with little benefit for an OLAP system. |
| **Document CRUD REST** (`/documents/{table}/{id}`) | Wrong paradigm for SQL-first system. Users can already do `INSERT`, `SELECT * WHERE id = ?`, `UPDATE`, `DELETE` via SQL. Adding REST sugar is redundant. |
| **Sort parameter in API** | Redundant once `ORDER BY` works correctly in Phase 1. Users should use SQL: `SELECT * FROM users ORDER BY age DESC` |
| **Source/field filtering param** | Redundant with `SELECT col1, col2 FROM ...`. SQL already provides this. |
| **Highlighting** | Text-search specific feature. DuckDB is an OLAP database, not a full-text search engine. Use purpose-built tools (Elasticsearch, Meilisearch) for this. |
| **Scroll API / Cursor pagination** | Requires server-side state (result caching). Offset/limit pagination is simpler and sufficient for analytics workloads. For very large exports, users can add `LIMIT` + `OFFSET` in a loop. |
| **Search_after pagination** | Needs sort values from previous page. More complex than needed for current use case. Consider later if deep pagination becomes a bottleneck. |

---

## Implementation Order

### Immediate (Next Session)
1. **Phase 1: Fix result merging** — Critical correctness bug, blocks accurate analytics

### Short-term (Next 1-2 weeks of work)
2. **Phase 2.1: Bulk operations** — High-value feature, straightforward implementation
3. **Phase 2.2: Pagination** — Simple enhancement to existing endpoint
4. **Phase 2.3: Multi-query** — Modest complexity, good UX win
5. **Phase 2.4: Table introspection** — Low complexity, enables tooling

### Medium-term (Next month)
6. **Phase 2.5: Enhanced health/stats** — Observability foundation
7. **Phase 3.1-3.2: Module system + YAML config** — Architectural foundation for scale-out

### Long-term (2-3 months)
8. **Phase 3.3-3.5: Microservices split + gRPC + Ring** — Full distributed deployment
9. **Phase 4: Go client library** — Developer experience, requires stable server API

---

## New Dependencies Summary

| Dependency | Phase | Purpose | Justification |
|------------|-------|---------|---------------|
| None | 1-2 | Correctness + API features | Pure stdlib implementation possible |
| `gopkg.in/yaml.v3` | 3 | YAML config files | Industry standard, zero-alloc parser |
| `google.golang.org/grpc` | 3 | Inter-component RPC | Industry standard, high-performance |
| `google.golang.org/protobuf` | 3 | gRPC message serialization | Required by gRPC |
| `github.com/hashicorp/memberlist` | 3 | Gossip-based node discovery | Battle-tested (used by Consul, Nomad, Loki) |

**Total new dependencies:** 4 (only for distributed mode; Phases 1-2 remain stdlib-only)

---

## Success Metrics

### Phase 1
- [ ] `SELECT COUNT(*) FROM table` on 3 shards returns 1 row with correct total
- [ ] `SELECT * FROM table ORDER BY col LIMIT 10` returns exactly 10 rows in correct order
- [ ] `SELECT DISTINCT col FROM table` returns no duplicates across shards
- [ ] All 11 existing tests pass + 8 new merge tests pass

### Phase 2
- [ ] Bulk insert 10,000 rows completes in <1 second
- [ ] Multi-query with 5 queries executes in parallel (faster than sequential)
- [ ] `/admin/tables` returns all tables from all shards
- [ ] `/health` shows per-shard latency

### Phase 3
- [ ] Single binary can run as `write`, `read`, `backend`, or `all` via `-target` flag
- [ ] YAML config loaded successfully with `common` section inheritance
- [ ] Module dependency graph resolves correctly

### Phase 4
- [ ] Client library can query distributed cluster (3 nodes: 1 write, 1 read, 1 backend)
- [ ] `BulkIndexer` achieves >10k inserts/sec
- [ ] Client automatically retries failed requests

---

## References

- [Elasticsearch API Documentation](https://www.elastic.co/docs/reference/elasticsearch/clients/go)
- [go-elasticsearch GitHub](https://github.com/elastic/go-elasticsearch)
- [Elasticsearch Pagination Methods](https://opster.com/guides/elasticsearch/how-tos/elasticsearch-pagination-techniques/)
- [Elasticsearch Multi-Search API](https://www.elastic.co/guide/en/elasticsearch/reference/8.15/search-multi-search.html)
- [Grafana Loki Components](https://grafana.com/docs/loki/latest/get-started/components/)
- [Loki Configuration System](https://grafana.com/docs/loki/latest/configure/)
- [Loki Hash Rings](https://grafana.com/docs/loki/latest/get-started/hash-rings/)
- [Loki Gossip Protocol](https://grafana.com/blog/2020/03/25/how-were-using-gossip-to-improve-cortex-and-loki-availability/)
- [go-elasticsearch Bulk Indexer Example](https://github.com/elastic/go-elasticsearch/blob/main/_examples/bulk/indexer.go)

---

## Next Steps

1. **Review this plan** with stakeholders
2. **Start Phase 1** (fix result merging) — highest priority, blocks accurate results
3. **Write checkpoint** after Phase 1 completion
4. **Iterate through phases** sequentially, maintaining test coverage

This plan balances **immediate correctness fixes** (Phase 1), **high-value API features** (Phase 2), and **long-term architectural evolution** (Phases 3-4) inspired by proven systems (Elasticsearch, Loki).
