# Checkpoint 020 — Multi-Node Compose & UAT Scripts

**Date:** 2026-02-15
**Status:** Compiles clean, all tests pass (280 tests). No code changes — infrastructure/tooling only.

## What Changed

- Added 3-node distributed Docker Compose example in `examples/distributed/`
- Created per-node config files for write, read, and all (monolithic gateway) roles
- Added automated UAT test script with 27 test cases covering health, index CRUD, document ingestion, queries, cross-index queries, aliases, templates, metrics, and cleanup
- Added Makefile targets: `compose-distributed-up`, `compose-distributed-down`, `uat`
- Updated `AGENT_PROMPT.md` project layout to include `examples/distributed/`

## Current State

The project now has two Docker Compose setups:
- **`examples/local/`** — Single-node monolithic mode (existing)
- **`examples/distributed/`** — 3-node cluster with write-node (target: write, port 8081), read-node (target: read, port 8082), and all-node (target: all, port 8083) connected via gossip on a shared Docker network

The UAT script (`examples/distributed/test.sh`) starts the cluster, waits for health, runs 27 HTTP API tests against the all-node gateway, and tears down on exit. Run via `make uat`.

## New Files

| File | Description |
|---|---|
| `examples/distributed/docker-compose.yaml` | 3-service compose with cluster-net, health checks, depends_on |
| `examples/distributed/config-write.yaml` | Write-path config (target: write, gossip peers, distributor) |
| `examples/distributed/config-read.yaml` | Read-path config (target: read, gossip peers, querier) |
| `examples/distributed/config-all.yaml` | Monolithic gateway config (target: all, gossip peers, ISM enabled) |
| `examples/distributed/test.sh` | UAT test script (27 tests, requires curl + jq) |

## Modified Files

| File | Change |
|---|---|
| `Makefile` | Added compose-distributed-up, compose-distributed-down, uat targets |
| `AGENT_PROMPT.md` | Added examples/distributed/ to project layout tree |

## Tests

280 tests, all passing. No new Go tests (no code changes). UAT script adds 27 integration-level HTTP tests run via Docker.

## Known Issues

None.

## Next Steps

- Add CI pipeline (GitHub Actions) using the Dockerfile and UAT script
- Run `make uat` to validate the full distributed flow end-to-end
- Consider adding observability stack (Prometheus + Jaeger) to the distributed compose
