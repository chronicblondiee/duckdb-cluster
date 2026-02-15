# DuckDB Cluster — Agent Instructions

You are the development agent for `duckdb-cluster`, a distributed clustering layer for DuckDB written in Go. This file is your entry point every session.

---

## 1. Getting Oriented

**Before doing anything else:**

1. Read the latest checkpoint in `checkpoint/` (files are numbered `CHECKPOINT_NNN.md` — read the highest number)
2. Run `go test ./...` to confirm the project is in a healthy state
3. Run `go build ./cmd/duckdb-cluster/` to confirm it compiles

The checkpoint tells you what exists, what works, and what's left to do. Do not re-read every source file — trust the checkpoint unless something fails.

---

## 2. Choosing the Right Sub-Agent

This project uses specialized sub-agents for different domains. **Before starting any task, read this section to delegate to the correct specialist.**

### Sub-Agents

| Agent | File | Scope |
|---|---|---|
| **Data Plane** | `checkpoint/agents/data-plane.md` | Index/ISM/shard/router/cluster — core data storage and query |
| **Control Plane** | `checkpoint/agents/control-plane.md` | Security/reliability/observability/distributed/operations — cluster management |
| **API & Integration** | `checkpoint/agents/api-integration.md` | HTTP server/handlers/CLI/client/config — external interface |

### Decision Tree

1. **Does the task involve indices, aliases, templates, ISM policies, document ingestion, shards, routing, or query merging?**
   → Read `checkpoint/agents/data-plane.md`
   → Files: `internal/index/`, `internal/ism/`, `internal/shard/`, `internal/router/`, `internal/cluster/`

2. **Does the task involve authentication, authorization, TLS, rate limiting, observability, metrics, tracing, distributed mode, gossip, gRPC, backup, migration, or rebalancing?**
   → Read `checkpoint/agents/control-plane.md`
   → Files: `internal/security/`, `internal/reliability/`, `internal/observability/`, `internal/ring/`, `internal/grpc/`, `internal/distributor/`, `internal/frontend/`, `internal/ingester/`, `internal/querier/`, `internal/backup/`, `internal/migration/`, `internal/rebalance/`, `internal/module/`, `internal/modules/`

3. **Does the task involve HTTP handlers, API endpoints, CLI commands, client library, configuration, or integration tests?**
   → Read `checkpoint/agents/api-integration.md`
   → Files: `internal/api/`, `internal/config/`, `pkg/client/`, `cmd/duckdb-cluster/`, `internal/integration/`

4. **Does the task span multiple domains?**
   → Start with the **primary domain** agent (where the business logic lives)
   → Complete that domain's work first, then use the next agent for integration
   → Example: new index feature + API endpoint → start with Data Plane, then API & Integration

5. **Is the task a bug fix or test addition?**
   → Use the agent that owns the file being modified

### Keywords Quick Reference

| Data Plane | Control Plane | API & Integration |
|---|---|---|
| index, alias, template | security, auth, JWT, RBAC | HTTP, handler, endpoint |
| ISM, policy, state machine | TLS, rate limit, middleware | server, route, request |
| document, mapping, schema | metrics, prometheus, tracing | CLI, command, flag |
| shard, partition, routing | distributed, ring, gossip | client, bulk, config |
| query, fan-out, merge | gRPC, replication, consistency | YAML, integration test |
| DuckDB, DDL, table | backup, migration, rebalance | JSON, response, error |
| | reliability, admission, timeout | |

---

## 3. Project Layout

```
duckdb-cluster/
├── cmd/duckdb-cluster/
│   ├── main.go                     CLI: init, start, status, version, migrate, backup, rebalance, index, alias, template, ism
│   ├── cmd_index.go                CLI index subcommands (list, create, delete, get, close, open)
│   ├── cmd_alias.go                CLI alias subcommands (list, create, delete, get)
│   ├── cmd_template.go             CLI template subcommands (list, create, delete, get)
│   └── cmd_ism.go                  CLI ISM subcommands (list, create, delete, get, status, attach, detach, retry)
├── internal/
│   ├── shard/
│   │   ├── shard.go                Single DuckDB instance wrapper
│   │   └── manager.go              Multi-shard lifecycle + parallel ops
│   ├── router/
│   │   ├── router.go               Query classification + routing
│   │   ├── strategy.go             FNV hash-based shard selection
│   │   └── merger.go               Result merging from fan-out
│   ├── cluster/
│   │   ├── cluster.go              Cluster controller (Init/Start/Shutdown)
│   │   └── config.go               JSON config with defaults
│   ├── config/
│   │   └── config.go               Full config (server, distributed, module settings)
│   ├── api/
│   │   ├── server.go               HTTP server + graceful shutdown + route registration
│   │   ├── handlers.go             Core REST endpoint handlers (query, health, shards)
│   │   ├── handlers_index.go       Index CRUD + document ingestion + query routing
│   │   ├── handlers_alias.go       Alias HTTP handlers
│   │   ├── handlers_template.go    Template HTTP handlers
│   │   ├── handlers_cross_index.go Cross-index query fan-out + merge
│   │   ├── handlers_ism.go         ISM policy CRUD, attach/detach, status, retry
│   │   ├── handlers_auth.go        Authentication endpoints
│   │   ├── handlers_backup.go      Backup/restore endpoints
│   │   ├── handlers_migration.go   Migration status/run endpoints
│   │   └── handlers_rebalance.go   Rebalance plan/run/status/stream endpoints
│   ├── index/
│   │   ├── index.go                Multi-index logical namespaces with independent shards
│   │   ├── registry.go             Index CRUD, persistence, auto-migration from flat layout
│   │   ├── alias.go                Index aliases (transparent routing to single/multiple indices)
│   │   ├── template.go             Index templates (glob patterns, priority, auto-apply)
│   │   ├── mapping.go              Dynamic + explicit schema detection, DuckDB native types
│   │   ├── document.go             JSON/Protobuf document ingestion with schema evolution
│   │   ├── schema_registry.go      Protobuf schema management
│   │   └── validation.go           Index name + shard count validation
│   ├── ism/
│   │   ├── policy.go               ISM policy data model (states, actions, transitions)
│   │   ├── state.go                Per-index ISM state tracking
│   │   ├── validation.go           Policy structural validation
│   │   ├── manager.go              Policy CRUD, persistence, auto-attach
│   │   ├── runner.go               Background executor (actions, transitions, retries)
│   │   └── cron.go                 Cron expression matching
│   ├── security/
│   │   ├── auth.go                 JWT-style authentication
│   │   ├── authz.go                RBAC authorization
│   │   ├── middleware.go           Auth middleware
│   │   ├── ratelimit.go            Rate limiting
│   │   └── tls.go                  TLS support
│   ├── reliability/
│   │   ├── admission.go            Admission control (memory/CPU thresholds)
│   │   ├── backpressure.go         Backpressure mechanisms
│   │   ├── degradation.go          Graceful degradation
│   │   └── timeout.go              Timeout management
│   ├── observability/
│   │   ├── logger.go               Structured logging (slog)
│   │   ├── metrics.go              Prometheus metrics
│   │   ├── tracing.go              OpenTelemetry tracing
│   │   └── middleware.go           Observability middleware
│   ├── ring/
│   │   ├── ring.go                 Consistent hashing ring with memberlist gossip
│   │   └── health.go               Node health tracking
│   ├── modules/
│   │   ├── server.go               HTTP/gRPC server module
│   │   ├── admin.go                Admin operations module
│   │   ├── frontend.go             Frontend gateway module
│   │   ├── ingester.go             Ingester module (data writes)
│   │   ├── querier.go              Querier module (data reads)
│   │   └── distributor.go          Distributor module (replication)
│   ├── module/
│   │   └── module.go               Module interface with lifecycle management
│   ├── grpc/
│   │   ├── ingester_server.go      gRPC ingester server
│   │   ├── ingester_client.go      gRPC ingester client
│   │   ├── querier_server.go       gRPC querier server
│   │   └── querier_client.go       gRPC querier client
│   ├── distributor/
│   │   ├── distributor.go          Write distribution across replicas
│   │   └── replication.go          Replication logic
│   ├── frontend/
│   │   └── frontend.go             Request demultiplexing
│   ├── ingester/
│   │   └── ingester.go             Standalone ingester (distributed mode)
│   ├── querier/
│   │   ├── querier.go              Standalone querier (distributed mode)
│   │   └── consistency.go          Read consistency modes
│   ├── migration/
│   │   ├── migration.go            MigrationManager (register/pending/run)
│   │   └── version.go              Version tracking, persisted to _migrations.json
│   ├── rebalance/
│   │   ├── rebalance.go            WriteGate + pub/sub for SSE streaming
│   │   ├── executor.go             Rebalance plan execution with write gate
│   │   └── plan.go                 Shard migration planning
│   ├── backup/                     Backup/restore operations
│   └── integration/
│       └── distributed_test.go     Distributed mode integration tests
├── proto/
│   ├── common.proto                HealthRequest/HealthResponse
│   ├── ingester.proto              PushRequest/PushResponse
│   └── querier.proto               QueryRequest/QueryResponse
├── pkg/client/
│   ├── client.go                   HTTP client library for duckdb-cluster
│   └── bulk.go                     Bulk operations helper
├── checkpoint/
│   ├── CHECKPOINT_NNN.md           Agent checkpoint summaries
│   └── agents/                     Sub-agent prompts (data-plane, control-plane, api-integration)
├── Makefile                        build, run, test, clean, docker
├── Dockerfile                      Multi-stage container build
├── go.mod / go.sum                 Module: github.com/chronicblondiee/duckdb-cluster
├── SECURITY.md                     Security documentation
├── OBSERVABILITY.md                Observability guide
└── README.md                       User-facing docs
```

---

## 4. Constraints

- **Go only.** External dependencies are intentionally limited:
  - `github.com/duckdb/duckdb-go/v2` — DuckDB driver
  - `github.com/hashicorp/memberlist` — gossip-based cluster membership
  - `github.com/prometheus/client_golang` — Prometheus metrics
  - `go.opentelemetry.io/otel` — distributed tracing
  - `google.golang.org/grpc` + `google.golang.org/protobuf` — inter-node gRPC
  - `gopkg.in/yaml.v3` — YAML config parsing
  - `github.com/robfig/cron/v3` — cron expression parsing (ISM)
- **No frameworks.** HTTP via `net/http`, CLI via `flag`, logging via `log/slog`.
- **No over-engineering.** Don't add abstractions, interfaces, or config options unless the task specifically calls for them.
- **Two modes:** Monolithic (single-node, all-in-one) and Distributed (modular with separate ingester/querier/distributor/frontend roles).
- **Platform:** Arch Linux (CachyOS). Go installed via `pacman`.

---

## 5. Workflow

### Starting a task

1. Read the latest checkpoint (step 1 above)
2. Read the appropriate sub-agent prompt (step 2 above)
3. Understand what's being asked
4. Make the changes
5. Run `go build ./cmd/duckdb-cluster/` — must compile clean
6. Run `go test ./...` — all tests must pass
7. If you added new behavior, add tests for it
8. Update the checkpoint (see section 6)
9. Commit (see section 7)

### Cross-domain tasks

For tasks spanning multiple domains:
1. Start with the **primary domain** agent (where business logic lives)
2. Complete that domain's work and tests
3. Note in the checkpoint what other domains need (e.g., "Requires API endpoint in `handlers_index.go`")
4. Load the next domain's sub-agent and continue
5. Create a single checkpoint covering all work

### When things break

- Read the failing test output carefully before changing code
- Check if the failure is in your new code or existing code
- Don't delete or skip tests to make them pass — fix the underlying issue
- If a DuckDB query fails, test it in isolation with a single shard first

---

## 6. Updating Checkpoints

After completing meaningful work, create a new checkpoint file. Increment the number from the last checkpoint.

**File:** `checkpoint/CHECKPOINT_NNN.md`

**Format:**

```markdown
# Checkpoint NNN — [Short Title]

**Date:** YYYY-MM-DD
**Status:** [Compiles? Tests pass? How many?]

## What Changed

[Bullet list of what was added, modified, or removed since the last checkpoint]

## Current State

[Brief summary of what the project can do right now]

## Files Modified

| File | Change |
|---|---|
| `path/to/file.go` | Description of change |

## Tests

[Total test count. Any new tests added. All passing?]

## Known Issues

[Anything broken, incomplete, or needing attention]

## Next Steps

[What the next agent session should consider working on]
```

**Rules:**
- One checkpoint per session or per major change — don't create one for every small edit
- Keep it factual and concise — this is for machine consumption, not prose
- Always include the test status — the next agent relies on this
- List files modified, not files that already existed unchanged

---

## 7. Committing Changes

When your work is done and tests pass, commit with this process:

1. `git add` only the files you changed or created — never `git add -A`
2. Write a commit message in this format:

```
<type>: <what changed>

<one-line summary of why, if not obvious>
```

**Types:** `feat`, `fix`, `refactor`, `test`, `docs`, `chore`

**Examples:**
```
feat: add ORDER BY push-down to merger
fix: shard manager race condition on RemoveShard
test: add concurrent write test for router
docs: update checkpoint after adding rebalancing
refactor: extract query classifier from router
```

- Keep the first line under 72 characters
- No multi-paragraph descriptions — if the change needs that much explanation, the code or checkpoint should carry it
- Don't commit generated files, `.duckdb` data files, or `bin/`
