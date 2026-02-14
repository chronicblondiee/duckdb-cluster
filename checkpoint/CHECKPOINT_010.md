# Checkpoint 010 — Phase 7.3.3: Admin CLI Improvements

**Date:** 2026-02-14
**Status:** Compiles clean, all tests pass (151 tests)

## What Changed

Added CLI subcommands for version, migration, and backup management. All commands call HTTP endpoints on the running server (same pattern as `status`).

### New Commands

1. **`version`** — Shows binary version, state version, applied/pending migration counts. Falls back to build-time version if server unreachable.
2. **`migrate status`** — Displays migration status including pending migration list and last migration time.
3. **`migrate run`** — Triggers pending migrations, shows results with backup ID.
4. **`backup list`** — Lists all backups in a formatted table (ID, type, status, shards, timestamp).
5. **`backup create`** — Creates a new backup (`--type full|incremental`).
6. **`backup restore`** — Restores from a backup (`--id <backup-id>`).
7. **`backup delete`** — Deletes a backup (`--id <backup-id>`).

## Current State

The CLI now provides full admin access to version, migration, and backup operations without needing curl. All commands use `--addr` flag (default `:8080`) to connect to a running server.

## Files Modified

| File | Change |
|------|--------|
| `cmd/duckdb-cluster/main.go` | Added `cmdVersion`, `cmdMigrate` (status/run), `cmdBackup` (list/create/restore/delete), updated switch and `printUsage` |

## Tests

**Total: 151 tests passing (unchanged)**

No new tests added — these are CLI commands that call HTTP endpoints on a running server. The underlying HTTP handlers are already tested in `internal/api/`.

## Known Issues

None. All existing tests continue to pass.

## Next Steps

### Recommended: Phase 7.3.4 — Data Migration / Shard Rebalancing

- Move data between shards when adding/removing shards
- Rebalance partitions after topology changes
