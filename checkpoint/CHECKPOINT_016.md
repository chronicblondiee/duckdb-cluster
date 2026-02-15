# Checkpoint 016 — Template Auto-Apply Mappings

**Date:** 2026-02-15
**Status:** Compiles clean, all tests pass (228 tests)

## What Changed

Templates now automatically apply their mappings when new indices are created matching the template's glob pattern. Previously, only `ShardCount` and `PartitionKeyField` were applied from templates — mappings were ignored. Now the highest-priority matching template's mapping is deep-cloned and set on the new index. Explicit mappings provided in the create request are merged on top (explicit fields override template fields, template-only fields are preserved).

**1. Mapping.Clone()** (`internal/index/mapping.go`)
- Added `Clone()` method for deep-copying a Mapping
- Added `cloneFields()` and `cloneFieldMapping()` helpers for recursive FieldMapping deep copy
- Ensures template and index don't share pointers

**2. Template mapping in Registry.Create()** (`internal/index/registry.go`)
- After matching the highest-priority template, if `tmpl.Mapping != nil`, sets `meta.Mapping = tmpl.Mapping.Clone()`
- `NewIndex()` already respects non-nil `meta.Mapping`

**3. Explicit mapping merge** (`internal/api/handlers_index.go`)
- Changed from replacing the entire mapping to merging explicit fields on top of template-applied mapping
- Explicit fields override, template-only fields are preserved

## Current State

Index templates now fully auto-apply:
- Settings (ShardCount, PartitionKeyField) — existing
- Mappings (field definitions, dynamic flag) — new
- Explicit mappings in create request merge over template mappings
- Each index gets its own deep-cloned copy (no shared state)

## Files Modified

| File | Change |
|---|---|
| `internal/index/mapping.go` | Added `Clone()`, `cloneFields()`, `cloneFieldMapping()` |
| `internal/index/registry.go` | Apply template mapping in `Create()` |
| `internal/api/handlers_index.go` | Merge explicit mappings over template mappings |
| `internal/api/handlers_index_test.go` | Added 4 tests + `index` import |

## Tests

**Total: 228 tests passing (+4 from last checkpoint)**

New tests:
- `TestTemplateMappingAutoApplied` — template mapping fields auto-applied to matching index
- `TestTemplateMappingWithExplicitOverride` — explicit fields override template, template-only preserved
- `TestNoTemplateMatchDefaultMapping` — non-matching index gets default empty dynamic mapping
- `TestTemplateMappingCloneIsolation` — modifying one index's mapping doesn't affect another's

## Known Issues

None.

## Next Steps

- Index-aware ingester/querier/distributor in distributed mode
- Index lifecycle events (webhooks/notifications)
- Index statistics and monitoring per index
- Cross-index WHERE clause push-down optimization
