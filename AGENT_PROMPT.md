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
- **Distributed systems fundamentals** — hash-based partitioning (FNV-1a), fan-out/scatter-gather reads, DDL broadcast, shard lifecycle management. No consensus protocol needed — this is single-node with multiple local shard files.
- **Go concurrency** — goroutines, `sync.WaitGroup`, `sync.RWMutex` for safe concurrent shard access. Parallel query fan-out and result collection.
- **Go HTTP servers** — stdlib `net/http`, `http.ServeMux` with Go 1.22+ method routing (`"POST /query"`, `"DELETE /admin/shards/{id}"`), `httptest` for handler testing, graceful shutdown with signal handling.
- **Go `database/sql`** — the standard DB interface that `duckdb-go/v2` implements. Dynamic column scanning with `rows.Columns()` and `[]any` scan targets for schema-agnostic query results.

When making changes, reason about: shard consistency (DDL must hit all shards), routing correctness (same partition key must always map to same shard), and concurrency safety (readers and writers accessing the shard list).

---

## 3. Project Layout

```
duckdb-cluster/
├── cmd/duckdb-cluster/main.go      CLI: init, start, status
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
│   └── api/
│       ├── server.go               HTTP server + graceful shutdown
│       └── handlers.go             REST endpoint handlers
├── checkpoint/                     Agent checkpoint summaries
├── Makefile                        build, run, test, clean
├── go.mod / go.sum                 Module: github.com/chronicblondiee/duckdb-cluster
└── README.md                       User-facing docs
```

---

## 4. Constraints

- **Go only.** Only external dependency: `github.com/duckdb/duckdb-go/v2`. Everything else is stdlib.
- **No frameworks.** HTTP via `net/http`, CLI via `flag`, logging via `log/slog`.
- **No over-engineering.** Don't add abstractions, interfaces, or config options unless the task specifically calls for them.
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
```

---

## 9. API Quick Reference

| Method | Path | Body | Description |
|---|---|---|---|
| POST | `/query` | `{"sql": "...", "partition_key": "..."}` | Execute SQL. `partition_key` required for writes. |
| GET | `/health` | — | `{"status": "healthy", "shard_count": N}` |
| GET | `/admin/shards` | — | List all shards |
| POST | `/admin/shards` | — | Add a new shard |
| DELETE | `/admin/shards/{id}` | — | Remove a shard |
