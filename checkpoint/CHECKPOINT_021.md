# Checkpoint 021 — Observability Stack + UAT Fixes

**Date:** 2026-02-15
**Status:** Compiles clean, all Go tests pass (280 tests). UAT passes (31 tests, up from 27).

## What Changed

- Fixed 11 UAT test failures in `test.sh`: added `_id` fields for partition key, switched bulk ingest from JSON array to NDJSON, fixed template field names (`pattern` instead of `index_patterns`, `mapping` instead of `mappings`)
- Added Prometheus, Jaeger, and Grafana to the distributed Docker Compose
- Created a dedicated Prometheus config for multi-node scraping (3 targets by Docker service name)
- Enabled OpenTelemetry tracing in all 3 node configs pointing to `jaeger:4317`
- Added 4 observability UAT tests (Prometheus health, Prometheus targets, Jaeger UI, Grafana health)
- Added `wait_for_url` helper to test script for observability service readiness

## Current State

The distributed Docker Compose (`examples/distributed/`) now includes a full observability stack:
- **Prometheus** (port 9090) scrapes metrics from all 3 cluster nodes every 10s
- **Jaeger** (port 16686 UI, port 4317 OTLP) receives traces from all nodes
- **Grafana** (port 3000) with pre-provisioned Prometheus datasource and dashboards

All 31 UAT tests pass: 27 existing cluster tests + 4 new observability tests.

## Files Modified

| File | Change |
|---|---|
| `examples/distributed/test.sh` | Fixed doc ingestion (added `_id`), bulk format (NDJSON), template fields (`pattern`/`mapping`); added observability test section with 4 tests and `wait_for_url` helper |
| `examples/distributed/docker-compose.yaml` | Added prometheus, jaeger, grafana services; added prometheus-data and grafana-data volumes |
| `examples/distributed/config-write.yaml` | Added tracing config (enabled, jaeger:4317, sample_rate 1.0) |
| `examples/distributed/config-read.yaml` | Added tracing config (enabled, jaeger:4317, sample_rate 1.0) |
| `examples/distributed/config-all.yaml` | Added tracing config (enabled, jaeger:4317, sample_rate 1.0) |

## New Files

| File | Description |
|---|---|
| `examples/distributed/prometheus.yml` | Prometheus scrape config targeting write-node:8080, read-node:8080, all-node:8080 |

## Tests

280 Go tests, all passing. 31 UAT tests, all passing (4 new observability tests).

## Known Issues

None.

## Next Steps

- Add CI pipeline (GitHub Actions) using the Dockerfile and UAT script
- Create Grafana dashboards specific to distributed mode (per-node views)
- Add alerting rules to Prometheus config
