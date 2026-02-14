# Checkpoint 014 — Index Aliases, Templates, Cross-Index Queries, and CLI

**Date:** 2026-02-14
**Status:** Compiles clean, all tests pass (219 tests)

## What Changed

Added index aliases, index templates, cross-index queries, and CLI commands for index management. Also added comprehensive API handler tests for index CRUD endpoints.

### 1. Index Aliases (`internal/index/alias.go`)
- `AliasManager` maps alias names to one or more index names
- CRUD operations: Put, Get, Delete, List, Resolve
- Persisted to `{dataDir}/indices/_aliases.json`
- Loaded on startup via `LoadAll()`
- Single-index aliases automatically resolve in `resolveIndex()` for transparent routing
- API validates that referenced indices exist before creating aliases

### 2. Index Templates (`internal/index/template.go`)
- `TemplateManager` manages templates with glob patterns and priorities
- Templates define settings (shard count, partition key) and optional mappings
- `Match(indexName)` returns matching templates sorted by priority (highest first)
- `MatchGlob()` supports `*` wildcard patterns (prefix*, *suffix, *contains*)
- Applied automatically during `Registry.Create()` — template defaults fill in unset settings
- Persisted to `{dataDir}/indices/_templates.json`

### 3. Cross-Index Queries (`internal/api/handlers_cross_index.go`)
- Multi-index spec detection: comma-separated (`idx-a,idx-b`) or wildcard (`logs-*`)
- `resolveMultipleIndices()` expands wildcards, aliases, and comma-separated lists
- Read queries fan out to all matched indices in parallel goroutines
- Results merged: columns unioned, rows concatenated, ShardID=-1
- Write operations rejected for multi-index queries
- Pagination applied after merge

### 4. CLI Commands
- `duckdb-cluster index` — list, create, delete, get, close, open
- `duckdb-cluster alias` — list, create, delete, get
- `duckdb-cluster template` — list, create, delete, get
- All use HTTP calls to running server (same pattern as backup/rebalance)
- Tabwriter-formatted output for list commands

### 5. API Handler Tests (`internal/api/handlers_index_test.go`)
- 15 tests covering all index CRUD endpoints via httptest
- Create (201), duplicate (409), invalid name (400)
- List, get, get not found (404)
- Delete, delete _default (403)
- Close/open lifecycle
- Put/get mapping
- Index doc + bulk docs
- Query with index field

### 6. API Endpoints (New)
- `PUT /aliases/{name}` — Create/update alias
- `GET /aliases/{name}` — Get alias
- `DELETE /aliases/{name}` — Delete alias
- `GET /aliases` — List all aliases
- `PUT /templates/{name}` — Create/update template
- `GET /templates/{name}` — Get template
- `DELETE /templates/{name}` — Delete template
- `GET /templates` — List all templates

## Current State

Full index management system. Aliases allow transparent routing through named references. Templates auto-apply settings to new indices matching glob patterns. Cross-index queries fan out reads across multiple indices in parallel. CLI provides full management interface for indices, aliases, and templates.

## Files Added

| File | Purpose |
|------|---------|
| `internal/api/handlers_index_test.go` | 15 API handler tests for index CRUD |
| `internal/index/alias.go` | AliasManager with CRUD + persistence |
| `internal/index/alias_test.go` | Alias unit tests (CRUD, persistence, validation) |
| `internal/api/handlers_alias.go` | Alias HTTP handlers |
| `internal/index/template.go` | TemplateManager with glob matching + persistence |
| `internal/index/template_test.go` | Template unit tests (glob, priority, CRUD, persistence) |
| `internal/api/handlers_template.go` | Template HTTP handlers |
| `internal/api/handlers_cross_index.go` | Cross-index query fan-out + merge |
| `internal/api/handlers_cross_index_test.go` | Cross-index query tests |
| `cmd/duckdb-cluster/cmd_index.go` | CLI index subcommands |
| `cmd/duckdb-cluster/cmd_alias.go` | CLI alias subcommands |
| `cmd/duckdb-cluster/cmd_template.go` | CLI template subcommands |

## Files Modified

| File | Change |
|------|--------|
| `internal/api/server.go` | Added aliasManager, templateManager fields; init + 8 new routes |
| `internal/api/handlers_index.go` | Updated resolveIndex() for alias; added resolveMultipleIndices() |
| `internal/api/handlers.go` | Detect multi-index spec in handleQuery(), delegate to cross-index handler |
| `internal/index/registry.go` | Added templateManager field; apply templates in Create() |
| `cmd/duckdb-cluster/main.go` | Added index/alias/template command dispatch + usage |

## Tests

**Total: 219 tests passing (+24 from last checkpoint)**

New tests:
- `internal/api/handlers_index_test.go`: 15 tests (index CRUD, mapping, docs, query with index)
- `internal/index/alias_test.go`: 3 tests (CRUD, persistence, validation)
- `internal/index/template_test.go`: 4 tests (glob matching, CRUD, priority/match, persistence, validation)
- `internal/api/handlers_cross_index_test.go`: 3 tests (comma-separated, wildcard, write rejection)

## Known Issues

None.

## Next Steps

- Index-aware ingester/querier/distributor in distributed mode
- Index lifecycle events (webhooks/notifications)
- Template mappings applied to new indices
- Index statistics and monitoring per index
- Cross-index aggregation queries
