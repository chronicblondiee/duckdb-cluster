# Checkpoint 012 — Phase 7.3.5: Auto-Rebalance, Write Pausing, SSE Streaming

**Date:** 2026-02-14
**Status:** Compiles clean, all tests pass (165 tests)

## What Changed

Added three operational features to the rebalance system:

### 1. Write Pausing During Rebalance
- `WriteGate` type (atomic bool) pauses/resumes write acceptance
- Rebalancer acquires gate at start of `Execute()`, releases via `defer` on completion
- `handleQuery` rejects INSERT/UPDATE/DELETE with 503 when gate is paused
- `handleBulk` rejects all statements with 503 when gate is paused
- `handleMultiQuery` rejects if any query is a write and gate is paused
- Reads and DDL are unaffected during rebalance

### 2. SSE Progress Streaming
- Pub/sub subscriber system on `Rebalancer`: `Subscribe()` returns buffered channel + unsubscribe closure
- `setStatus()` auto-notifies all subscribers (non-blocking send)
- `GET /admin/rebalance/stream` endpoint streams `text/event-stream` with JSON status updates
- Sends current status immediately on connect, then streams updates until completion
- Closes on client disconnect or rebalance completion

### 3. Auto-Rebalance on Shard Add/Remove
- `POST /admin/shards` accepts optional `{"rebalance": {"partition_key_column": "..."}}` body
  - If present, launches rebalance in background after shard creation
  - Response includes `"rebalance": "started"`
- `DELETE /admin/shards/{id}` accepts optional rebalance config in body
  - If present, runs rebalance synchronously to N-1 shards before removal
  - After data drains, removes the last shard (now empty)
- Empty body preserves existing behavior (no breaking changes)

## Current State

Full rebalance lifecycle automation. Shards can be added/removed with optional automatic data redistribution in a single API call. Writes are safely paused during rebalance. Progress is observable in real-time via SSE streaming or polling.

## Files Modified

| File | Change |
|------|--------|
| `internal/rebalance/rebalance.go` | Added `WriteGate` type, `WriteGateRef()` accessor, subscriber pub/sub (`Subscribe`, `notify`), modified `setStatus` to auto-notify |
| `internal/rebalance/executor.go` | Added `writeGate.Pause()`/`defer Resume()` in `Execute()` |
| `internal/api/handlers.go` | Added `addShardRequest`/`removeShardRequest` types, modified `handleAddShard`/`handleRemoveShard` for optional auto-rebalance, added `isWriteSQL` helper, added write gate checks in `handleQuery`/`handleBulk`/`handleMultiQuery` |
| `internal/api/handlers_rebalance.go` | Added `handleRebalanceStream` SSE handler |
| `internal/api/server.go` | Registered `GET /admin/rebalance/stream` route |
| `internal/rebalance/rebalance_test.go` | Added `TestWriteGate`, `TestWriteGateDuringExecute`, `TestSubscribeReceivesUpdates` |

## Tests

**Total: 165 tests passing (+3 from last checkpoint)**

New tests:
- `TestWriteGate` — Verify Pause/Resume/IsPaused state transitions
- `TestWriteGateDuringExecute` — Verify gate is unpaused after Execute completes
- `TestSubscribeReceivesUpdates` — Verify subscriber receives planning/running/completed state progression

## Known Issues

None.

## Next Steps

- API handler tests for write rejection during rebalance (503 responses)
- API handler tests for auto-rebalance on add/remove endpoints
- CLI commands for SSE stream monitoring (`rebalance watch`)
- Connection pooling for production workloads
