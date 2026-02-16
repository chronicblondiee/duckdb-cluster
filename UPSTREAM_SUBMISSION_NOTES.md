# Upstream submission package (`duckdb-cluster`)

## What to open as PR

Suggested PR title:
`Harden SQL keyword detection and improve build/runtime compatibility`

### Changes to include

1. **Router keyword extraction hardening**
- Make top-level SQL statement detection robust to:
  - leading/trailing whitespace
  - line and block comments
  - parenthesized subexpressions and quoted strings
  - CTE (`WITH`) wrappers by routing on the actual read/write statement after the CTE prologue
- Export a shared write-operation predicate to avoid policy drift.

2. **API write gating parity**
- Reuse router-level write detection in API handlers so `writes paused` checks match routing behavior exactly.

3. **Build/deployment compatibility**
- Use a configurable Go builder image tag (default to a stable current version),
  and keep `go.mod` language version aligned with commonly available toolchains for broader reproducibility.

4. **Operational hardening (optional follow-up)**
- Add parser-focused test coverage for comment-prefixed SQL, CTE statements, and unsupported token detection.
- Add metrics for parser fallback and unsupported keyword cases.

## Evidence in this repository

- `services/duckdb-cluster/internal/router/router.go`
- `services/duckdb-cluster/internal/api/handlers.go`
- `services/duckdb-cluster/Dockerfile`
- `services/duckdb-cluster/go.mod`
- `services/duckdb-cluster/UPSTREAM_FEEDBACK_NON_PROPRIETARY.md`

## Non-proprietary scope note

Nothing in this submission references customer data or Golden Eye internals; all proposals are generic service hardening and compatibility improvements suitable for upstream OSS consumption.
