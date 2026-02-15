# Checkpoint 018 — Dockerfile, Docker Compose, and Local UAT Pipeline

**Date:** 2026-02-15
**Status:** Compiles clean, all tests pass (280 tests)

## What Changed

- Added multi-stage Dockerfile for containerized builds
- Added Docker Compose setup in `examples/local/` for local UAT testing
- Added Makefile targets for Docker build/run/test and compose up/down
- Fixed authz middleware bug: `/health` and `/metrics` were blocked because auth middleware skipped setting user context but authz still required it

## New Files

| File | Description |
|---|---|
| `Dockerfile` | Multi-stage build: golang:1.25-bookworm builder, debian:bookworm-slim runtime, non-root user, CGO enabled |
| `.dockerignore` | Excludes bin/, data/, checkpoint/, .git/, markdown, .duckdb files |
| `examples/local/docker-compose.yaml` | Single-service compose with health check, named volume, config mount |
| `examples/local/config.yaml` | UAT config: 3 shards, ISM enabled, metrics on |

## Modified Files

| File | Change |
|---|---|
| `Makefile` | Added docker-build, docker-run, docker-test, compose-up, compose-down targets |
| `internal/security/middleware.go` | Added `/health` and `/metrics` skip to `HTTPAuthzMiddleware` (matching auth skip list) |

## Makefile Targets

| Target | Description |
|---|---|
| `make docker-build` | Build image as `duckdb-cluster:<git-version>` |
| `make docker-run` | Build + run with port 8080 and named volume |
| `make docker-test` | Build + run `version` as smoke test |
| `make compose-up` | Build + start compose in detached mode |
| `make compose-down` | Stop containers and remove volumes |

## Tests

280 tests, all passing. No new tests added (infrastructure-only change).

## Known Issues

None.

## Next Steps

- Add CI pipeline (GitHub Actions) using the Dockerfile
- Add multi-node compose example for distributed mode testing
- Consider adding a test script to `examples/local/` for automated UAT scenarios
