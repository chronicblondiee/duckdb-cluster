# Checkpoint 015 — Cross-Index Aggregation Queries

**Date:** 2026-02-15
**Status:** Compiles clean, all tests pass (224 tests)

## What Changed

Added proper cross-index aggregation support. Previously, cross-index queries (`"index": "idx-a,idx-b"` or `"index": "logs-*"`) with aggregation functions (COUNT, SUM, AVG, GROUP BY, ORDER BY, LIMIT, DISTINCT) returned incorrect results because per-index aggregated results were simply concatenated. Now, a two-phase approach collects raw data from each index, then re-executes the original query against the combined dataset using a server-level MergeEngine.

**1. Exported router helpers** (`internal/router/router.go`)
- `requiresMergeEngine` → `RequiresMergeEngine` (detects aggregation/ordering/limit keywords)
- `extractTableName` → `ExtractTableName` (pulls table name from SELECT queries)

**2. MergeAndQueryFromMaps** (`internal/router/merger.go`)
- New `MergeEngine.MergeAndQueryFromMaps()` method works with `[]map[string]any` rows instead of `*shard.QueryResultSet`
- Infers DuckDB types from Go values via `inferDuckDBType()`
- Creates temp table, inserts all rows, re-executes original query, returns aggregated results + column names

**3. Server-level MergeEngine** (`internal/api/server.go`)
- Added `mergeEngine *router.MergeEngine` field to `Server`
- Initialized in `NewServer()`, closed during shutdown

**4. Aggregation-aware cross-index handler** (`internal/api/handlers_cross_index.go`)
- `handleCrossIndexQuery` detects aggregation queries via `RequiresMergeEngine()`
- Delegates to new `handleCrossIndexAggregation()` for two-phase merge
- Extracted `fanOutToIndices()` helper for parallel query execution across indices
- Non-aggregation queries keep existing concatenation behavior (no regression)

## Current State

Cross-index queries now correctly handle:
- `COUNT(*)`, `SUM()`, `AVG()`, `MIN()`, `MAX()` across indices
- `GROUP BY` with proper category merging across indices
- `ORDER BY` + `LIMIT` applied globally (not per-index)
- `DISTINCT` across indices
- AVG computed from raw values (not average-of-averages)

## Files Modified

| File | Change |
|---|---|
| `internal/router/router.go` | Exported `RequiresMergeEngine()` and `ExtractTableName()` |
| `internal/router/merger.go` | Added `MergeAndQueryFromMaps()` and `inferDuckDBType()` |
| `internal/api/server.go` | Added `mergeEngine` field, import, init, shutdown cleanup |
| `internal/api/handlers_cross_index.go` | Added aggregation branching, `handleCrossIndexAggregation()`, `fanOutToIndices()` |
| `internal/api/handlers_cross_index_test.go` | Added 5 aggregation tests + `createIndexWithData` helper |

## Tests

**Total: 224 tests passing (+5 from last checkpoint)**

New tests:
- `TestCrossIndexAggregationCount` — COUNT(*) across two indices
- `TestCrossIndexAggregationSum` — SUM across indices
- `TestCrossIndexAggregationAvg` — AVG from raw values (not average-of-averages)
- `TestCrossIndexAggregationGroupBy` — GROUP BY with overlapping categories
- `TestCrossIndexAggregationOrderByLimit` — Global ORDER BY + LIMIT

## Known Issues

None.

## Next Steps

- Index-aware ingester/querier/distributor in distributed mode
- Index lifecycle events (webhooks/notifications)
- Template mappings applied to new indices
- Index statistics and monitoring per index
- Cross-index WHERE clause push-down optimization
