# Checkpoint 013 — Index System with Mappings, Document Ingestion, and Schema Registry

**Date:** 2026-02-14
**Status:** Compiles clean, all tests pass (195 tests)

## What Changed

Added an Elasticsearch/OpenSearch-style index system with three major features:

### 1. Index Abstraction (`internal/index/`)
- `Index` type wraps N shards in a named logical namespace
- Each index has its own `shard.Manager`, `router.Router`, data directory, settings, and lifecycle state (open/closed)
- `Registry` manages all indices: Create, Get, Delete, List, Open, Close, LoadAll, CloseAll
- Metadata persisted in `{dataDir}/indices/_metadata.json`
- Flat shard layout auto-migrates to `_default` index on first startup
- Index name validation (lowercase alphanumeric + hyphens/underscores)

### 2. Mappings (Dynamic + Explicit)
- `Mapping` type describes per-index document schema (field names, types, nested structure)
- Dynamic mapping (default): auto-detects fields from incoming documents, extends schema via `ALTER TABLE ADD COLUMN`
- Explicit mapping: users define schema upfront via `PUT /indices/{name}/_mapping`
- Type inference from JSON: string→VARCHAR, int→BIGINT, float→DOUBLE, bool→BOOLEAN, object→STRUCT, array→LIST
- Type inference from protobuf descriptors
- DuckDB native types for nested data (STRUCT, LIST, MAP)

### 3. Document Ingestion (JSON + Protobuf)
- `POST /indices/{name}/_doc` accepts JSON or protobuf (Content-Type based)
- First document auto-creates table (`_docs`) with inferred schema
- Subsequent documents evolve schema for new fields
- Configurable partition key field per index (defaults to `_id`)
- Bulk document ingestion via `POST /indices/{name}/_bulk`
- Schema registry for protobuf: register FileDescriptorSet, runtime deserialization via `dynamicpb`

### 4. Cluster Integration
- `Cluster.Registry` field added
- `Init()` creates `_default` index instead of flat shards
- `Start()` loads indices via `Registry.LoadAll()` (handles migration)
- `Shutdown()` closes all indices via registry
- Backward compat: `Cluster.Manager`/`Router` point to `_default` index

### 5. API Endpoints
- `PUT /indices/{name}` — Create index
- `GET /indices` — List all indices
- `GET /indices/{name}` — Get index detail
- `DELETE /indices/{name}` — Delete index
- `POST /indices/{name}/_close` / `_open` — Lifecycle
- `PUT /GET /indices/{name}/_mapping` — Mapping CRUD
- `POST /indices/{name}/_doc` — Index document (JSON/protobuf)
- `POST /indices/{name}/_bulk` — Bulk index documents
- `PUT /GET /DELETE /indices/{name}/_schema` — Protobuf schema registry
- `POST /query` now accepts optional `"index"` field (defaults to `_default`)

## Current State

Full index system operational. Multiple indices with independent shard counts, schemas, and data isolation. Document ingestion with dynamic schema detection. Protobuf support via schema registry. All existing API endpoints backward-compatible via `_default` index.

## Files Modified

| File | Change |
|------|--------|
| `internal/index/index.go` | New — Index type, NewIndex, OpenIndex, Close, Reopen, Delete, Route |
| `internal/index/registry.go` | New — Registry, Create, Get, Delete, List, LoadAll, migration, SaveCatalog |
| `internal/index/validation.go` | New — ValidateName, ValidateShardCount |
| `internal/index/mapping.go` | New — Mapping, FieldMapping, InferFieldFromValue, EvolveSchema, GenerateCreateTableSQL |
| `internal/index/document.go` | New — IndexDocument, IndexDocumentBulk, buildInsertSQL |
| `internal/index/schema_registry.go` | New — SchemaRegistry, Register, Get, Delete, DeserializeProtobuf, InferMappingFromProtoDescriptor |
| `internal/index/index_test.go` | New — 15 tests for Index/Registry lifecycle and routing |
| `internal/index/mapping_test.go` | New — 7 tests for mapping inference, evolution, DuckDB types |
| `internal/index/document_test.go` | New — 5 tests for document ingestion and schema evolution |
| `internal/index/schema_registry_test.go` | New — 5 tests for proto schema registry |
| `internal/api/handlers_index.go` | New — All index CRUD, mapping, document, schema HTTP handlers |
| `internal/api/server.go` | Modified — Added registry/schemaRegistry fields, registered 15 new routes |
| `internal/api/handlers.go` | Modified — Added Index field to queryRequest/bulkStatement, resolve index before routing |
| `internal/cluster/cluster.go` | Modified — Added Registry field, Init/Start/Shutdown use Registry |
| `internal/config/config.go` | Modified — Added DefaultIndex to CommonConfig |
| `cmd/duckdb-cluster/main_test.go` | Modified — Updated integration test for new index-based init/start |

## Tests

**Total: 195 tests passing (+30 from last checkpoint)**

New tests in `internal/index/`:
- Index lifecycle: TestNewIndex, TestOpenIndex, TestIndexCloseAndReopen, TestIndexDelete
- Registry: TestRegistryCreateGet, TestRegistryList, TestRegistryDelete, TestRegistryLoadAll, TestRegistryMigration, TestRegistryCloseAndOpenIndex
- Validation: TestValidateName, TestValidateShardCount
- Routing isolation: TestIndexScopedRouting
- Mapping: TestInferFieldFromValue, TestInferMappingFromDocument, TestNestedStructMapping, TestListMapping, TestGenerateCreateTableSQL, TestMappingEvolution, TestMappingRejectUnknown
- Documents: TestIndexJSONDocument, TestAutoCreateTable, TestPartitionKeyExtraction, TestBulkIndexDocuments, TestSchemaEvolutionOnDocument
- Schema registry: TestRegisterAndGetSchema, TestSchemaLoadAll, TestSchemaDelete, TestInferMappingFromProtoDescriptor, TestDeserializeProtobuf

## Known Issues

None.

## Next Steps

- API handler tests for index CRUD endpoints (httptest)
- Index aliases (name → one or more indices)
- Index templates (auto-create indices matching patterns)
- Cross-index queries
- Index-aware ingester/querier/distributor in distributed mode
- CLI commands for index management
