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
duckdb-cluster init [--shards N] [--data-dir PATH]
duckdb-cluster start [--addr :8080] [--data-dir PATH]
duckdb-cluster status [--addr :8080]
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
├── cmd/duckdb-cluster/     CLI entrypoint
├── internal/
│   ├── shard/              Single shard wrapper + multi-shard manager
│   ├── router/             Query classification, hash routing, result merging
│   ├── cluster/            Cluster controller + JSON config
│   └── api/                HTTP server + REST handlers
├── Makefile
└── cluster.json            Generated config (after init)
```

## Configuration

`cluster.json` is created by `init` and read by `start`:

```json
{
  "data_dir": "./data",
  "num_shards": 3,
  "listen_addr": ":8080"
}
```

## Development

```bash
make build    # Build binary to bin/
make run      # Build and start server
make test     # Run all tests
make clean    # Remove bin/ and data/
```

## Dependencies

- [duckdb-go/v2](https://github.com/duckdb/duckdb-go) — DuckDB driver for Go
- Go stdlib only for everything else (net/http, database/sql, hash/fnv, encoding/json)
