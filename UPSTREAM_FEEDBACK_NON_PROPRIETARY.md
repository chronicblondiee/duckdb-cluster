# Non-proprietary feedback for upstream `duckdb-cluster`

This file tracks open-source hardening opportunities observed while integrating with
`duckdb-cluster` in Golden Eye.

## Routing and SQL parsing
- Improve routing keyword extraction to tolerate SQL prologues:
  - leading comments (`--`, `/* ... */`)
  - top-level `WITH ...` queries (CTEs)
  - parenthesized or nested statement fragments
- Share write-detection logic between API write-gating and router routing to avoid
  divergence between read/write behavior.

## Build compatibility
- Current upstream `go.mod` and Docker build pinned to an unreleased Go point version.
  This makes local reproducible builds harder in environments without a future Go toolchain.
  A more conservative baseline (or explicit image arg) would reduce friction.

## API ergonomics
- Consider returning clearer validation for unsupported SQL forms (current behavior is a
  generic “unsupported SQL statement” branch when statement extraction fails).
- Add an explicit parser test matrix for:
  - comment-prefixed SQL
  - `WITH ... SELECT` / `WITH ... INSERT`
  - multi-statement input handling policy.

## Performance visibility
- Add basic metrics for routing fallback paths (unsupported keyword fallback + CTE parse depth
  fallbacks), to make it easy to spot parser misses in production traffic.
