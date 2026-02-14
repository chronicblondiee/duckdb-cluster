# Checkpoint 011 — Phase 7.3.4: Data Migration / Shard Rebalancing

**Date:** 2026-02-14
**Status:** Compiles clean, all tests pass (162 tests)

## What Changed

Added a shard rebalancing system that migrates data between shards after topology changes. When shards are added or removed, the hash routing formula (`FNV-1a % N`) changes, causing partition keys to map to different shards. The rebalancer scans all rows, recomputes correct shard placement, and moves misplaced rows via SQL-level SELECT/INSERT/DELETE.

### Core Features

1. **Plan computation** — Discovers tables, scans partition keys, computes which rows need to move and where. Can target a different shard count (e.g., N-1 for pre-removal rebalance).
2. **Batched execution** — Groups migrations by (table, source, target) and processes in configurable batch sizes. INSERT-before-DELETE ordering prevents data loss on crash.
3. **Progress tracking** — Thread-safe status with states: idle, planning, running, completed, failed. Tracks rows scanned, rows moved, tables done, and errors.
4. **HTTP API** — `POST /admin/rebalance` (async execute), `GET /admin/rebalance/status`, `POST /admin/rebalance/plan` (dry run).
5. **CLI commands** — `rebalance plan`, `rebalance run`, `rebalance status` with `--partition-key`, `--tables`, `--batch-size`, `--target-shards` flags.

### Workflows

- **Add shard:** `POST /admin/shards` → `POST /admin/rebalance {"partition_key_column":"id"}`
- **Remove shard:** `POST /admin/rebalance {"partition_key_column":"id","target_shard_count":N-1}` → wait for completion → `DELETE /admin/shards/{id}`

## Current State

Full data migration support. Shards can be added/removed with automatic data redistribution. The rebalancer handles arbitrary shard count changes, is idempotent (safe to run multiple times), and provides progress visibility via API and CLI.

## Files Modified

| File | Change |
|------|--------|
| `internal/rebalance/rebalance.go` | New: Core types (Config, Status, Plan, Migration), Rebalancer struct, GetStatus |
| `internal/rebalance/plan.go` | New: Plan computation — table discovery, column validation, partition key scanning, hash route comparison |
| `internal/rebalance/executor.go` | New: Migration execution — grouping, batched SELECT/INSERT/DELETE, progress tracking |
| `internal/rebalance/rebalance_test.go` | New: 7 tests covering plan, add-shard, remove-shard, idempotency, empty shards, validation |
| `internal/api/handlers_rebalance.go` | New: HTTP handlers for rebalance, rebalance/plan, rebalance/status |
| `internal/api/server.go` | Added rebalancer field, import, initialization, 3 route registrations |
| `cmd/duckdb-cluster/main.go` | Added rebalance command with plan/run/status subcommands |

## Tests

**Total: 162 tests passing (+11 from last checkpoint)**

New tests:
- `TestPlanComputation` — verifies plan correctness with same and different shard counts
- `TestRebalanceAfterAddShard` — end-to-end add shard + rebalance + verify placement
- `TestRebalanceBeforeRemoveShard` — rebalance to N-1 shards, verify source shard empty
- `TestRebalanceIdempotent` — running rebalance twice moves 0 rows second time
- `TestRebalanceEmptyShards` — no errors on empty tables
- `TestPlanInvalidColumn` — nonexistent column skipped gracefully
- `TestPlanMissingPartitionKeyColumn` — empty config returns error

## Known Issues

None.

## Next Steps

- Phase 7.3.5: Automated rebalance on shard add/remove (optional trigger in AddShard/RemoveShard handlers)
- Connection pooling / write pausing during rebalance for production safety
- Progress streaming via SSE or WebSocket for long-running rebalances
