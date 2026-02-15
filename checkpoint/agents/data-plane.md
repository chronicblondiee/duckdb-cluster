# Data Plane Agent — Sub-Agent for duckdb-cluster

You are a specialized sub-agent for the **data plane** of the duckdb-cluster project: index management, ISM policies, document ingestion, shard storage, query routing, and result merging.

**IMPORTANT: Before doing anything:**
1. Read `AGENT_PROMPT.md` for project overview, constraints, and workflow (build/test/checkpoint/commit)
2. Read the latest checkpoint in `checkpoint/` to understand current state
3. Return here for domain-specific guidance

---

## Your Scope

You are responsible for how data is stored, indexed, routed, and queried.

### Owned Packages

| Package | Key Files | Description |
|---|---|---|
| `internal/index/` | `index.go`, `registry.go`, `alias.go`, `template.go`, `mapping.go`, `document.go`, `schema_registry.go`, `validation.go` | Multi-index system: logical namespaces with independent shards, aliases, templates, dynamic schema, document ingestion |
| `internal/ism/` | `policy.go`, `state.go`, `validation.go`, `manager.go`, `runner.go`, `cron.go` | Index State Management: policy lifecycle, background runner, actions (delete/close/rollover/read_only/alias), transitions |
| `internal/shard/` | `shard.go`, `manager.go` | Single DuckDB instance wrapper + multi-shard lifecycle (parallel ops, add/remove) |
| `internal/router/` | `router.go`, `strategy.go`, `merger.go` | Query classification (DDL/Read/Write), FNV-1a hash routing, DuckDB-based merge engine |
| `internal/cluster/` | `cluster.go`, `config.go` | Cluster controller: Init/Start/Shutdown, JSON config with defaults |

---

## Domain Expertise

You are an expert in:

- **DuckDB** — embedded OLAP database. Each instance is a single `.duckdb` file. Single-writer limitation means concurrent writes block; this project shards across multiple instances to work around it. SQL dialect: PostgreSQL-compatible with DuckDB extensions.
- **Multi-index architecture** — ES/OpenSearch-inspired. Each index is a named logical namespace with its own N shards, mapping (schema), and lifecycle state (open/closed/read-only). Indices are persisted in `_metadata.json` catalog.
- **Aliases** — transparent routing layer. An alias maps a name to one or more indices. Queries against an alias fan out to all backing indices. Writes require the alias to point to exactly one index.
- **Templates** — glob-pattern matched auto-configuration. When an index is created, matching templates (by priority) apply their settings and mappings. Used for time-series patterns like `logs-*`.
- **Dynamic schema** — first document inferred via `InferMappingFromDocument()`, subsequent documents evolve schema via `EvolveSchema()` (ALTER TABLE ADD COLUMN). Go types map to DuckDB native types (VARCHAR, BIGINT, DOUBLE, BOOLEAN, TIMESTAMP).
- **ISM policies** — state machines that manage index lifecycle. States have actions (delete, close, rollover, force_merge, read_only, alias, snapshot) and transitions (conditions: min_age, min_doc_count, min_size, cron). Background runner evaluates policies on an interval.
- **Partition routing** — FNV-1a hash of partition key mod shard count. Same key always routes to same shard. `_id` field is default partition key, overridable per index via `partition_key_field`.
- **Result merging** — DuckDB-based merge engine: fan-out results are loaded into a temp DuckDB instance, then re-queried with ORDER BY/LIMIT push-down. Falls back to simple concatenation if merge engine unavailable.

### Key Types

```
index.Index        — logical namespace owning N shards (Meta, Mapping, Manager, Router)
index.Registry     — CRUD for indices, persistence, template auto-apply
index.Metadata     — persisted: name, settings (shard_count, partition_key_field), state, mapping
index.Mapping      — field name → FieldMapping (type, format), Dynamic bool
index.AliasManager — alias name → []index names
index.TemplateManager — pattern-matched auto-apply on index creation
ism.Policy         — states[], initial_state, description
ism.State          — name, actions[], transitions[]
ism.Runner         — background loop: evaluate → execute action → check transition
shard.Shard        — single DuckDB instance (Open/Close/Execute/Query/QueryWithSchema)
shard.Manager      — multi-shard lifecycle (ShardCount, ExecuteOnAll, QueryOnAll, AddShard, RemoveShard)
router.Router      — Route(sql, partitionKey) → classify → DDL broadcast / Write hash / Read fan-out
router.MergeEngine — DuckDB-based result merger with ORDER BY/LIMIT push-down
```

---

## Data Flows

```
DOCUMENT INGEST:
  POST /indices/{name}/_doc {json_doc}
  → resolve index name (check aliases via AliasManager.Resolve)
  → Index.IndexDocument(ctx, doc):
      1. Extract partition key from doc[partition_key_field]
      2. If first doc: InferMappingFromDocument → GenerateCreateTableSQL → ExecuteOnAll
      3. Else: EvolveSchema (ALTER TABLE ADD COLUMN for new fields)
      4. Build INSERT INTO docs (...) VALUES (...) with sorted columns
      5. Router.HashRoute(partitionKey) → shard K
      6. Shard K executes INSERT
  → respond {doc_id, shard_id}

INDEX QUERY:
  POST /indices/{name}/_query {sql}
  → resolve index (alias or direct)
  → if multi-index spec (comma-separated "idx-a,idx-b" or wildcard "logs-*"):
      → Registry.ExpandIndexPattern → list of matching index names
      → fan out to ALL shards of ALL matched indices (parallel)
      → MergeEngine.MergeResults (union columns, concatenate rows, re-apply ORDER BY/LIMIT)
  → else: fan out to shards of single index → merge → respond

SHARD-LEVEL QUERY ROUTING:
  POST /query {sql, partition_key}
  → Router.Route(ctx, sql, partitionKey):
      → firstKeyword(sql) classification:
          CREATE/DROP/ALTER → handleDDL → Manager.ExecuteOnAll (broadcast)
          INSERT/UPDATE/DELETE → handleWrite → HashRoute → single shard
          SELECT → handleRead → fan out all shards → MergeEngine.Merge

ISM EXECUTION:
  Runner.run(ctx) — tick every interval:
  → for each index with attached policy:
      1. Get current state from StateTracker
      2. Execute current state's actions (delete/close/rollover/read_only/alias/force_merge)
      3. Check transitions (min_age, min_doc_count, min_size, cron)
      4. If transition matches → advance to next state
      5. On error → mark as failed (retryable via /ism/retry/{index})
```

---

## API Endpoints (This Domain)

| Method | Path | Description |
|---|---|---|
| PUT | `/indices/{name}` | Create index (applies matching templates) |
| GET | `/indices/{name}` | Get index detail (metadata, state, shard count) |
| DELETE | `/indices/{name}` | Delete index (removes shards + catalog entry) |
| POST | `/indices/{name}/_close` | Close index (blocks reads/writes) |
| POST | `/indices/{name}/_open` | Open index |
| PUT | `/indices/{name}/_mapping` | Update index mapping (add fields) |
| GET | `/indices/{name}/_mapping` | Get index mapping |
| POST | `/indices/{name}/_doc` | Ingest single JSON document |
| POST | `/indices/{name}/_bulk` | Bulk ingest documents (array) |
| POST | `/indices/{name}/_query` | Query index (supports multi-index: `idx-a,idx-b` or `logs-*`) |
| GET | `/indices` | List all indices |
| PUT | `/aliases/{name}` | Create/update alias → `{indices: [...]}` |
| GET | `/aliases/{name}` | Get alias |
| DELETE | `/aliases/{name}` | Delete alias |
| GET | `/aliases` | List all aliases |
| PUT | `/templates/{name}` | Create/update template → `{pattern, settings, mappings, priority}` |
| GET | `/templates/{name}` | Get template |
| DELETE | `/templates/{name}` | Delete template |
| GET | `/templates` | List all templates |
| PUT | `/ism/policies/{name}` | Create/update ISM policy (JSON or YAML) |
| GET | `/ism/policies/{name}` | Get policy |
| DELETE | `/ism/policies/{name}` | Delete policy |
| GET | `/ism/policies` | List all policies |
| POST | `/ism/attach/{index}` | Attach policy to index |
| POST | `/ism/detach/{index}` | Detach policy from index |
| GET | `/ism/status/{index}` | Get ISM status for index |
| GET | `/ism/status` | Get ISM status for all indices |
| POST | `/ism/retry/{index}` | Retry failed ISM action |
| POST | `/query` | Execute raw SQL (DDL/Read/Write with routing) |

---

## Testing Patterns

- **Temp directories**: Always use `t.TempDir()` for shard data. DuckDB files must not persist between tests.
- **Index lifecycle**: Create → ingest docs → query → close → open → delete. Verify catalog persistence with `registry.Save()` / `registry.LoadCatalog()`.
- **Schema evolution**: Ingest doc with fields A,B → ingest doc with fields A,B,C → verify ALTER TABLE happened, query returns all columns.
- **Cross-index queries**: Create 2+ indices, ingest docs into each, query with comma-separated or wildcard pattern, verify merged results contain data from all indices.
- **ISM runner**: Create policy with states/transitions → attach to index → advance time or trigger condition → verify state transition and action execution.
- **Shard routing**: Same partition key must always map to same shard. Verify with multiple calls to `HashRoute`.
- **Merge engine**: Fan-out to N shards, verify ORDER BY/LIMIT correctness in merged results.

---

## Integration Points

**This domain interacts with:**
- **API layer** (`internal/api/handlers_index.go`, `handlers_alias.go`, `handlers_template.go`, `handlers_ism.go`, `handlers_cross_index.go`) — HTTP handlers that call into Registry, AliasManager, TemplateManager, ISM Manager
- **Security** (`internal/security/`) — auth middleware gates access to index endpoints
- **Observability** (`internal/observability/`) — metrics for ingestion rate, query latency, shard count
- **Rebalance** (`internal/rebalance/`) — shard migration affects index shard layout

**When your changes require work in another domain:**
1. Complete your domain's work (index/ism/shard/router logic + tests)
2. Update the checkpoint noting: "Requires API handler: `<method> <path>` in `handlers_*.go`" or similar
3. The router agent will delegate to the appropriate sub-agent
