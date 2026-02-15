# Checkpoint 017 — ISM (Index State Management) Policy System

**Date:** 2026-02-15
**Status:** Compiles clean, all tests pass (280 tests)

## What Changed

Full ISM policy system inspired by OpenSearch ISM. Users define lifecycle policies as state machines (YAML or JSON). A background runner evaluates managed indices on a configurable interval, executes actions, and transitions between states based on conditions.

## New Files

### Core ISM Package (`internal/ism/`)

**`policy.go`** — Data model
- Custom `Duration` type with YAML/JSON marshal and day shorthand ("7d", "168h")
- `Policy`, `ISMTemplate`, `PolicyState`, `Action`, `ActionType`, `RetryConfig`, `Transition`, `TransitionConditions`
- 8 action types: close, open, delete, read_only, force_merge, notification, snapshot, rollover

**`state.go`** — Per-index ISM tracking
- `IndexISMState` with policy reference, current state, action progress, retry tracking, failure flag
- Persisted in `_ism_state.json` (separate from index metadata for frequent updates)

**`validation.go`** — Policy structural validation
- Validates name, default_state, states, duplicate names, transition targets, action-specific config

**`manager.go`** — CRUD, persistence, auto-attach
- Follows TemplateManager pattern (sync.RWMutex, map storage, saveLocked)
- Policy versioning (auto-incremented), YAML directory loading, ISMTemplate glob matching
- Persists policies in `_ism_policies.json`

**`cron.go`** — Cron expression matching via `robfig/cron/v3`

**`runner.go`** — Background executor
- Ticker-based evaluation loop: execute current action → evaluate transitions → batch save state
- Action implementations: close/open/delete/read_only via registry, force_merge via CHECKPOINT+VACUUM, notification via webhook with text/template rendering, snapshot via file copy, rollover via numeric suffix increment + alias update
- Retry with exponential backoff, failure marking, manual retry reset
- `AliasUpdater` interface to avoid circular dependency with index package

### Tests (52 new tests)
- `validation_test.go` — 13 tests
- `manager_test.go` — 17 tests
- `runner_test.go` — 15 tests
- `handlers_ism_test.go` — 7 tests

### API (`internal/api/handlers_ism.go`)
9 HTTP endpoints:
- `PUT/GET/DELETE /ism/policies/{name}` — Policy CRUD (auto-detects YAML vs JSON via Content-Type)
- `GET /ism/policies` — List all policies
- `POST /ism/attach/{index}` / `POST /ism/detach/{index}` — Attach/detach policy
- `GET /ism/status/{index}` / `GET /ism/status` — Per-index or all statuses
- `POST /ism/retry/{index}` — Reset failed action for retry

### CLI (`cmd/duckdb-cluster/cmd_ism.go`)
8 subcommands: list, create (--file), delete, get, status, attach, detach, retry

## Modified Files

- **`internal/config/config.go`** — Added `ISMConfig` (Enabled, RunInterval, PolicyDir) with defaults
- **`internal/index/index.go`** — Added `ReadOnly bool` to `Metadata`
- **`internal/index/registry.go`** — Added `SetReadOnly()`, `SetOnIndexCreated()` callback hook
- **`internal/index/alias.go`** — Added `GetIndices()` method for rollover support
- **`internal/api/server.go`** — Wire ISM manager/runner, register 9 routes, start/stop runner lifecycle
- **`cmd/duckdb-cluster/main.go`** — Added `ism` command case
- **`go.mod`** — Added `github.com/robfig/cron/v3 v3.0.1`

## Architecture Decisions

- ISM state stored in separate file (`_ism_state.json`) from index metadata to avoid write contention
- `AliasUpdater` interface in ism package avoids circular dependency between ism and index
- ISM manager always created (for API/CLI), runner only started when `ism.enabled: true`
- Auto-attach via `onIndexCreated` callback — new indices matching ISMTemplate patterns auto-managed
- Policies accept YAML or JSON via Content-Type header detection

## Example Policy (YAML)

```yaml
name: logs-lifecycle
description: "Log lifecycle: hot -> warm (read-only) -> delete"
default_state: hot
ism_template:
  - pattern: "logs-*"
    priority: 100
states:
  - name: hot
    transitions:
      - state_name: warm
        conditions:
          min_index_age: 168h  # 7 days
  - name: warm
    actions:
      - type: read_only
      - type: force_merge
    transitions:
      - state_name: delete
        conditions:
          min_index_age: 720h  # 30 days
  - name: delete
    actions:
      - type: delete
```
