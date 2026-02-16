# Checkpoint 023 — Stress/Load Testing Script

**Date:** 2026-02-16
**Status:** Compiles clean, all Go tests pass (280 tests). Demo UAT passes (85 tests). Stress test verified against live cluster (all 6 phases pass).

## What Changed

- Created `examples/demo/stress.sh` — a self-contained bash stress/load testing script for the demo cluster
- 678 lines, 6 test phases, follows the same patterns and conventions as `test.sh`
- Uses only `curl` + bash (no external load tools), consistent with the project's philosophy

## Stress Test Phases

| # | Phase | Description |
|---|-------|-------------|
| 1 | Setup | Health check, auth token, create `stress-test` index (3 shards) |
| 2 | Write Stress | Concurrent single-doc and bulk writes for `$DURATION` seconds |
| 3 | Read Stress | Concurrent SQL queries (point lookups, aggregations, scans) |
| 4 | Mixed Read/Write | Simultaneous writers and readers to test contention |
| 5 | Cross-Index Fan-out | Create 3 indices, concurrent cross-index queries |
| 6 | Rate Limit Validation | Burst 50 workers to verify 429 responses appear |
| 7 | Backpressure Validation | Flood writes, verify 503 backpressure and recovery |

## Configuration (env-overridable)

| Variable | Default | Description |
|----------|---------|-------------|
| `WRITE_CONCURRENCY` | 10 | Parallel writer processes |
| `READ_CONCURRENCY` | 20 | Parallel reader processes |
| `DURATION` | 30 | Seconds per test phase |
| `BULK_SIZE` | 100 | Documents per bulk request |
| `WRITE_URL` | `http://localhost:8081` | Write node |
| `READ_URL` | `http://localhost:8082` | Read node |
| `ALL_URL` | `http://localhost:8083` | All-in-one node |

## Metrics Reported Per Phase

- Total requests, successes, rate-limited (429), HTTP errors, connection errors
- Requests/second (throughput)
- p50, p95, p99 latency (from `curl -w '%{time_total}'`, only for 2xx responses)
- Final summary table across all phases

## Verified Results (DURATION=15, reduced concurrency)

| Phase | OK | 429 | Err | Req/s | p50 | Verdict |
|-------|----|-----|-----|-------|-----|---------|
| Write Stress | 497 | 0 | 0 | 29.2 | 0.058s | PASS |
| Read Stress | 61 | 144 | 0 | 3.4 | 2.896s | PASS |
| Mixed R/W | 2323 | 0 | 0 | 110.6 | 0.010s | PASS |
| Cross-Index Fan-out | 1488 | 0 | 0 | 99.2 | 0.049s | PASS |
| Rate Limit | 1322 | 17002 | 0 | 3664.8 | — | PASS |
| Backpressure | 804 | 0 | 0 | 160.8 | 0.003s | PASS |

## New Files

| File | Description |
|---|---|
| `examples/demo/stress.sh` | Stress/load testing script (678 lines, 7 phases) |

## Key Implementation Details

- Workers are bash background processes with shared results directory (`mktemp -d`)
- Each request logs HTTP status + latency; workers track ok/limited/fail/err separately
- 429 (rate-limited) responses excluded from error rate calculation
- Workers back off with `sleep 0.01` on 429 to avoid overwhelming the cluster
- 3-second cooldown between phases for server recovery
- `set +e` in all worker functions to prevent `set -e` from killing subshells on curl failures
- Index creation uses `partition_key_field: "id"` matching the generated document structure
- All SQL queries use `_docs` table name (DuckDB convention)

## Tests

280 Go tests, all passing. 85 demo UAT tests, all passing. Stress test verified (6/6 phases pass).

## Known Issues

None.

## Next Steps

- Add CI pipeline (GitHub Actions) for automated demo UAT
- Add Grafana dashboards for security metrics (auth failures, rate limiting)
- Soak testing (long-duration runs to check for memory leaks or degradation)
