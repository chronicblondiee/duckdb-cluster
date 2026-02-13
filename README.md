# duckdb-cluster

A lightweight distributed clustering layer for DuckDB written in Go. Each shard is an independent DuckDB instance. Writes are hash-routed across shards to bypass DuckDB's single-writer limitation. Reads fan out to all shards and results are merged.

## Quick Start

```bash
# Build
make build

# Initialize a cluster with 3 shards
./bin/duckdb-cluster init --shards 3

# Start the server
./bin/duckdb-cluster start

# Check status
./bin/duckdb-cluster status
```

## CLI

```
duckdb-cluster init [--shards N] [--data-dir PATH] [--config PATH]
duckdb-cluster start [--config PATH] [--target MODE] [--addr :8080] [--data-dir PATH]
duckdb-cluster status [--addr :8080]
```

**Examples:**
```bash
# Initialize with custom config
./duckdb-cluster init --shards 5 --data-dir /var/lib/duckdb --config prod.yaml

# Start with specific target
./duckdb-cluster start --config prod.yaml --target all

# Override listen address
./duckdb-cluster start --config prod.yaml --addr :9090
```

## API

### POST /query

Execute SQL statements. The router classifies the query and routes accordingly:

- **DDL** (`CREATE`, `DROP`, `ALTER`) — broadcast to all shards
- **Writes** (`INSERT`, `UPDATE`, `DELETE`) — hash-routed to one shard via `partition_key`
- **Reads** (`SELECT`) — fan out to all shards, results merged

```bash
# Create a table (runs on all shards)
curl -X POST localhost:8080/query \
  -d '{"sql": "CREATE TABLE users (id INTEGER, name VARCHAR)"}'

# Insert a row (routed by partition key)
curl -X POST localhost:8080/query \
  -d '{"sql": "INSERT INTO users VALUES (1, '\''alice'\'')", "partition_key": "1"}'

# Query all shards with pagination
curl -X POST localhost:8080/query \
  -d '{"sql": "SELECT * FROM users ORDER BY id", "limit": 10, "offset": 0}'
```

**Write request:**
```json
{"sql": "INSERT INTO users VALUES (1, 'alice')", "partition_key": "1"}
```

**Write response:**
```json
{"success": true, "rows_affected": 1, "shard_id": 2, "rows": null}
```

**Read response:**
```json
{
  "success": true,
  "columns": ["id", "name"],
  "rows": [{"id": 1, "name": "alice"}, {"id": 2, "name": "bob"}],
  "shard_id": -1
}
```

**Pagination:** Add `offset` and `limit` to any read query:
```json
{"sql": "SELECT * FROM users ORDER BY id", "offset": 20, "limit": 10}
```

### POST /bulk

Execute multiple statements in a single request. Statements are grouped by target shard and executed in parallel.

```bash
curl -X POST localhost:8080/bulk -d '{
  "statements": [
    {"sql": "INSERT INTO users VALUES (1, '\''alice'\'')", "partition_key": "1"},
    {"sql": "INSERT INTO users VALUES (2, '\''bob'\'')", "partition_key": "2"},
    {"sql": "INSERT INTO users VALUES (3, '\''charlie'\'')", "partition_key": "3"}
  ]
}'
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

### POST /multi-query

Execute multiple queries concurrently. Perfect for dashboards that need multiple metrics simultaneously.

```bash
curl -X POST localhost:8080/multi-query -d '{
  "queries": [
    {"sql": "SELECT COUNT(*) as cnt FROM users"},
    {"sql": "SELECT AVG(age) as avg_age FROM users"},
    {"sql": "SELECT * FROM orders WHERE user_id = 123", "partition_key": "123"}
  ]
}'
```

**Response:**
```json
{
  "results": [
    {"success": true, "columns": ["cnt"], "rows": [{"cnt": 1523}]},
    {"success": true, "columns": ["avg_age"], "rows": [{"avg_age": 35.7}]},
    {"success": true, "columns": ["id", "total"], "rows": [...]}
  ],
  "took_ms": 89
}
```

### GET /health

```json
{"status": "healthy", "shard_count": 3}
```

### GET /admin/tables

List all tables across the cluster:
```json
{"tables": ["users", "products", "orders"]}
```

### GET /admin/tables/{name}

Get schema information for a specific table:
```json
{
  "name": "users",
  "columns": [
    {"name": "id", "type": "INTEGER", "nullable": false},
    {"name": "email", "type": "VARCHAR", "nullable": false},
    {"name": "age", "type": "INTEGER", "nullable": true}
  ]
}
```

### GET /admin/stats

Get detailed cluster statistics including per-shard metrics:
```json
{
  "cluster": {
    "total_shards": 3,
    "total_tables": 4,
    "total_rows": 1523000
  },
  "shards": [
    {"id": 0, "path": "./data/shard_000.duckdb", "size_mb": 245, "table_count": 4, "row_count": 508000},
    {"id": 1, "path": "./data/shard_001.duckdb", "size_mb": 238, "table_count": 4, "row_count": 502000},
    {"id": 2, "path": "./data/shard_002.duckdb", "size_mb": 250, "table_count": 4, "row_count": 513000}
  ]
}
```

### GET /admin/shards

List all shards with their status.

### POST /admin/shards

Add a new shard dynamically. No request body needed.

### DELETE /admin/shards/{id}

Remove a shard by ID.

## Architecture

```
Client → POST /query {sql, partition_key}
       → Router classifies SQL by first keyword
       → DDL:    broadcast to ALL shards
       → Write:  HashRoute(partition_key, N) → single shard
       → Read:   fan out to all shards → merge results
       → JSON response
```

Each shard is a standalone DuckDB file (`shard_000.duckdb`, `shard_001.duckdb`, etc.) in the data directory. Partition key routing uses FNV-1a hashing.

## Project Structure

```
duckdb-cluster/
├── cmd/duckdb-cluster/     CLI entrypoint with module system
├── internal/
│   ├── module/             Module lifecycle manager
│   ├── modules/            Module implementations (server, ingester, querier, etc.)
│   ├── config/             YAML configuration system
│   ├── distributor/        Write path validation and routing
│   ├── ingester/           Shard ownership and writes
│   ├── querier/            Query execution
│   ├── frontend/           Query coordination with retries
│   ├── ring/               Consistent hash ring for distributed mode
│   ├── shard/              Single shard wrapper + multi-shard manager
│   ├── router/             Query classification, hash routing, result merging
│   ├── cluster/            Cluster controller (backward compatibility)
│   └── api/                HTTP server + REST handlers
├── checkpoint/             Development progress snapshots
├── Makefile
└── config.yaml             Generated config (after init)
```

## Configuration

Configuration can be provided via YAML file (recommended) or falls back to sensible defaults.

### YAML Configuration

`config.yaml` is created by `init` and read by `start`:

```yaml
target: all  # all | write | read | backend

common:
  data_dir: ./data
  num_shards: 3
  log_level: info

server:
  http_listen_addr: :8080
  grpc_listen_addr: :9095
  shutdown_timeout: 30s

distributor:
  max_query_length: 1048576  # 1MB

ingester:
  max_shards_per_instance: 10

querier:
  merge_strategy: duckdb
  max_concurrent_queries: 100
  query_timeout: 60s

query_frontend:
  query_timeout: 60s
  max_retries: 3
```

### Target Modes

The system supports multiple deployment targets for future distributed deployments:

| Target | Components | Use Case |
|---|---|---|
| `all` | All modules | Single-node deployment (default) |
| `write` | server, distributor, ingester | Write-only node |
| `read` | server, query-frontend, querier | Read-only node |
| `backend` | server, admin, compactor | Admin/maintenance node |

**Current:** Only `all` (monolithic) mode is production-ready.

## Development

```bash
make build    # Build binary to bin/
make run      # Build and start server
make test     # Run all tests
make clean    # Remove bin/ and data/
```

## Go Client Library

For Go applications, use the high-level client library with connection pooling, automatic retries, and efficient bulk indexing:

```go
import "github.com/brown/duckdb-cluster/pkg/client"

// Create client
c := client.New("http://localhost:8080")

// Execute queries
resp, err := c.Select(ctx, "SELECT * FROM users")
resp, err := c.Insert(ctx, "INSERT INTO users VALUES (1, 'Alice')", "user-1")

// Bulk inserts (10-100x faster)
bi := c.NewBulkIndexer(client.BulkIndexerConfig{
    FlushSize:     1000,
    FlushInterval: 5 * time.Second,
    Workers:       4,
})
defer bi.Close(ctx)

for _, item := range items {
    bi.Add(ctx, client.BulkItem{
        SQL:          "INSERT INTO events VALUES (...)",
        PartitionKey: item.Key,
    })
}
```

**See [`pkg/client/README.md`](pkg/client/README.md) for complete documentation and examples.**

## Dependencies

- [duckdb-go/v2](https://github.com/duckdb/duckdb-go) — DuckDB driver for Go
- [yaml.v3](https://gopkg.in/yaml.v3) — YAML configuration parsing
- Go stdlib for everything else (net/http, database/sql, hash/fnv, context, sync)

## Features

### Phase 1: Core Clustering
✅ Hash-based sharding with FNV-1a  
✅ Query classification (DDL, Read, Write)  
✅ Write routing to single shard  
✅ Read fan-out with result merging  
✅ DDL broadcast to all shards  

### Phase 2: Elasticsearch-Inspired APIs
✅ Pagination support (offset/limit)  
✅ Bulk operations endpoint  
✅ Multi-query concurrent execution  
✅ Table introspection  
✅ Enhanced cluster statistics  

### Phase 3: Loki-Inspired Architecture
✅ Module system with dependency resolution  
✅ YAML configuration with hierarchical structure  
✅ Component separation (Distributor, Ingester, Querier, Frontend)  
✅ Consistent hash ring for future distributed mode  
✅ Multiple deployment targets (all, write, read, backend)  

### Phase 4: Go Client Library
✅ High-level client API with functional options  
✅ Connection pooling and automatic retries  
✅ BulkIndexer for async batch inserts (10-100x faster)  
✅ Full context.Context support  
✅ Complete examples and documentation  

### Future: Phase 5 (Planned)
- gRPC transport for distributed multi-node deployments
- Memberlist integration for cluster membership
- Replication with configurable factor
- Multi-node integration tests
