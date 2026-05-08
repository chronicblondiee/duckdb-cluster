# Checkpoint 025 — Connection Pool Tuning + Read Bottleneck Analysis

**Date:** 2026-02-17
**Status:** Compiles clean, all 280 Go tests pass. Stress test shows modest read improvement but bottleneck remains.

## What Changed

- **DuckDB connection pool tuning** — set `MaxOpenConns=20`, `MaxIdleConns=20`, `ConnMaxLifetime=0` in `Shard.Open()`. Previously defaulted to 2 idle connections, causing massive contention with 20 concurrent readers.
- **Deep performance investigation** — created synthetic tests to isolate the read bottleneck. Confirmed DuckDB itself is extremely fast (<1ms queries), but cluster achieves only 4.1 req/s (1700x slowdown).

## Stress Test Results (1-Shard Config)

### Before Connection Pool Tuning (Checkpoint 024)
| Metric | Value |
|--------|-------|
| Read throughput | 2.9 req/s |
| Read p50 latency | 11.223s |
| Read p95 latency | 11.818s |
| Mixed R/W throughput | 112.6 req/s |
| Mixed R/W p50 | 0.029s |

### After Connection Pool Tuning (Checkpoint 025)
| Metric | Value | Change |
|--------|-------|--------|
| Read throughput | 4.1 req/s | **+41%** |
| Read p50 latency | 8.271s | **-26%** |
| Read p95 latency | 8.781s | **-26%** |
| Mixed R/W throughput | 114.7 req/s | +2% |
| Mixed R/W p50 | 0.027s | -7% |

## Performance Investigation Findings

Created synthetic benchmarks to isolate bottlenecks:

### Test 1: Raw DuckDB Query Performance
- Dataset: 250K rows (similar to stress test)
- Queries: COUNT(*), AVG(), LIMIT, WHERE, GROUP BY
- **Results:**
  - SELECT COUNT(*): **0.26ms**
  - SELECT AVG(): **0.23ms**
  - SELECT with LIMIT: **0.20ms**
  - GROUP BY: **2.7ms**

### Test 2: Concurrent File-Based DuckDB Reads
- 20 concurrent readers (matching stress test)
- 7 queries each (140 total)
- Same connection pool settings as production
- **Results:**
  - Total time: **19ms**
  - Throughput: **7270 req/s**
  - Average latency: **137µs**

### Comparison: Cluster vs Raw
| Metric | Raw DuckDB | Cluster | Slowdown Factor |
|--------|------------|---------|-----------------|
| Throughput | 7270 req/s | 4.1 req/s | **1700x slower** |
| p50 Latency | 0.14ms | 8271ms | **59,000x slower** |

## Root Cause Analysis

**Confirmed NOT the bottleneck:**
- ❌ DuckDB query execution (< 1ms per query)
- ❌ File I/O contention (tested with 20 concurrent readers)
- ❌ Connection pool exhaustion (fixed in this checkpoint)

**Likely bottleneck (unconfirmed):**
- ✅ HTTP request path serialization or blocking
- ✅ Middleware overhead (auth, backpressure, observability)
- ✅ JSON encoding/decoding of large result sets
- ✅ Some unknown synchronization point in the request path

## Current State

All previous optimizations working:
1. Write throughput: ~156 req/s (5-6x improvement from baseline)
2. Backpressure middleware active (enforcing concurrency limits)
3. Single-shard fast path active (bypasses merge engine)
4. WHERE/ORDER BY/LIMIT push-down working
5. Batch INSERTs (500 → 5000 rows per batch in merge engine)
6. Two-phase locking in IndexDocumentBulk
7. Conditional SaveCatalog (only when schema changes)
8. Connection pool properly tuned (20 connections)

Read performance improved but still severely bottlenecked. The 8-second p50 latency for simple queries that should complete in < 1ms indicates a fundamental issue in the request processing pipeline.

## Files Modified

| File | Change |
|------|--------|
| `internal/shard/shard.go` | Added connection pool tuning: `SetMaxOpenConns(20)`, `SetMaxIdleConns(20)`, `SetConnMaxLifetime(0)` |
| `internal/router/router.go` | Added debug comment (commented out) for single-shard fast path verification |

## Tests

280 Go tests, all passing.

**Stress test (1-shard config):**
- Write Stress: **PASS** (156.0 req/s, 0% errors)
- Read Stress: **PASS** (4.1 req/s, 0% errors, p50=8.3s)
- Mixed Read/Write: **PASS** (114.7 req/s, 0% errors)
- Cross-Index Fan-out: **PASS** (688.1 req/s, 0% errors)
- Rate Limit: **PASS** (rate limiting engaged)
- Backpressure: **WARN** (no 503s observed - expected for low write rate)

## Known Issues

- **Read performance severely degraded:** 8-second p50 latency for queries that should complete in < 1ms. The bottleneck is in the application layer, not DuckDB.
- **1700x slowdown unaccounted for:** Synthetic tests show DuckDB + concurrency is not the issue. Need to profile HTTP request path to identify serialization point.
- **Standalone benchmark misleading:** Standalone config shows high "throughput" but 98% connection errors, making comparisons invalid.

## Next Steps

### Immediate (High Priority)
- **Profile the request path** — add timing instrumentation to identify where 8+ seconds is being spent per query
- **Check for global locks** — search for `sync.Mutex` that might be serializing all requests
- **Test direct shard access** — bypass router/handler to isolate where slowdown occurs
- **Monitor goroutine count** — verify backpressure isn't over-throttling

### Medium Priority
- Add Grafana dashboards for backpressure metrics
- Consider connection pooling at HTTP layer (keep-alive, connection reuse)
- Investigate if JSON encoding of result sets is slow for large responses

### Low Priority  
- Soak testing for long-running stability
- Distributed aggregation (push partial aggregates to shards)
