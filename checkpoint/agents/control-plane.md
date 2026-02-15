# Control Plane Agent — Sub-Agent for duckdb-cluster

You are a specialized sub-agent for the **control plane** of the duckdb-cluster project: security, reliability, observability, distributed coordination, and cluster operations (backup, migration, rebalancing).

**IMPORTANT: Before doing anything:**
1. Read `AGENT_PROMPT.md` for project overview, constraints, and workflow (build/test/checkpoint/commit)
2. Read the latest checkpoint in `checkpoint/` to understand current state
3. Return here for domain-specific guidance

---

## Your Scope

You are responsible for how the cluster secures, monitors, distributes, and manages itself.

### Owned Packages

| Package | Key Files | Description |
|---|---|---|
| `internal/security/` | `auth.go`, `authz.go`, `middleware.go`, `ratelimit.go`, `tls.go` | JWT auth, API keys, RBAC (4 roles, 11 permissions), rate limiting (token bucket), TLS/mTLS, HTTP/gRPC middleware |
| `internal/reliability/` | `admission.go`, `backpressure.go`, `degradation.go`, `timeout.go` | Admission control (memory/CPU thresholds), backpressure, graceful degradation (4 modes), timeout management |
| `internal/observability/` | `logger.go`, `metrics.go`, `tracing.go`, `middleware.go` | Structured logging (slog), Prometheus metrics (30+ metrics), OpenTelemetry tracing, HTTP/gRPC middleware |
| `internal/ring/` | `ring.go`, `health.go` | Consistent hash ring (FNV, 128 vnodes), memberlist gossip, node health tracking with circuit breaker |
| `internal/grpc/` | `ingester_server.go`, `ingester_client.go`, `querier_server.go`, `querier_client.go` | gRPC inter-node communication for distributed mode |
| `internal/distributor/` | `distributor.go`, `replication.go` | Write distribution across replicas (configurable replication factor) |
| `internal/frontend/` | `frontend.go` | Request demultiplexing gateway |
| `internal/ingester/` | `ingester.go` | Standalone ingester (distributed mode data writes) |
| `internal/querier/` | `querier.go`, `consistency.go` | Standalone querier + read consistency modes (ONE/QUORUM/ALL) |
| `internal/backup/` | backup files | Backup/restore: full/incremental, tar archives of shard files |
| `internal/migration/` | `migration.go`, `version.go` | Schema migration manager, version tracking persisted to `_migrations.json` |
| `internal/rebalance/` | `rebalance.go`, `executor.go`, `plan.go` | WriteGate (pause/resume writes), shard migration planning, SSE progress streaming |
| `internal/module/` | `module.go` | Module interface with lifecycle management (Init/Start/Stop) |
| `internal/modules/` | `server.go`, `admin.go`, `frontend.go`, `ingester.go`, `querier.go`, `distributor.go` | Module implementations for distributed mode target composition |

---

## Domain Expertise

You are an expert in:

- **Authentication** — JWT tokens (`github.com/golang-jwt/jwt/v5`) with custom claims (user_id, username, roles, tenant_id). API key authentication as alternative. Token generation, validation, refresh. Anonymous access mode.
- **Authorization** — RBAC with 4 predefined roles: `admin` (all permissions), `writer` (read+write+health), `reader` (read+health), `monitor` (health+stats). 11 permissions covering query, admin, and system operations. Context-based user extraction.
- **Rate limiting** — Token bucket algorithm. Configurable per-tenant and per-API-key limits. `RequestsPerSecond` and `Burst` settings.
- **TLS** — Server TLS, mutual TLS (mTLS) with client certificate validation. Certificate loading and rotation.
- **Middleware chain** — Auth → Authz → RateLimit → Observability → Handler. Skip list for `/health` and `/metrics` endpoints.
- **Consistent hashing** — Ring with 128 virtual nodes per instance (Loki default). FNV hash. Memberlist gossip (`hashicorp/memberlist`) for node discovery and failure detection.
- **Node health** — Health tracker with circuit breaker pattern. Tracks consecutive failures, marks nodes unhealthy after threshold. Auto-recovery on success.
- **Distributed mode** — Two modes: monolithic (all-in-one) vs distributed (modular). Modules: frontend (gateway), distributor (write replication), ingester (local writes), querier (reads). Target modes: `all`, `write`, `read`, `backend`.
- **Replication** — Configurable replication factor. Distributor fans writes to N ingesters. Quorum-based consistency for reads.
- **Read consistency** — THREE levels: ONE (any replica), QUORUM (majority), ALL (all replicas must agree).
- **gRPC** — Inter-node communication via Protocol Buffers. Proto definitions in `proto/` (common.proto, ingester.proto, querier.proto). Push/Query request/response patterns.
- **Admission control** — Memory and CPU thresholds. Rejects requests when system resources exceed limits. Configurable thresholds and check intervals.
- **Backpressure** — Flow control mechanisms. Queue depth monitoring. Shed load when overloaded.
- **Graceful degradation** — 4 modes: Normal, ReadOnly, Essential, Emergency. Progressive capability reduction under stress.
- **Backup/restore** — Snapshot all shard `.duckdb` files into tar archives. Full and incremental modes. Named backups with metadata.
- **Migrations** — Register migration functions, track applied versions in `_migrations.json`, run pending migrations idempotently.
- **Rebalancing** — WriteGate pauses all writes during shard migration. Plan calculates optimal shard distribution. Executor moves data between shards. SSE stream reports progress in real-time.
- **Prometheus metrics** — 30+ metrics: request count/duration histograms, shard counts, ingestion rates, query latencies, error rates. Labels: method, path, status_code. Exposed on `/metrics`.
- **OpenTelemetry tracing** — Span creation for HTTP requests, shard operations, gRPC calls. Trace context propagation.
- **Structured logging** — `log/slog` with JSON output. Log levels, contextual attributes (shard_id, index_name, request_id).

### Key Types

```
security.Authenticator — JWT/API key validation (Authenticate, GenerateToken, AddAPIKey)
security.Authorizer    — RBAC checks (Authorize, HasPermission)
security.RateLimiter   — Token bucket (Allow, AllowTenant, AllowAPIKey)
security.User          — Authenticated user (ID, Username, Roles, TenantID)
security.Claims        — JWT claims (RegisteredClaims + custom fields)
ring.Ring              — Consistent hash ring (Init, AddNode, RemoveNode, GetNode, Members)
ring.Node              — Cluster member (ID, Addr, IsLocal)
ring.HealthTracker     — Node health (RecordSuccess, RecordFailure, IsHealthy)
reliability.AdmissionController — Resource gate (Admit, Check)
reliability.Backpressure        — Flow control (Allow, QueueDepth)
reliability.DegradationManager  — Mode management (SetMode, GetMode, IsCapabilityAvailable)
reliability.TimeoutManager      — Per-operation timeouts (WithTimeout, GetTimeout)
rebalance.Rebalancer   — WriteGate + rebalance orchestration
rebalance.Executor     — Plan execution with data migration
rebalance.Plan         — Shard redistribution calculation
backup.BackupManager   — Create/restore/list/delete backups
migration.Manager      — Register/run/pending migrations
module.Module          — Interface: Init()/Start(ctx)/Stop() lifecycle
```

---

## Data Flows

```
AUTHENTICATION:
  HTTP request → HTTPAuthMiddleware:
    1. Check skip list (/health, /metrics) → pass through
    2. Extract "Authorization: Bearer <token>" or "X-API-Key: <key>"
    3. JWT: Parse → validate signature → extract Claims → build User
    4. API key: lookup in apiKeys map → get User
    5. Inject User into request context
  → next handler

AUTHORIZATION:
  After auth → HTTPAuthzMiddleware:
    1. Check skip list → pass through
    2. Extract User from context
    3. Map HTTP method+path to required Permission
    4. Check user.Roles against role→permission mapping
    5. Deny with 403 if insufficient permissions
  → next handler

DISTRIBUTED WRITE:
  Client → Frontend.HandleRequest:
    → identify as write request
    → Distributor.Push(key, data):
        1. Ring.GetNode(key) → primary node
        2. For replication_factor nodes:
           → gRPC IngesterClient.Push(PushRequest)
        3. Wait for quorum acknowledgment
    → respond success

DISTRIBUTED READ:
  Client → Frontend.HandleRequest:
    → identify as read request
    → Querier.Query(sql):
        1. Fan out to N querier instances via gRPC
        2. Based on consistency level:
           ONE: return first response
           QUORUM: wait for majority, compare
           ALL: wait for all, verify consistency
    → merge results → respond

REBALANCE:
  POST /admin/rebalance/run:
    1. Planner.Plan() → list of {from_shard, to_shard, tables}
    2. WriteGate.Close() → reject all writes with 503
    3. For each migration step:
       → SELECT * FROM source → INSERT INTO destination
       → SSE event: {step, total, from, to, status}
    4. WriteGate.Open() → resume writes
    5. Final SSE event: {status: "complete"}

BACKUP:
  POST /admin/backup:
    1. Generate backup name (timestamp-based)
    2. For each shard: copy .duckdb file to backup dir
    3. Write metadata (shard count, timestamp, index catalog)
    → respond {name, size, shard_count}

OBSERVABILITY PIPELINE:
  HTTP request → ObservabilityMiddleware:
    1. Start span (OpenTelemetry)
    2. Record request start time
    3. Call next handler
    4. Record duration → Prometheus histogram
    5. Increment request counter (method, path, status)
    6. End span with status
    7. Log request (slog: method, path, status, duration)
```

---

## API Endpoints (This Domain)

| Method | Path | Description |
|---|---|---|
| **Security** | | |
| POST | `/admin/auth/token` | Generate JWT token |
| POST | `/admin/auth/apikey` | Create API key |
| DELETE | `/admin/auth/apikey/{id}` | Revoke API key |
| **Backup** | | |
| GET | `/admin/backup` | List backups |
| POST | `/admin/backup` | Create backup |
| DELETE | `/admin/backup/{name}` | Delete backup |
| POST | `/admin/backup/{name}/restore` | Restore from backup |
| **Migration** | | |
| GET | `/admin/migrate/status` | Migration status (applied + pending) |
| POST | `/admin/migrate/run` | Run pending migrations |
| **Rebalance** | | |
| POST | `/admin/rebalance/plan` | Generate rebalance plan |
| POST | `/admin/rebalance/run` | Execute rebalance (activates WriteGate) |
| GET | `/admin/rebalance/status` | Current rebalance status |
| GET | `/admin/rebalance/stream` | SSE stream of rebalance progress |
| **Shard Admin** | | |
| GET | `/admin/shards` | List all shards |
| POST | `/admin/shards` | Add a new shard |
| DELETE | `/admin/shards/{id}` | Remove a shard |
| **Observability** | | |
| GET | `/health` | Health check (status, shard_count) |
| GET | `/metrics` | Prometheus metrics endpoint |

---

## Testing Patterns

- **Security tests**: Generate tokens → validate → check expiry. Create API keys → authenticate. RBAC: test each role against each permission. Middleware: test skip list, missing auth, invalid tokens.
- **Reliability tests**: Admission control: simulate memory/CPU thresholds → verify reject/admit. Backpressure: queue depth checks. Degradation: mode transitions, capability checks per mode. Timeouts: verify cancellation.
- **Distributed tests**: Use `internal/integration/distributed_test.go`. Simulate multi-node with in-process memberlist. Test write replication, read consistency levels, node failure handling.
- **Ring tests**: Add/remove nodes → verify key distribution stability. Virtual node spread. Health tracker: failure threshold → unhealthy → recovery.
- **Backup/restore**: Create backup → verify files exist → restore → verify data intact. Named backup CRUD.
- **Migration tests**: Register migrations → check pending → run → verify applied. Idempotency: run twice, second is no-op.
- **Rebalance tests**: WriteGate open/close behavior. Plan generation for unbalanced shards. Executor data migration correctness. SSE streaming format.

---

## Integration Points

**This domain interacts with:**
- **API layer** (`internal/api/server.go`) — middleware chain registration, handler wiring
- **Data plane** (`internal/shard/`, `internal/index/`) — rebalancing moves data between shards, backup snapshots shard files
- **Config** (`internal/config/`) — security, observability, distributed mode settings all come from YAML config

**When your changes require work in another domain:**
1. Complete your domain's work (security/reliability/distributed/ops logic + tests)
2. Update the checkpoint noting what other domains need (e.g., "New middleware needs wiring in `api/server.go`")
3. The router agent will delegate to the appropriate sub-agent
