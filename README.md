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

# Query all shards
curl -X POST localhost:8080/query \
  -d '{"sql": "SELECT * FROM users"}'
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

### GET /health

```json
{"status": "healthy", "shard_count": 3}
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
