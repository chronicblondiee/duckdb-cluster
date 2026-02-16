# duckdb-cluster

A distributed clustering layer for DuckDB written in Go. Splits data across multiple independent DuckDB shards to enable parallel writes, then fans out reads and merges results. Supports both single-node and multi-node deployments with indices, aliases, templates, lifecycle automation, security, observability, and backup/restore.

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

### Cluster Management

```
duckdb-cluster init [--shards N] [--data-dir PATH] [--config PATH]
duckdb-cluster start [--config PATH] [--target MODE] [--addr :8080] [--data-dir PATH]
duckdb-cluster status [--addr :8080]
duckdb-cluster version [--addr :8080]
```

### Data Management

```
duckdb-cluster index list [--addr :8080]
duckdb-cluster index create --name NAME [--shards N] [--partition-key FIELD] [--addr :8080]
duckdb-cluster index delete --name NAME [--addr :8080]
duckdb-cluster index get --name NAME [--addr :8080]
duckdb-cluster index close --name NAME [--addr :8080]
duckdb-cluster index open --name NAME [--addr :8080]

duckdb-cluster alias list [--addr :8080]
duckdb-cluster alias create --name NAME --indices idx1,idx2 [--addr :8080]
duckdb-cluster alias delete --name NAME [--addr :8080]
duckdb-cluster alias get --name NAME [--addr :8080]

duckdb-cluster template list [--addr :8080]
duckdb-cluster template create --name PATTERN [--shards N] [--partition-key FIELD] [--addr :8080]
duckdb-cluster template delete --name NAME [--addr :8080]
duckdb-cluster template get --name NAME [--addr :8080]

duckdb-cluster ism list [--addr :8080]
duckdb-cluster ism create --name NAME --file policy.yaml [--addr :8080]
duckdb-cluster ism delete --name NAME [--addr :8080]
duckdb-cluster ism get --name NAME [--addr :8080]
duckdb-cluster ism status [--addr :8080]
duckdb-cluster ism attach --index NAME --policy POLICY [--addr :8080]
duckdb-cluster ism detach --index NAME [--addr :8080]
duckdb-cluster ism retry --index NAME [--addr :8080]
```

### Operations

```
duckdb-cluster migrate status [--addr :8080]
duckdb-cluster migrate run [--addr :8080]

duckdb-cluster backup list [--addr :8080]
duckdb-cluster backup create [--type full|incremental] [--addr :8080]
duckdb-cluster backup restore --id ID [--addr :8080]
duckdb-cluster backup delete --id ID [--addr :8080]

duckdb-cluster rebalance plan --partition-key COL [--tables t1,t2] [--target-shards N] [--addr :8080]
duckdb-cluster rebalance run --partition-key COL [--batch-size N] [--target-shards N] [--addr :8080]
duckdb-cluster rebalance status [--addr :8080]
```

## API

### Core Query Endpoints

#### POST /query

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

#### POST /bulk

Execute multiple statements in a single request. Statements are grouped by target shard and executed in parallel.

```bash
curl -X POST localhost:8080/bulk -d '{
  "statements": [
    {"sql": "INSERT INTO users VALUES (1, '\''alice'\'')", "partition_key": "1"},
    {"sql": "INSERT INTO users VALUES (2, '\''bob'\'')", "partition_key": "2"}
  ]
}'
```

#### POST /multi-query

Execute multiple queries concurrently.

```bash
curl -X POST localhost:8080/multi-query -d '{
  "queries": [
    {"sql": "SELECT COUNT(*) as cnt FROM users"},
    {"sql": "SELECT AVG(age) as avg_age FROM users"}
  ]
}'
```

#### GET /health

```json
{"status": "healthy", "shard_count": 3}
```

#### GET /metrics

Prometheus metrics endpoint.

### Index Management

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/indices/{name}` | Create an index |
| `GET` | `/indices` | List all indices |
| `GET` | `/indices/{name}` | Get index details |
| `DELETE` | `/indices/{name}` | Delete an index |
| `POST` | `/indices/{name}/_close` | Close an index |
| `POST` | `/indices/{name}/_open` | Open a closed index |
| `PUT` | `/indices/{name}/_mapping` | Set index mapping |
| `GET` | `/indices/{name}/_mapping` | Get index mapping |
| `PUT` | `/indices/{name}/_schema` | Set protobuf schema |
| `GET` | `/indices/{name}/_schema` | Get protobuf schema |
| `DELETE` | `/indices/{name}/_schema` | Delete protobuf schema |

```bash
# Create an index with 3 shards
curl -X PUT localhost:8080/indices/logs -d '{
  "num_shards": 3,
  "partition_key_field": "id"
}'

# List indices
curl localhost:8080/indices
```

### Document Ingestion

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/indices/{name}/_doc` | Index a single document |
| `POST` | `/indices/{name}/_bulk` | Bulk index documents |

```bash
# Index a document (schema auto-detected)
curl -X POST localhost:8080/indices/logs/_doc -d '{
  "id": "evt-1",
  "level": "info",
  "message": "server started",
  "timestamp": "2026-02-16T10:00:00Z"
}'

# Bulk index
curl -X POST localhost:8080/indices/logs/_bulk -d '{
  "documents": [
    {"id": "evt-2", "level": "warn", "message": "high memory"},
    {"id": "evt-3", "level": "error", "message": "timeout"}
  ]
}'
```

### Aliases

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/aliases/{name}` | Create/update an alias |
| `GET` | `/aliases` | List all aliases |
| `GET` | `/aliases/{name}` | Get alias details |
| `DELETE` | `/aliases/{name}` | Delete an alias |

```bash
# Create an alias pointing to multiple indices
curl -X PUT localhost:8080/aliases/logs-all -d '{
  "indices": ["logs-2026-01", "logs-2026-02"]
}'
```

### Templates

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/templates/{name}` | Create a template |
| `GET` | `/templates` | List all templates |
| `GET` | `/templates/{name}` | Get template details |
| `DELETE` | `/templates/{name}` | Delete a template |

```bash
# Create a template for all logs-* indices
curl -X PUT localhost:8080/templates/logs-template -d '{
  "index_patterns": ["logs-*"],
  "num_shards": 3,
  "partition_key_field": "id"
}'
```

### ISM (Index State Management)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/ism/policies/{name}` | Create/update a policy |
| `GET` | `/ism/policies` | List all policies |
| `GET` | `/ism/policies/{name}` | Get policy details |
| `DELETE` | `/ism/policies/{name}` | Delete a policy |
| `POST` | `/ism/attach/{index}` | Attach a policy to an index |
| `POST` | `/ism/detach/{index}` | Detach a policy from an index |
| `GET` | `/ism/status` | Get ISM status for all indices |
| `GET` | `/ism/status/{index}` | Get ISM status for one index |
| `POST` | `/ism/retry/{index}` | Retry a failed action |

### Admin & Operations

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/shards` | List shards |
| `POST` | `/admin/shards` | Add a shard |
| `DELETE` | `/admin/shards/{id}` | Remove a shard |
| `GET` | `/admin/tables` | List tables |
| `GET` | `/admin/tables/{name}` | Get table schema |
| `GET` | `/admin/stats` | Cluster statistics |
| `POST` | `/admin/auth/token` | Generate JWT token |
| `POST` | `/admin/auth/apikey` | Register an API key |
| `DELETE` | `/admin/auth/apikey` | Revoke an API key |
| `POST` | `/admin/backups` | Create a backup |
| `GET` | `/admin/backups` | List backups |
| `POST` | `/admin/backups/{id}/restore` | Restore from backup |
| `DELETE` | `/admin/backups/{id}` | Delete a backup |
| `GET` | `/admin/version` | Version and migration info |
| `POST` | `/admin/migrate` | Run pending migrations |
| `POST` | `/admin/rebalance` | Start rebalance |
| `POST` | `/admin/rebalance/plan` | Dry-run rebalance plan |
| `GET` | `/admin/rebalance/status` | Rebalance progress |
| `GET` | `/admin/rebalance/stream` | SSE stream of rebalance events |

## Architecture

### The Big Picture

DuckDB is a fast embedded database, but it only allows one writer at a time. duckdb-cluster works around this by splitting your data across multiple independent DuckDB files called **shards**. Each shard is its own database, so writes can happen in parallel across shards without blocking each other.

```
                         ┌─────────────┐
                         │   Client    │
                         └──────┬──────┘
                                │
                         ┌──────▼──────┐
                         │  HTTP API   │
                         └──────┬──────┘
                                │
                         ┌──────▼──────┐
                         │   Router    │  ← classifies SQL as DDL / Write / Read
                         └──┬───┬───┬──┘
                            │   │   │
              ┌─────────────┤   │   ├─────────────┐
              ▼             ▼   │   ▼             ▼
         ┌─────────┐  ┌─────────┐  ┌─────────┐
         │ Shard 0 │  │ Shard 1 │  │ Shard 2 │   ← each is a separate .duckdb file
         └─────────┘  └─────────┘  └─────────┘
```

### How Queries Work

Every SQL statement that comes in gets classified by the **Router**:

- **DDL** (`CREATE TABLE`, `ALTER`, `DROP`) — sent to **all** shards so the schema stays consistent everywhere
- **Writes** (`INSERT`, `UPDATE`, `DELETE`) — a hash function (FNV-1a) turns the partition key into a shard number, so the row goes to exactly **one** shard
- **Reads** (`SELECT`) — sent to **all** shards in parallel, then results are collected and merged into a single response

For read queries that involve `ORDER BY`, `LIMIT`, `DISTINCT`, or aggregations, the merger loads partial results into a temporary in-memory DuckDB instance and re-runs the query to get the correct final answer.

### Indices, Aliases, and Templates

Instead of working with raw tables and shards directly, you can organize data into **indices** (similar to Elasticsearch):

- **Index** — a named collection of shards. Each index has its own shard files, schema, and partition key. For example, a `logs` index might have 3 shards while a `users` index has 1.
- **Alias** — a pointer to one or more indices. You can query an alias and it transparently fans out to all the indices behind it. Useful for things like `logs-current` always pointing at the latest logs index.
- **Template** — a set of defaults (shard count, partition key, schema) that get auto-applied when creating new indices whose name matches a glob pattern. For example, a template matching `logs-*` can ensure every logs index gets the right settings.

Documents are ingested as JSON. The schema is detected automatically from the first document and evolves as new fields appear.

### Index State Management (ISM)

ISM lets you automate the lifecycle of your indices with policies. A policy is a state machine — each state has actions and conditions for transitioning to the next state:

- **Rollover** — when an index gets too old or too large, create a new one and update the alias
- **Force Merge** — compact the data (runs `VACUUM` on the shards)
- **Read-Only** — lock an index to prevent further writes
- **Snapshot** — back up all shard files
- **Delete** — remove an index after a retention period
- **Notification** — send a webhook when a state change happens

ISM runs on a background timer, evaluating policies and executing actions automatically.

### Two Deployment Modes

**Monolithic (single node)** — everything runs in one process. The Router talks directly to local shards. This is the simplest setup and works well for small-to-medium workloads.

```
┌─────────────────────────────────────────────────┐
│                  Single Process                  │
│  API → Router → Shards (all local)              │
└─────────────────────────────────────────────────┘
```

**Distributed (multi-node)** — components are split across machines that communicate via gRPC. A consistent hash ring (powered by memberlist gossip) determines which node owns which shards.

```
┌──────────────┐    ┌──────────────┐    ┌──────────────┐
│  Write Node  │    │  Read Node   │    │  All Node    │
│  Distributor │    │  Frontend    │    │  All modules │
│  Ingester    │    │  Querier     │    │              │
└──────┬───────┘    └──────┬───────┘    └──────────────┘
       │                   │
       └─────── gRPC ──────┘
              ▲
       ┌──────┴──────┐
       │  Hash Ring  │  ← gossip-based node discovery
       └─────────────┘
```

The distributed components are:

| Component | What it does |
|---|---|
| **Distributor** | Receives writes, validates them, routes to the correct Ingester based on the hash ring. Handles replication if configured. |
| **Ingester** | Owns a set of shards and executes writes against them. Each shard lives on exactly one Ingester. |
| **Querier** | Executes read queries against its local shards and returns partial results. |
| **Frontend** | Coordinates read queries — sends them to all Queriers, collects results, handles retries and timeouts. |

Nodes discover each other through gossip (memberlist). When a node joins or leaves, the hash ring rebalances and shard ownership shifts accordingly.

### Security and Reliability

The cluster includes built-in production hardening:

- **Authentication** — JWT tokens (HS256) and API keys, with role-based access (admin, read, write)
- **Rate Limiting** — per-client request throttling to prevent abuse
- **Admission Control** — monitors memory and CPU; starts rejecting requests when the system is overloaded
- **Backpressure** — caps concurrent writes (default 100) and reads (default 1000) with queuing and timeouts
- **TLS** — optional encrypted connections
- **Backup/Restore** — scheduled or on-demand shard snapshots with compression and retention policies
- **Rebalancing** — when shards need redistribution, writes are paused, data is migrated, then writes resume

### Observability

- **Metrics** — Prometheus endpoint (`/metrics`) with counters, histograms, and gauges for requests, latency, shard health, and more
- **Tracing** — OpenTelemetry integration for distributed request tracing
- **Logging** — structured JSON logging via `slog`

## Project Structure

```
duckdb-cluster/
├── cmd/duckdb-cluster/        CLI entrypoint (init, start, status, index, alias, template, ism, backup, rebalance, migrate)
├── internal/
│   ├── api/                   HTTP server + REST handlers
│   ├── backup/                Backup/restore operations
│   ├── cluster/               Cluster controller
│   ├── config/                YAML configuration system
│   ├── distributor/           Write path validation and routing
│   ├── frontend/              Query coordination with retries
│   ├── grpc/                  gRPC client/server for distributed mode
│   ├── index/                 Index registry, aliases, templates, mappings, document ingestion, schema registry
│   ├── ingester/              Shard ownership and writes
│   ├── ism/                   Index State Management (policies, runner, cron)
│   ├── migration/             Schema migration versioning
│   ├── module/                Module lifecycle manager
│   ├── modules/               Module implementations (server, ingester, querier, distributor, frontend, admin)
│   ├── observability/         Metrics, tracing, structured logging
│   ├── querier/               Query execution and consistency
│   ├── rebalance/             Shard rebalancing with write gate
│   ├── reliability/           Admission control, backpressure, degradation, timeouts
│   ├── ring/                  Consistent hash ring with memberlist gossip
│   ├── router/                Query classification, hash routing, result merging
│   ├── security/              JWT auth, RBAC, rate limiting, TLS
│   └── shard/                 Single shard wrapper + multi-shard manager
├── proto/                     Protobuf definitions (ingester, querier, common)
├── pkg/client/                Go client library with bulk indexer
├── examples/
│   ├── local/                 Single-node Docker Compose
│   ├── distributed/           Multi-node Docker Compose + UAT tests
│   └── demo/                  Full demo with observability stack
├── Dockerfile                 Multi-stage container build
├── Makefile                   Build, test, docker, compose targets
└── config.yaml                Generated config (after init)
```

## Configuration

Configuration is provided via YAML file. `config.yaml` is created by `init` and read by `start`. All settings have sensible defaults — you only need to specify what you want to override.

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
  max_query_length: 1048576
  replication_factor: 1

ingester:
  max_shards_per_instance: 10

querier:
  merge_strategy: duckdb       # duckdb | simple
  max_concurrent_queries: 100
  query_timeout: 60s
  read_consistency: one         # one | quorum | all

query_frontend:
  query_timeout: 60s
  max_retries: 3

ring:
  instance_id: instance-0
  instance_addr: localhost:9095
  memberlist:
    join_peers: []              # empty = single-node mode
    bind_addr: 0.0.0.0
    bind_port: 7946

security:
  authentication:
    enabled: false
    jwt_secret: ""
    token_expiration: 24h
    allow_anonymous: true
  rate_limit:
    enabled: false
    requests_per_second: 100
    burst: 200
    per_tenant: true
  tls:
    enabled: false
    cert_file: ""
    key_file: ""

observability:
  metrics:
    enabled: true
    namespace: duckdb_cluster
  tracing:
    enabled: false
    otlp_endpoint: localhost:4317
    service_name: duckdb-cluster
    sample_rate: 1.0
  logging:
    level: info
    format: text                # text | json

reliability:
  backpressure:
    enabled: true
    max_concurrent_writes: 100
    max_concurrent_reads: 1000
    max_queue_size: 1000
    queue_timeout: 10s
  admission_control:
    enabled: false
    max_memory_mb: 8192
    max_cpu_percent: 90

backup:
  storage_type: local
  local_path: ./data/backups
  compression: true
  retention: 7
  schedule_interval: 0s        # 0 = disabled

ism:
  enabled: false
  run_interval: 5m
  policy_dir: ""
```

### Target Modes

| Target | Components | Use Case |
|---|---|---|
| `all` | All modules | Single-node deployment (default) |
| `write` | server, distributor, ingester | Write-only node |
| `read` | server, query-frontend, querier | Read-only node |
| `backend` | server, admin, compactor | Admin/maintenance node |

## Docker Deployment

### Single Node

```bash
make compose-up          # start
make compose-down        # stop
```

Uses `examples/local/docker-compose.yaml`.

### Multi-Node Distributed

```bash
make compose-distributed-up    # start write + read + all-in-one nodes
make compose-distributed-down  # stop
make uat                       # run UAT test suite against the cluster
```

Uses `examples/distributed/docker-compose.yaml`.

### Demo with Observability

The `examples/demo/` directory includes a full demo environment with Prometheus, Jaeger, and Grafana pre-configured alongside the cluster.

## Development

```bash
make build                       # Build binary to bin/
make run                         # Build and start server
make test                        # Run all tests
make clean                       # Remove bin/ and data/
make proto                       # Regenerate gRPC protobuf code
make docker-build                # Build Docker image
make docker-run                  # Run Docker container
make compose-up                  # Start local Docker Compose
make compose-distributed-up      # Start distributed Docker Compose
make uat                         # Run UAT tests against distributed cluster
```

## Go Client Library

For Go applications, use the high-level client library with connection pooling, automatic retries, and efficient bulk indexing:

```go
import "github.com/chronicblondiee/duckdb-cluster/pkg/client"

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

- [duckdb-go/v2](https://github.com/duckdb/duckdb-go) — DuckDB driver
- [hashicorp/memberlist](https://github.com/hashicorp/memberlist) — gossip-based cluster membership
- [prometheus/client_golang](https://github.com/prometheus/client_golang) — Prometheus metrics
- [opentelemetry](https://opentelemetry.io/docs/languages/go/) — distributed tracing
- [grpc](https://grpc.io/docs/languages/go/) + [protobuf](https://protobuf.dev/) — inter-node gRPC transport
- [yaml.v3](https://gopkg.in/yaml.v3) — YAML config parsing
- [robfig/cron](https://github.com/robfig/cron) — cron expression parsing (ISM)
- Go stdlib for everything else (`net/http`, `database/sql`, `hash/fnv`, `log/slog`, `context`, `sync`)

## Features

### Core Clustering
- Hash-based sharding with FNV-1a
- Query classification (DDL, Read, Write)
- Write routing to single shard
- Read fan-out with result merging (DuckDB-powered merge for ORDER BY/LIMIT/aggregations)
- DDL broadcast to all shards
- Pagination, bulk operations, multi-query concurrent execution

### Index Management (Elasticsearch-inspired)
- Named indices with independent shard sets
- Index aliases (transparent multi-index routing)
- Index templates (glob-pattern auto-apply on creation)
- Dynamic schema detection and evolution
- JSON and Protobuf document ingestion
- Protobuf schema registry

### Index State Management (ISM)
- Policy-based state machine with configurable actions
- Actions: rollover, force merge, read-only, snapshot, delete, notification
- Conditions: min index age, min doc count, cron schedule
- Auto-attach policies to new indices via templates
- Background runner with retry and exponential backoff

### Distributed Mode (Loki-inspired)
- Module system with dependency resolution
- YAML configuration with hierarchical structure
- Component separation (Distributor, Ingester, Querier, Frontend)
- Consistent hash ring with memberlist gossip
- gRPC transport for inter-node communication
- Multiple deployment targets (all, write, read, backend)
- Replication with configurable factor

### Security
- JWT authentication (HS256) with configurable expiration
- API key registration and revocation
- Role-based access control (admin, read, write)
- Per-tenant and per-API-key rate limiting
- TLS/mTLS support

### Reliability
- Admission control (memory/CPU thresholds)
- Backpressure (concurrent write/read limits with queuing)
- Graceful degradation with error thresholds
- Configurable timeouts (query, write, replication, health check)

### Observability
- Prometheus metrics (`/metrics`) with shard, request, and replication gauges
- OpenTelemetry distributed tracing (OTLP export)
- Structured logging via `slog` (text or JSON format)

### Operations
- Backup/restore with compression and retention policies
- Schema migration versioning with automatic backup before migration
- Shard rebalancing with write gate and batch migration
- Go client library with bulk indexer
- Docker and Docker Compose deployment examples
- UAT and stress testing suites
