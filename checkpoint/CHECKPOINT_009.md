# Checkpoint 009 — Phase 7.3.2: Rolling Upgrades (Version & Migration)

**Date:** 2026-02-14
**Status:** Compiles clean, all tests pass (151 tests = 139 existing + 12 new migration tests)

## What Changed

Implemented Phase 7.3.2 - Version management and schema migration system for production upgrades.

### Major Changes

1. **Version Management** — Build-time version embedding via `-ldflags`, SemVer parsing and comparison
2. **Schema Migration System** — Migration registry, state persistence (JSON), sequential per-shard application
3. **Auto-Backup Before Migration** — Uses existing BackupManager to create safety backup before applying changes
4. **Crash-Safe Progress** — State persisted after each migration; partial failures don't lose progress
5. **API Endpoints** — `GET /admin/version` (status) and `POST /admin/migrate` (trigger)
6. **Startup Auto-Migration** — Pending migrations run automatically on server start

## Current State

The project now supports versioned schema migrations across all shards. On startup (or via API), the system:
1. Checks for pending migrations
2. Creates an auto-backup if any are pending
3. Applies each migration to every shard sequentially
4. Persists state after each migration
5. Reports results (with backup ID for rollback if failure occurs)

Migrations are registered as Go functions in `cmd/duckdb-cluster/main.go:registerMigrations()`.

## Files Created

| File | Purpose |
|------|---------|
| `internal/migration/version.go` | SemVer parsing, build-time `Version` variable |
| `internal/migration/migration.go` | Migration manager: registry, state, runner |
| `internal/migration/migration_test.go` | 12 tests covering parsing, state, execution |
| `internal/api/handlers_migration.go` | HTTP handlers for version + migrate endpoints |

## Files Modified

| File | Change |
|------|--------|
| `internal/api/server.go` | Added `migrationManager` field, `SetMigrationManager()`, 2 new routes |
| `internal/modules/server.go` | Added `migrationManager` field + setter, passes to api.Server on Init |
| `cmd/duckdb-cluster/main.go` | Creates migration manager, runs pending on startup, wires to server module |
| `Makefile` | Added `VERSION` and `LDFLAGS` for build-time version injection |

## Tests

**Total: 151 tests passing (139 existing + 12 new)**

### New Migration Tests (12 tests)

1. `TestParseSemVer` — valid/invalid version strings (subtests)
2. `TestSemVerLess` — version comparison (subtests)
3. `TestSemVerEqual` — equality check
4. `TestMigrationStateRoundTrip` — save/load JSON state
5. `TestMigrationStateMissing` — missing file returns empty state
6. `TestAtomicStateWrite` — tmp file cleaned up after rename
7. `TestPendingMigrations` — diff registered vs applied
8. `TestRunPendingNoMigrations` — no-op when nothing pending
9. `TestRunPendingAppliesAll` — creates table on all shards
10. `TestRunPendingIdempotent` — second run is no-op
11. `TestRunPendingPartialFailure` — first migration saved, second fails
12. `TestGetStatus` — API status with applied/pending counts

## Known Issues

None. All existing tests continue to pass.

## Next Steps

### Recommended: Phase 7.3.3 — Admin CLI Improvements

- Enhanced CLI commands for migration status, manual backup, version info
- `duckdb-cluster migrate status` / `duckdb-cluster migrate run`
- `duckdb-cluster version`

### Alternative: Phase 7.3.4 — Data Migration / Shard Rebalancing
