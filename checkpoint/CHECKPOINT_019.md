# Checkpoint 019 — Sub-Agent Architecture

**Date:** 2026-02-15
**Status:** Compiles clean, all tests pass (280 tests). No code changes — documentation only.

## What Changed

- Created sub-agent system with 3 specialized agents in `checkpoint/agents/`
- Refactored `AGENT_PROMPT.md` from monolithic prompt to lightweight router
- Moved domain expertise, data flows, API references, and testing patterns into sub-agents

## Current State

The agent prompt system now uses a hub-and-spoke model:
- **Hub** (`AGENT_PROMPT.md`): project overview, layout, constraints, workflow, checkpoint/commit conventions, sub-agent delegation decision tree
- **Data Plane** (`checkpoint/agents/data-plane.md`): index, ISM, shard, router, cluster expertise
- **Control Plane** (`checkpoint/agents/control-plane.md`): security, reliability, observability, distributed, operations expertise
- **API & Integration** (`checkpoint/agents/api-integration.md`): HTTP server, handlers, CLI, client, config expertise

Each sub-agent carries: owned packages table, domain expertise, data flow diagrams, relevant API endpoints, testing patterns, and integration point documentation.

## New Files

| File | Description |
|---|---|
| `checkpoint/agents/data-plane.md` | Sub-agent for index/ISM/shard/router/cluster domain |
| `checkpoint/agents/control-plane.md` | Sub-agent for security/reliability/observability/distributed/ops domain |
| `checkpoint/agents/api-integration.md` | Sub-agent for HTTP server/handlers/CLI/client/config domain |

## Modified Files

| File | Change |
|---|---|
| `AGENT_PROMPT.md` | Refactored: replaced domain expertise (section 2) with sub-agent delegation decision tree; removed data flow reference (section 8) and API quick reference (section 9); added cross-domain workflow; updated project layout tree to include `checkpoint/agents/` and `Dockerfile` |

## Tests

280 tests, all passing. No new tests (no code changes).

## Known Issues

None.

## Next Steps

- Add CI pipeline (GitHub Actions) using the Dockerfile
- Add multi-node compose example for distributed mode testing
- Consider adding a test script to `examples/local/` for automated UAT scenarios
