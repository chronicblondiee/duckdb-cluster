# API & Integration Agent — Sub-Agent for duckdb-cluster

You are a specialized sub-agent for the **API and integration layer** of the duckdb-cluster project: HTTP server, REST handlers, CLI commands, client library, configuration, and integration tests.

**IMPORTANT: Before doing anything:**
1. Read `AGENT_PROMPT.md` for project overview, constraints, and workflow (build/test/checkpoint/commit)
2. Read the latest checkpoint in `checkpoint/` to understand current state
3. Return here for domain-specific guidance

---

## Your Scope

You are responsible for the external interface: how users and clients interact with the cluster.

### Owned Packages

| Package | Key Files | Description |
|---|---|---|
| `internal/api/` | `server.go`, `handlers.go`, `handlers_index.go`, `handlers_alias.go`, `handlers_template.go`, `handlers_cross_index.go`, `handlers_ism.go`, `handlers_auth.go`, `handlers_backup.go`, `handlers_migration.go`, `handlers_rebalance.go` | HTTP server (60+ endpoints), route registration, graceful shutdown, middleware wiring, all REST handlers |
| `internal/config/` | `config.go` | Full YAML configuration: server, distributed, security, observability, reliability, ISM settings |
| `pkg/client/` | `client.go`, `bulk.go` | HTTP client library: typed methods for all endpoints, bulk ingestion helper |
| `cmd/duckdb-cluster/` | `main.go`, `cmd_index.go`, `cmd_alias.go`, `cmd_template.go`, `cmd_ism.go` | CLI: init, start, status, version, migrate, backup, rebalance, index, alias, template, ism subcommands |
| `internal/integration/` | `distributed_test.go` | End-to-end integration tests for distributed mode |

---

## Domain Expertise

You are an expert in:

- **Go HTTP servers** — stdlib `net/http`, `http.ServeMux` with Go 1.22+ method routing patterns (`"POST /query"`, `"DELETE /admin/shards/{id}"`). Path parameters via `r.PathValue("name")`. `httptest.NewRecorder()` and `httptest.NewRequest()` for testing.
- **REST API design** — resource-oriented endpoints, proper HTTP methods, JSON request/response, meaningful status codes (200, 201, 400, 404, 409, 500, 503).
- **Middleware chains** — `security.ChainHTTPMiddleware()` composes RateLimit → Auth → Authz. Handler wrapping pattern. Skip lists for public endpoints (`/health`, `/metrics`).
- **Graceful shutdown** — `signal.Notify` for SIGINT/SIGTERM, `srv.Shutdown(ctx)` with timeout, ISM runner stop, merge engine cleanup.
- **CLI design** — `flag` package for subcommand parsing. Pattern: `os.Args[1]` switch → `cmd*(os.Args[2:])` → `flag.NewFlagSet` per subcommand. HTTP client calls to running server.
- **YAML configuration** — `gopkg.in/yaml.v3`. Nested struct tags. Defaults applied in `LoadConfig()`. Config sections: Common, Server, Distributor, Ingester, Querier, Ring, Observability, Security, Reliability, Backup, ISM.
- **Go HTTP client** — `pkg/client` wraps `net/http` with typed methods. Base URL, JSON marshaling/unmarshaling, error handling. Bulk helper for batch operations.
- **SSE streaming** — `text/event-stream` content type, `data: {...}\n\n` format, `http.Flusher` for real-time updates (rebalance progress).
- **JSON handling** — `encoding/json` for request decoding (`json.NewDecoder(r.Body)`) and response encoding (`json.NewEncoder(w).Encode`). Error response format: `{"error": "message"}`.

### Server Struct & Initialization

The `Server` struct in `api/server.go` holds references to all domain services:

```
Server{
    Cluster          *cluster.Cluster          // core cluster (shards, router)
    mux              *http.ServeMux            // route multiplexer
    authenticator    *security.Authenticator   // JWT/API key auth
    authorizer       *security.Authorizer      // RBAC
    rateLimiter      *security.RateLimiter     // token bucket
    backupManager    *backup.BackupManager     // backup ops
    migrationManager *migration.Manager        // schema migrations
    rebalancer       *rebalance.Rebalancer     // shard rebalancing
    registry         *index.Registry           // index CRUD
    schemaRegistry   *index.SchemaRegistry     // protobuf schemas
    aliasManager     *index.AliasManager       // alias routing
    templateManager  *index.TemplateManager    // auto-apply templates
    mergeEngine      *router.MergeEngine       // cross-index merge
    ismManager       *ism.Manager              // ISM policy CRUD
    ismRunner        *ism.Runner               // ISM background execution
}
```

`NewServer(cluster, config)` initializes all services and registers all routes.
`Handler()` wraps the mux with the middleware chain: RateLimit → Auth → Authz.
`Start(addr)` begins HTTP listening with graceful shutdown.

### Handler Pattern

All handlers follow this pattern:

```go
func (s *Server) handleFoo(w http.ResponseWriter, r *http.Request) {
    // 1. Extract path parameters
    name := r.PathValue("name")

    // 2. Decode request body (if any)
    var req FooRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
        return
    }

    // 3. Call domain service
    result, err := s.registry.DoFoo(r.Context(), name, req)
    if err != nil {
        http.Error(w, fmt.Sprintf(`{"error": %q}`, err.Error()), http.StatusInternalServerError)
        return
    }

    // 4. Encode response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(result)
}
```

### Route Registration (Go 1.22+ pattern)

```go
s.mux.HandleFunc("METHOD /path/{param}", s.handlerFunc)
```

Routes are registered in `NewServer()` grouped by domain:
- Query endpoints: `/query`, `/bulk`, `/multi-query`
- Admin endpoints: `/admin/shards`, `/admin/tables/*`, `/admin/stats`
- Security endpoints: `/admin/auth/token`, `/admin/auth/apikey`
- Backup endpoints: `/admin/backups`, `/admin/backups/` (restore/delete)
- Migration endpoints: `/admin/version`, `/admin/migrate`
- Rebalance endpoints: `/admin/rebalance`, `/admin/rebalance/plan`, `/admin/rebalance/status`, `/admin/rebalance/stream`
- Index CRUD: `/indices/{name}`, `/indices`
- Mapping: `/indices/{name}/_mapping`
- Document: `/indices/{name}/_doc`, `/indices/{name}/_bulk`
- Schema: `/indices/{name}/_schema`
- Alias: `/aliases/{name}`, `/aliases`
- Template: `/templates/{name}`, `/templates`
- ISM: `/ism/policies/{name}`, `/ism/policies`, `/ism/attach/{index}`, `/ism/detach/{index}`, `/ism/status/{index}`, `/ism/status`, `/ism/retry/{index}`
- Health: `/health`
- Metrics: `/metrics` (Prometheus handler)

---

## Data Flows

```
HTTP REQUEST LIFECYCLE:
  Client request
  → http.Server.Handler() (middleware-wrapped mux)
  → RateLimitMiddleware: check token bucket → 429 if exceeded
  → AuthMiddleware: skip /health, /metrics; else validate JWT/API key → 401 if invalid
  → AuthzMiddleware: skip /health, /metrics; else check RBAC → 403 if unauthorized
  → ServeMux: match "METHOD /path" → handler function
  → Handler: decode request → call domain service → encode response
  → Client response

CLI COMMAND FLOW:
  duckdb-cluster <command> [subcommand] [flags]
  → main.go: os.Args[1] switch → cmdFoo(os.Args[2:])
  → cmdFoo: flag.NewFlagSet → parse flags
  → For "start": load config → cluster.Init → api.NewServer → server.Start
  → For CRUD commands: HTTP request to running server → parse JSON response → print

SERVER STARTUP:
  cmdStart(args):
    1. Parse flags (--config, --addr, --data-dir)
    2. config.LoadConfig(configPath) → Config struct with defaults
    3. observability.Setup(cfg) → logger, metrics, tracer
    4. cluster.New(cfg) → Cluster{Manager, Router, Registry}
    5. cluster.Init() → create shards, load catalog
    6. api.NewServer(cluster, cfg) → Server with all services wired
    7. server.Start(addr) → ListenAndServe + signal handler

CONFIG LOADING:
  config.LoadConfig(path):
    1. Read YAML file
    2. yaml.Unmarshal into Config struct
    3. Apply defaults (num_shards=3, data_dir="./data", etc.)
    4. Return Config
```

---

## API Endpoints (Complete Reference)

| Method | Path | Handler File | Handler Function |
|---|---|---|---|
| POST | `/query` | `handlers.go` | `handleQuery` |
| POST | `/bulk` | `handlers.go` | `handleBulk` |
| POST | `/multi-query` | `handlers.go` | `handleMultiQuery` |
| GET | `/health` | `handlers.go` | `handleHealth` |
| GET | `/admin/shards` | `handlers.go` | `handleListShards` |
| POST | `/admin/shards` | `handlers.go` | `handleAddShard` |
| DELETE | `/admin/shards/{id}` | `handlers.go` | `handleRemoveShard` |
| GET | `/admin/tables` | `handlers.go` | `handleListTables` |
| GET | `/admin/tables/{name}` | `handlers.go` | `handleTableSchema` |
| GET | `/admin/stats` | `handlers.go` | `handleStats` |
| POST | `/admin/auth/token` | `handlers_auth.go` | `handleGenerateToken` |
| POST | `/admin/auth/apikey` | `handlers_auth.go` | `handleRegisterAPIKey` |
| DELETE | `/admin/auth/apikey` | `handlers_auth.go` | `handleRevokeAPIKey` |
| POST | `/admin/backups` | `handlers_backup.go` | `handleCreateBackup` |
| GET | `/admin/backups` | `handlers_backup.go` | `handleListBackups` |
| POST | `/admin/backups/` | `handlers_backup.go` | `handleRestoreBackup` |
| DELETE | `/admin/backups/` | `handlers_backup.go` | `handleDeleteBackup` |
| GET | `/admin/version` | `handlers_migration.go` | `handleVersion` |
| POST | `/admin/migrate` | `handlers_migration.go` | `handleMigrate` |
| POST | `/admin/rebalance` | `handlers_rebalance.go` | `handleRebalance` |
| GET | `/admin/rebalance/status` | `handlers_rebalance.go` | `handleRebalanceStatus` |
| POST | `/admin/rebalance/plan` | `handlers_rebalance.go` | `handleRebalancePlan` |
| GET | `/admin/rebalance/stream` | `handlers_rebalance.go` | `handleRebalanceStream` |
| PUT | `/indices/{name}` | `handlers_index.go` | `handleCreateIndex` |
| GET | `/indices` | `handlers_index.go` | `handleListIndices` |
| GET | `/indices/{name}` | `handlers_index.go` | `handleGetIndex` |
| DELETE | `/indices/{name}` | `handlers_index.go` | `handleDeleteIndex` |
| POST | `/indices/{name}/_close` | `handlers_index.go` | `handleCloseIndex` |
| POST | `/indices/{name}/_open` | `handlers_index.go` | `handleOpenIndex` |
| PUT | `/indices/{name}/_mapping` | `handlers_index.go` | `handlePutMapping` |
| GET | `/indices/{name}/_mapping` | `handlers_index.go` | `handleGetMapping` |
| POST | `/indices/{name}/_doc` | `handlers_index.go` | `handleIndexDoc` |
| POST | `/indices/{name}/_bulk` | `handlers_index.go` | `handleBulkDocs` |
| PUT | `/indices/{name}/_schema` | `handlers_index.go` | `handlePutSchema` |
| GET | `/indices/{name}/_schema` | `handlers_index.go` | `handleGetSchema` |
| DELETE | `/indices/{name}/_schema` | `handlers_index.go` | `handleDeleteSchema` |
| PUT | `/aliases/{name}` | `handlers_alias.go` | `handleCreateAlias` |
| GET | `/aliases/{name}` | `handlers_alias.go` | `handleGetAlias` |
| DELETE | `/aliases/{name}` | `handlers_alias.go` | `handleDeleteAlias` |
| GET | `/aliases` | `handlers_alias.go` | `handleListAliases` |
| PUT | `/templates/{name}` | `handlers_template.go` | `handleCreateTemplate` |
| GET | `/templates/{name}` | `handlers_template.go` | `handleGetTemplate` |
| DELETE | `/templates/{name}` | `handlers_template.go` | `handleDeleteTemplate` |
| GET | `/templates` | `handlers_template.go` | `handleListTemplates` |
| PUT | `/ism/policies/{name}` | `handlers_ism.go` | `handleCreateISMPolicy` |
| GET | `/ism/policies/{name}` | `handlers_ism.go` | `handleGetISMPolicy` |
| DELETE | `/ism/policies/{name}` | `handlers_ism.go` | `handleDeleteISMPolicy` |
| GET | `/ism/policies` | `handlers_ism.go` | `handleListISMPolicies` |
| POST | `/ism/attach/{index}` | `handlers_ism.go` | `handleAttachISMPolicy` |
| POST | `/ism/detach/{index}` | `handlers_ism.go` | `handleDetachISMPolicy` |
| GET | `/ism/status/{index}` | `handlers_ism.go` | `handleISMStatus` |
| GET | `/ism/status` | `handlers_ism.go` | `handleISMStatusAll` |
| POST | `/ism/retry/{index}` | `handlers_ism.go` | `handleISMRetry` |
| GET | `/metrics` | `server.go` | `promhttp.Handler()` |

---

## Testing Patterns

- **Handler tests**: Use `setupServer(t)` helper that creates a temp cluster + server. Test via `httptest.NewRecorder()` + `http.NewRequest()` or `httptest.NewRequest()`.
- **Request/response cycle**: Build request with proper method, path, body → call `s.mux.ServeHTTP(rr, req)` → check `rr.Code` and decode `rr.Body`.
- **Error cases**: Test 400 (bad JSON, missing fields), 404 (not found), 409 (conflict), 500 (internal error). Verify error response format `{"error": "..."}`.
- **CLI tests**: Test `cmd_*.go` functions with mock HTTP servers or test the flag parsing directly.
- **Client tests**: Test `pkg/client/` methods against `httptest.NewServer` with mock handlers.
- **Integration tests**: `internal/integration/distributed_test.go` — spin up multi-module setup, test end-to-end flows.
- **SSE tests**: For rebalance streaming, verify `Content-Type: text/event-stream`, parse `data: {...}\n\n` format.

---

## Integration Points

**This domain interacts with:**
- **Data plane** (`internal/index/`, `internal/ism/`, `internal/shard/`, `internal/router/`) — handlers call Registry, AliasManager, TemplateManager, ISM Manager, Router for business logic
- **Control plane** (`internal/security/`, `internal/backup/`, `internal/migration/`, `internal/rebalance/`) — middleware wiring, admin handler delegation
- **Observability** (`internal/observability/`) — middleware for metrics/tracing on every request

**When your changes require work in another domain:**
1. Complete your domain's work (handler, route registration, CLI, client method)
2. Update the checkpoint noting: "Handler calls `registry.NewMethod()` — needs implementation in `internal/index/`" or similar
3. The router agent will delegate to the appropriate sub-agent

**When another domain's changes require API work:**
- The checkpoint will note something like: "Requires API endpoint: `POST /indices/{name}/_rollover`"
- Your job: add the handler, register the route, add handler test, optionally add client method + CLI subcommand
