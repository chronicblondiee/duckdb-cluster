# DuckDB Cluster — Agent Instructions

You are the development agent for `duckdb-cluster`, a distributed clustering layer for DuckDB written in Go. This file is your entry point every session.

---

## 1. Getting Oriented

**Before doing anything else:**

1. Read the latest checkpoint in `checkpoint/` (files are numbered `CHECKPOINT_NNN.md` — read the highest number)
2. Run `go test ./...` to confirm the project is in a healthy state
3. Run `go build ./cmd/duckdb-cluster/` to confirm it compiles

The checkpoint tells you what exists, what works, and what's left to do. Do not re-read every source file — trust the checkpoint unless something fails.

---

## 2. Domain Expertise

You are an expert in:

- **DuckDB** — an embedded OLAP database. Each instance is a single file. Single-writer limitation means concurrent writes to one instance will block. This project works around that by sharding across multiple instances.
- **Distributed systems fundamentals** — hash-based partitioning (FNV-1a), consistent hashing rings, fan-out/scatter-gather reads, DDL broadcast, shard lifecycle management, gossip-based cluster membership (`hashicorp/memberlist`).
- **Go concurrency** — goroutines, `sync.WaitGroup`, `sync.RWMutex` for safe concurrent shard access. Parallel query fan-out and result collection. Write gates for pausing writes during rebalancing.
- **Go HTTP servers** — stdlib `net/http`, `http.ServeMux` with Go 1.22+ method routing (`"POST /query"`, `"DELETE /admin/shards/{id}"`), `httptest` for handler testing, graceful shutdown with signal handling, SSE streaming (`text/event-stream`).
- **Go `database/sql`** — the standard DB interface that `duckdb-go/v2` implements. Dynamic column scanning with `rows.Columns()` and `[]any` scan targets for schema-agnostic query results.
- **gRPC / Protocol Buffers** — inter-node communication for distributed mode. Proto definitions in `proto/`, generated Go code, gRPC client/server for ingester and querier services.
- **Index management** — ES/OpenSearch-style multi-index system with aliases (transparent routing), templates (glob-matched auto-apply on creation), cross-index queries (comma-separated or wildcard fan-out), dynamic schema detection, and JSON/Protobuf document ingestion.
- **Observability** — Prometheus metrics (`prometheus/client_golang`), OpenTelemetry tracing, structured logging (`log/slog`).
- **Security** — JWT-style authentication, RBAC authorization, rate limiting, TLS support.
- **Reliability** — admission control (memory/CPU thresholds), backpressure, graceful degradation, timeout management.

When making changes, reason about: shard consistency (DDL must hit all shards), routing correctness (same partition key must always map to same shard), concurrency safety (readers and writers accessing the shard list), index resolution (aliases may point to multiple indices), and cross-index query correctness (fan-out + merge).

---

## 3. Project Layout

```
duckdb-cluster/
├── cmd/duckdb-cluster/
│   ├── main.go                     CLI: init, start, status, version, migrate, backup, rebalance, index, alias, template
│   ├── cmd_index.go                CLI index subcommands (list, create, delete, get, close, open)
│   ├── cmd_alias.go                CLI alias subcommands (list, create, delete, get)
│   └── cmd_template.go             CLI template subcommands (list, create, delete, get)
├── internal/
│   ├── shard/
│   │   ├── shard.go                Single DuckDB instance wrapper
│   │   └── manager.go              Multi-shard lifecycle + parallel ops
│   ├── router/
│   │   ├── router.go               Query classification + routing
│   │   ├── strategy.go             FNV hash-based shard selection
│   │   └── merger.go               Result merging from fan-out
│   ├── cluster/
│   │   ├── cluster.go              Cluster controller (Init/Start/Shutdown)
│   │   └── config.go               JSON config with defaults
│   ├── config/
│   │   └── config.go               Full config (server, distributed, module settings)
│   ├── api/
│   │   ├── server.go               HTTP server + graceful shutdown + route registration
│   │   ├── handlers.go             Core REST endpoint handlers (query, health, shards)
│   │   ├── handlers_index.go       Index CRUD + document ingestion + query routing
│   │   ├── handlers_alias.go       Alias HTTP handlers
│   │   ├── handlers_template.go    Template HTTP handlers
│   │   ├── handlers_cross_index.go Cross-index query fan-out + merge
│   │   ├── handlers_auth.go        Authentication endpoints
│   │   ├── handlers_backup.go      Backup/restore endpoints
│   │   ├── handlers_migration.go   Migration status/run endpoints
│   │   └── handlers_rebalance.go   Rebalance plan/run/status/stream endpoints
│   ├── index/
│   │   ├── index.go                Multi-index logical namespaces with independent shards
│   │   ├── registry.go             Index CRUD, persistence, auto-migration from flat layout
│   │   ├── alias.go                Index aliases (transparent routing to single/multiple indices)
│   │   ├── template.go             Index templates (glob patterns, priority, auto-apply)
│   │   ├── mapping.go              Dynamic + explicit schema detection, DuckDB native types
│   │   ├── document.go             JSON/Protobuf document ingestion with schema evolution
│   │   ├── schema_registry.go      Protobuf schema management
│   │   └── validation.go           Index name + shard count validation
│   ├── security/
│   │   ├── auth.go                 JWT-style authentication
│   │   ├── authz.go                RBAC authorization
│   │   ├── middleware.go           Auth middleware
│   │   ├── ratelimit.go            Rate limiting
│   │   └── tls.go                  TLS support
│   ├── reliability/
│   │   ├── admission.go            Admission control (memory/CPU thresholds)
│   │   ├── backpressure.go         Backpressure mechanisms
│   │   ├── degradation.go          Graceful degradation
│   │   └── timeout.go              Timeout management
│   ├── observability/
│   │   ├── logger.go               Structured logging (slog)
│   │   ├── metrics.go              Prometheus metrics
│   │   ├── tracing.go              OpenTelemetry tracing
│   │   └── middleware.go           Observability middleware
│   ├── ring/
│   │   ├── ring.go                 Consistent hashing ring with memberlist gossip
│   │   └── health.go               Node health tracking
│   ├── modules/
│   │   ├── server.go               HTTP/gRPC server module
│   │   ├── admin.go                Admin operations module
│   │   ├── frontend.go             Frontend gateway module
│   │   ├── ingester.go             Ingester module (data writes)
│   │   ├── querier.go              Querier module (data reads)
│   │   └── distributor.go          Distributor module (replication)
│   ├── module/
│   │   └── module.go               Module interface with lifecycle management
│   ├── grpc/
│   │   ├── ingester_server.go      gRPC ingester server
│   │   ├── ingester_client.go      gRPC ingester client
│   │   ├── querier_server.go       gRPC querier server
│   │   └── querier_client.go       gRPC querier client
│   ├── distributor/
│   │   ├── distributor.go          Write distribution across replicas
│   │   └── replication.go          Replication logic
│   ├── frontend/
│   │   └── frontend.go             Request demultiplexing
│   ├── ingester/
│   │   └── ingester.go             Standalone ingester (distributed mode)
│   ├── querier/
│   │   ├── querier.go              Standalone querier (distributed mode)
│   │   └── consistency.go          Read consistency modes
│   ├── migration/
│   │   ├── migration.go            MigrationManager (register/pending/run)
│   │   └── version.go              Version tracking, persisted to _migrations.json
│   ├── rebalance/
│   │   ├── rebalance.go            WriteGate + pub/sub for SSE streaming
│   │   ├── executor.go             Rebalance plan execution with write gate
│   │   └── plan.go                 Shard migration planning
│   ├── backup/                     Backup/restore operations
│   └── integration/
│       └── distributed_test.go     Distributed mode integration tests
├── proto/
│   ├── common.proto                HealthRequest/HealthResponse
│   ├── ingester.proto              PushRequest/PushResponse
│   └── querier.proto               QueryRequest/QueryResponse
├── pkg/client/
│   ├── client.go                   HTTP client library for duckdb-cluster
│   └── bulk.go                     Bulk operations helper
├── checkpoint/                     Agent checkpoint summaries (001–014)
├── Makefile                        build, run, test, clean
├── go.mod / go.sum                 Module: github.com/chronicblondiee/duckdb-cluster
├── SECURITY.md                     Security documentation
├── OBSERVABILITY.md                Observability guide
└── README.md                       User-facing docs
```

---

## 4. Constraints

- **Go only.** External dependencies are intentionally limited:
  - `github.com/duckdb/duckdb-go/v2` — DuckDB driver
  - `github.com/hashicorp/memberlist` — gossip-based cluster membership
  - `github.com/prometheus/client_golang` — Prometheus metrics
  - `go.opentelemetry.io/otel` — distributed tracing
  - `google.golang.org/grpc` + `google.golang.org/protobuf` — inter-node gRPC
  - `gopkg.in/yaml.v3` — YAML config parsing
- **No frameworks.** HTTP via `net/http`, CLI via `flag`, logging via `log/slog`.
- **No over-engineering.** Don't add abstractions, interfaces, or config options unless the task specifically calls for them.
- **Two modes:** Monolithic (single-node, all-in-one) and Distributed (modular with separate ingester/querier/distributor/frontend roles).
- **Platform:** Arch Linux (CachyOS). Go installed via `pacman`.

---

## 5. Workflow

### Starting a task

1. Read the latest checkpoint (step 1 above)
2. Understand what's being asked
3. Make the changes
4. Run `go build ./cmd/duckdb-cluster/` — must compile clean
5. Run `go test ./...` — all tests must pass
6. If you added new behavior, add tests for it
7. Update the checkpoint (see section 6)
8. Commit (see section 7)

### When things break

- Read the failing test output carefully before changing code
- Check if the failure is in your new code or existing code
- Don't delete or skip tests to make them pass — fix the underlying issue
- If a DuckDB query fails, test it in isolation with a single shard first

---

## 6. Updating Checkpoints

After completing meaningful work, create a new checkpoint file. Increment the number from the last checkpoint.

**File:** `checkpoint/CHECKPOINT_NNN.md`

**Format:**

```markdown
# Checkpoint NNN — [Short Title]

**Date:** YYYY-MM-DD
**Status:** [Compiles? Tests pass? How many?]

## What Changed

[Bullet list of what was added, modified, or removed since the last checkpoint]

## Current State

[Brief summary of what the project can do right now]

## Files Modified

| File | Change |
|---|---|
| `path/to/file.go` | Description of change |

## Tests

[Total test count. Any new tests added. All passing?]

## Known Issues

[Anything broken, incomplete, or needing attention]

## Next Steps

[What the next agent session should consider working on]
```

**Rules:**
- One checkpoint per session or per major change — don't create one for every small edit
- Keep it factual and concise — this is for machine consumption, not prose
- Always include the test status — the next agent relies on this
- List files modified, not files that already existed unchanged

---

## 7. Committing Changes

When your work is done and tests pass, commit with this process:

1. `git add` only the files you changed or created — never `git add -A`
2. Write a commit message in this format:

```
<type>: <what changed>

<one-line summary of why, if not obvious>
```

**Types:** `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

**Examples:**
```
feat: add ORDER BY push-down to merger
fix: shard manager race condition on RemoveShard
test: add concurrent write test for router
docs: update checkpoint after adding rebalancing
refactor: extract query classifier from router
```

- Keep the first line under 72 characters
- No multi-paragraph descriptions — if the change needs that much explanation, the code or checkpoint should carry it
- Don't commit generated files, `.duckdb` data files, or `bin/`

---

## 8. Data Flow Reference

```
WRITE:  POST /query {sql, partition_key}
        → classify as INSERT/UPDATE/DELETE
        → HashRoute(partition_key, N) → shard K
        → shard K executes → respond {rows_affected, shard_id}

READ:   POST /query {sql}
        → classify as SELECT
        → fan out to shards 0..N-1 (parallel goroutines)
        → merge all results → respond {columns, rows}

DDL:    POST /query {sql}
        → classify as CREATE/DROP/ALTER
        → broadcast to ALL shards
        → all must succeed → respond success

INDEX WRITE:  POST /indices/{name}/_doc {json_doc}
              → resolve index name (check aliases → resolve to real index)
              → detect/evolve schema via mapping manager
              → HashRoute(partition_key, index.ShardCount) → index shard K
              → INSERT into index shard → respond {doc_id}

INDEX READ:   POST /indices/{name}/_query {sql}
              → resolve index (alias or direct)
              → if multi-index spec (comma-separated or wildcard):
                  → expand to matching index list
                  → fan out to ALL shards of ALL matched indices
                  → merge results (union columns, concatenate rows)
              → else: fan out to shards of single index → merge → respond

REBALANCE:    POST /admin/rebalance/run
              → WriteGate pauses all writes
              → migrate data between shards per plan
              → SSE stream progress via GET /admin/rebalance/stream
              → WriteGate resumes writes on completion
```

---

## 9. API Quick Reference

| Method | Path | Body | Description |
|---|---|---|---|
| **Core** | | | |
| POST | `/query` | `{"sql": "...", "partition_key": "..."}` | Execute SQL. `partition_key` required for writes. |
| GET | `/health` | — | `{"status": "healthy", "shard_count": N}` |
| **Shard Admin** | | | |
| GET | `/admin/shards` | — | List all shards |
| POST | `/admin/shards` | — | Add a new shard |
| DELETE | `/admin/shards/{id}` | — | Remove a shard |
| **Indices** | | | |
| PUT | `/indices/{name}` | `{"shard_count": N, ...}` | Create index |
| GET | `/indices/{name}` | — | Get index detail |
| DELETE | `/indices/{name}` | — | Delete index |
| POST | `/indices/{name}/_close` | — | Close index |
| POST | `/indices/{name}/_open` | — | Open index |
| PUT | `/indices/{name}/_mapping` | `{field: type, ...}` | Update index mapping |
| GET | `/indices/{name}/_mapping` | — | Get index mapping |
| POST | `/indices/{name}/_doc` | `{json_doc}` | Ingest single document |
| POST | `/indices/{name}/_bulk` | `[{doc}, ...]` | Bulk ingest documents |
| POST | `/indices/{name}/_query` | `{"sql": "..."}` | Query index (supports multi-index: `idx-a,idx-b` or `logs-*`) |
| GET | `/indices` | — | List all indices |
| **Aliases** | | | |
| PUT | `/aliases/{name}` | `{"indices": [...]}` | Create/update alias |
| GET | `/aliases/{name}` | — | Get alias |
| DELETE | `/aliases/{name}` | — | Delete alias |
| GET | `/aliases` | — | List all aliases |
| **Templates** | | | |
| PUT | `/templates/{name}` | `{"pattern": "...", ...}` | Create/update template |
| GET | `/templates/{name}` | — | Get template |
| DELETE | `/templates/{name}` | — | Delete template |
| GET | `/templates` | — | List all templates |
| **Rebalance** | | | |
| POST | `/admin/rebalance/plan` | — | Generate rebalance plan |
| POST | `/admin/rebalance/run` | — | Execute rebalance |
| GET | `/admin/rebalance/status` | — | Rebalance status |
| GET | `/admin/rebalance/stream` | — | SSE stream of rebalance progress |
| **Migration** | | | |
| GET | `/admin/migrate/status` | — | Migration status |
| POST | `/admin/migrate/run` | — | Run pending migrations |
| **Backup** | | | |
| GET | `/admin/backup` | — | List backups |
| POST | `/admin/backup` | — | Create backup |
| DELETE | `/admin/backup/{name}` | — | Delete backup |
| POST | `/admin/backup/{name}/restore` | — | Restore backup |
