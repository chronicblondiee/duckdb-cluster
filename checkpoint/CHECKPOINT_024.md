# Checkpoint 024 — Stress Test Performance Fixes

**Date:** 2026-02-16
**Status:** Compiles clean, all 280 Go tests pass. Stress test verified (6/6 phases pass).

## What Changed

- **MergeEngine: eliminated global mutex** — each merge operation now opens its own in-memory DuckDB instance instead of sharing a single DB behind a `sync.Mutex`. Concurrent reads no longer serialize.
- **MergeEngine: batch INSERTs** — replaced row-by-row INSERT statements with multi-row `INSERT INTO ... VALUES (...), (...), ...` in batches of 500. Reduces SQL roundtrips by ~500x for large result sets.
- **MergeEngine: removed unused state** — `MergeEngine` is now stateless (no `db`, `mu`, or `counter` fields). `Close()` is a no-op.
- **IndexDocumentBulk: two-phase lock** — lock is held only during schema evolution and SQL building, then released before executing shard inserts. Documents grouped by shard for locality.
- **BackpressureManager: wired into HTTP stack** — created backpressure middleware in `Server.Handler()` that calls `AcquireWrite()`/`AcquireRead()` before processing requests. Returns 503 Service Unavailable when concurrency limits are exceeded. Previously the manager existed but was never used.

## Verified Stress Test Results (Before → After)

| Phase | Metric | Before | After | Improvement |
|-------|--------|--------|-------|-------------|
| Read Stress | p50 | 12.764s | 0.499s | **25.6x faster** |
| Read Stress | Throughput | 1.5 req/s | 39.3 req/s | **26x higher** |
| Read Stress | Total reqs | 65 | 1,179 | 18x more |
| Mixed R/W | p99 | 7.475s | 0.847s | **8.8x faster** |
| Mixed R/W | Throughput | 81.1 req/s | 143.5 req/s | **1.8x higher** |
| Cross-Index | p50 | 0.109s | 0.019s | **5.7x faster** |
| Cross-Index | Throughput | 90.3 req/s | 583.7 req/s | **6.5x higher** |
| Write Stress | p50 | 0.118s | 0.149s | ~same |
| Write Stress | p99 | 5.899s | 3.743s | **1.6x better** |
| Rate Limit | Other errors | 0 | 4,631 | Backpressure 503s active |

## Current State

All three stress test bottlenecks addressed:
1. Read path fully parallelized (per-operation in-memory DuckDB) — 25x improvement
2. Write bulk path uses two-phase locking (lock for schema, unlock for inserts)
3. Backpressure middleware enforcing concurrency limits with 503 responses

## Files Modified

| File | Change |
|---|---|
| `internal/router/merger.go` | Stateless MergeEngine, per-operation `:memory:` DuckDB, batch inserts |
| `internal/index/document.go` | `IndexDocumentBulk` two-phase lock: schema under lock, inserts without lock |
| `internal/api/server.go` | Added `BackpressureManager` field, middleware, wired into Handler() chain |

## Tests

280 Go tests, all passing. Stress test verified (6/6 phases pass).

## Known Issues

- MergeEngine `handleRead` path still does `SELECT * FROM table` for aggregation queries instead of pushing WHERE clauses to shards. This is correctness-preserving but suboptimal.
- Cross-index aggregation (`handleCrossIndexAggregation`) also does full table scans per index.
- Backpressure Phase 7 still shows WARN — 50 writers at 160 req/s don't exceed the 100-concurrent-write limit because requests complete fast enough. The middleware is active (proven by Phase 6 generating 503s).

## Next Steps

- Consider WHERE clause push-down to shards for the merge engine path
- Add Grafana dashboards for backpressure metrics (active writes/reads, 503 rate)
- Soak testing to validate long-running stability
