# DuckDB Cluster Observability

This document describes the observability features in duckdb-cluster, including metrics, distributed tracing, and structured logging.

## Overview

DuckDB Cluster implements production-grade observability with:

- **Prometheus Metrics** — Performance counters, latencies, health status
- **Distributed Tracing** — OpenTelemetry-compatible request tracing  
- **Structured Logging** — JSON/text logging with trace context integration
- **Grafana Dashboards** — Pre-built visualization templates

## Quick Start

### 1. Configure Observability

Edit your `config.yaml`:

```yaml
observability:
  metrics:
    enabled: true
    path: "/metrics"
    namespace: "duckdb_cluster"
  
  tracing:
    enabled: true
    otlp_endpoint: "localhost:4317"  # Jaeger/OpenTelemetry collector
    service_name: "duckdb-cluster"
    environment: "production"
    sample_rate: 1.0  # 0.0-1.0 (1.0 = trace everything)
  
  logging:
    level: "info"  # debug, info, warn, error
    format: "json"  # json or text
```

### 2. Start Observability Stack

```bash
# Start Prometheus, Jaeger, and Grafana
docker-compose -f docker-compose-observability.yml up -d

# Access UIs:
# - Prometheus: http://localhost:9090
# - Jaeger UI: http://localhost:16686
# - Grafana: http://localhost:3000 (admin/admin)
```

### 3. Start Cluster with Observability

```bash
./duckdb-cluster start --config config.yaml
```

### 4. View Metrics and Traces

- **Metrics Endpoint**: `curl http://localhost:8080/metrics`
- **Grafana Dashboards**: Navigate to http://localhost:3000 → Dashboards → DuckDB Cluster
- **Jaeger Traces**: http://localhost:16686 → Search for service "duckdb-cluster"

---

## Metrics

### Available Metrics

#### Write Path
| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `duckdb_cluster_write_total` | Counter | Total write operations | `operation`, `status` |
| `duckdb_cluster_write_latency_seconds` | Histogram | Write latency distribution | `operation`, `shard` |
| `duckdb_cluster_write_errors_total` | Counter | Write errors | `operation`, `error_type` |
| `duckdb_cluster_replication_success_total` | Counter | Successful replications | `target_node` |
| `duckdb_cluster_replication_failure_total` | Counter | Failed replications | `target_node`, `reason` |
| `duckdb_cluster_replication_latency_seconds` | Histogram | Replication latency | `target_node` |

#### Read Path
| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `duckdb_cluster_query_total` | Counter | Total queries | `query_type`, `status` |
| `duckdb_cluster_query_latency_seconds` | Histogram | Query latency distribution | `query_type`, `consistency_level` |
| `duckdb_cluster_query_errors_total` | Counter | Query errors | `query_type`, `error_type` |
| `duckdb_cluster_merge_latency_seconds` | Histogram | Result merge latency | - |
| `duckdb_cluster_shard_query_latency_seconds` | Histogram | Per-shard query latency | `shard_id` |

#### Node Health
| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `duckdb_cluster_node_health_status` | Gauge | Node health (0=down, 1=unhealthy, 2=healthy) | `node_id` |
| `duckdb_cluster_node_failure_total` | Counter | Node failure count | `node_id` |
| `duckdb_cluster_node_recovery_total` | Counter | Node recovery count | `node_id` |
| `duckdb_cluster_circuit_breaker_state` | Gauge | Circuit breaker state (0=closed, 1=half-open, 2=open) | `node_id` |

#### gRPC
| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `duckdb_cluster_grpc_request_total` | Counter | Total gRPC requests | `service`, `method` |
| `duckdb_cluster_grpc_request_duration_seconds` | Histogram | gRPC request duration | `service`, `method`, `status` |
| `duckdb_cluster_grpc_request_errors_total` | Counter | gRPC errors | `service`, `method`, `code` |

#### Cluster
| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `duckdb_cluster_shard_count` | Gauge | Number of shards | - |
| `duckdb_cluster_active_connections` | Gauge | Active HTTP connections | - |
| `duckdb_cluster_replication_factor` | Gauge | Replication factor | - |

### Querying Metrics

**PromQL Examples:**

```promql
# Average write latency (p95) over last 5 minutes
histogram_quantile(0.95, rate(duckdb_cluster_write_latency_seconds_bucket[5m]))

# Query throughput (queries per second)
rate(duckdb_cluster_query_total[1m])

# Error rate
rate(duckdb_cluster_write_errors_total[5m]) / rate(duckdb_cluster_write_total[5m])

# Replication lag by node
histogram_quantile(0.99, rate(duckdb_cluster_replication_latency_seconds_bucket[5m])) by (target_node)

# Nodes down
sum(duckdb_cluster_node_health_status == 0)
```

---

## Distributed Tracing

### How It Works

1. Each request generates a unique **trace ID**
2. Operations create **spans** (units of work)
3. Spans are linked hierarchically (parent → child)
4. Traces are exported to Jaeger/Zipkin via OTLP

### Trace Hierarchy Example

```
POST /query (partition_key=user-123)
 ├─ Distributor.Push
 │   └─ ReplicateWrite
 │       ├─ gRPC → node-0
 │       ├─ gRPC → node-1
 │       └─ gRPC → node-2
 └─ Ingester.Push
     └─ Router.Route
         └─ Shard-5.Exec
```

### Using Traces

**In Jaeger UI:**

1. Select service: `duckdb-cluster`
2. Filter by:
   - Operation (e.g., `Distributor.Push`, `ReplicateWrite`)
   - Tags (e.g., `partition_key=user-123`, `error=true`)
   - Duration (e.g., `>100ms`)
3. Click a trace to see:
   - Timeline view of all spans
   - Errors and events
   - Custom attributes (partition keys, row counts, etc.)

**Trace Attributes:**

- `partition_key` — Write routing key
- `sql_length` — Query length in bytes
- `rows_affected` — Rows modified/returned
- `shard_id` — Target shard ID
- `replicated` — Whether write was replicated
- `consistency_level` — Read consistency (ONE/QUORUM/ALL)
- `error` — Error flag (true/false)

---

## Structured Logging

### Log Levels

- `debug` — Detailed diagnostic information
- `info` — General informational messages (default)
- `warn` — Warning messages (e.g., retries, degraded performance)
- `error` — Error messages (failures)

### Log Formats

**Text (Human-readable):**
```
2026-02-13T12:30:00.123Z INFO distributor pushing write sql_length=42 partition_key=user-123
```

**JSON (Machine-parseable):**
```json
{
  "time": "2026-02-13T12:30:00.123Z",
  "level": "INFO",
  "msg": "distributor pushing write",
  "sql_length": 42,
  "partition_key": "user-123",
  "trace_id": "a1b2c3d4e5f6...",
  "span_id": "1a2b3c4d..."
}
```

### Trace Context Integration

Logs automatically include `trace_id` and `span_id` when tracing is enabled, allowing you to:
- Jump from logs to traces
- Correlate errors across distributed requests
- Debug with full context

---

## Grafana Dashboards

### Included Dashboards

1. **Cluster Overview** (`grafana/dashboards/cluster-overview.json`)
   - Shard count, replication factor, active connections
   - Write/query throughput
   - Latency percentiles (p50/p95/p99)
   - Node health status

2. **Replication** (`grafana/dashboards/replication.json`)
   - Replication success/failure rates
   - Replication latency by target node
   - Circuit breaker states
   - Node failure/recovery events

### Importing Dashboards

**Option 1: Auto-provisioning (Docker Compose)**

Dashboards are automatically loaded when using `docker-compose-observability.yml`.

**Option 2: Manual Import**

1. Open Grafana: http://localhost:3000
2. Navigate to **Dashboards** → **Import**
3. Upload JSON file from `grafana/dashboards/`
4. Select **Prometheus** as data source

### Creating Custom Dashboards

Use the provided dashboards as templates. Common panel types:

- **Time Series** — Latency, throughput over time
- **Stat** — Single value (current shard count, replication factor)
- **Gauge** — Node health status
- **Table** — Per-node/per-shard breakdowns

---

## Production Best Practices

### 1. Sampling

For high-traffic deployments, use sampling to reduce overhead:

```yaml
tracing:
  sample_rate: 0.1  # Trace 10% of requests
```

### 2. Retention

Configure Prometheus retention:

```yaml
# prometheus.yml
global:
  retention: 30d  # Keep metrics for 30 days
```

### 3. Alerting

**Prometheus Alerting Rules (example):**

```yaml
groups:
  - name: duckdb_cluster
    rules:
      - alert: HighWriteLatency
        expr: histogram_quantile(0.95, rate(duckdb_cluster_write_latency_seconds_bucket[5m])) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High write latency (p95 > 1s)"
      
      - alert: NodeDown
        expr: duckdb_cluster_node_health_status == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Node {{ $labels.node_id }} is down"
      
      - alert: HighErrorRate
        expr: rate(duckdb_cluster_write_errors_total[5m]) / rate(duckdb_cluster_write_total[5m]) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Error rate > 5%"
```

### 4. Log Aggregation

For distributed deployments, aggregate logs with:
- **Loki** (Grafana's log aggregation)
- **Elasticsearch + Filebeat**
- **Fluentd/Fluent Bit**

Filter logs by `trace_id` to debug specific requests.

---

## Troubleshooting

### Metrics Not Appearing

1. Check metrics endpoint: `curl http://localhost:8080/metrics`
2. Verify Prometheus is scraping:
   - Prometheus UI → Status → Targets
   - Should see `duckdb-cluster` target as **UP**
3. Check Prometheus config (`observability/prometheus.yml`)

### Traces Not Appearing

1. Verify tracing is enabled in `config.yaml`
2. Check Jaeger is running: `docker ps | grep jaeger`
3. Test OTLP endpoint: `telnet localhost 4317`
4. Check logs for tracing errors

### High Latency

Use tracing to identify bottlenecks:
1. Jaeger UI → Find traces with high duration
2. Expand trace → Identify slow spans
3. Common causes:
   - Network latency (replication)
   - Slow shard queries (check DuckDB query plans)
   - Lock contention (concurrent writes)

### Memory Leaks

Monitor with:
```promql
process_resident_memory_bytes{job="duckdb-cluster"}
```

If growing unbounded:
- Check for unclosed database connections
- Review shard cache settings
- Profile with Go pprof: `curl http://localhost:8080/debug/pprof/heap > heap.prof`

---

## Architecture

### Metrics Collection Flow

```
Application Code
  ↓
Metrics Recording (Prometheus client)
  ↓
/metrics HTTP Endpoint
  ↓
Prometheus (scrapes every 10s)
  ↓
Grafana (queries Prometheus)
```

### Tracing Export Flow

```
Application Code (span.Start/End)
  ↓
OpenTelemetry SDK (batching)
  ↓
OTLP Exporter (gRPC)
  ↓
Jaeger Collector (localhost:4317)
  ↓
Jaeger Storage (in-memory or Cassandra)
  ↓
Jaeger UI (query interface)
```

---

## Configuration Reference

### Full Example

```yaml
observability:
  metrics:
    enabled: true          # Enable Prometheus metrics
    path: "/metrics"       # Metrics HTTP endpoint
    namespace: "duckdb_cluster"  # Metric name prefix
  
  tracing:
    enabled: true          # Enable distributed tracing
    otlp_endpoint: "localhost:4317"  # OpenTelemetry collector
    service_name: "duckdb-cluster"   # Service name in traces
    environment: "production"         # Environment tag
    sample_rate: 1.0       # Sampling rate (0.0-1.0)
  
  logging:
    level: "info"          # Log level: debug, info, warn, error
    format: "json"         # Format: json or text
```

### Environment Variables

Override config with environment variables:

```bash
export DUCKDB_CLUSTER_TRACING_ENABLED=true
export DUCKDB_CLUSTER_TRACING_ENDPOINT=localhost:4317
export DUCKDB_CLUSTER_LOG_LEVEL=debug
export DUCKDB_CLUSTER_LOG_FORMAT=json
```

---

## Performance Impact

| Feature | CPU Overhead | Memory Overhead | Network Overhead |
|---------|--------------|-----------------|------------------|
| Metrics (Prometheus) | ~0.1% | ~10 MB | Negligible (pull-based) |
| Tracing (100% sampling) | ~2-5% | ~50 MB | ~10 KB/request |
| Tracing (10% sampling) | ~0.5% | ~10 MB | ~1 KB/request |
| JSON Logging | ~0.5% | ~5 MB | Negligible |

**Recommendation**: Use 100% sampling in development, 10-20% in production.

---

## FAQ

**Q: Can I disable observability?**  
A: Yes. Set `enabled: false` for metrics and tracing in `config.yaml`.

**Q: Do I need Jaeger for metrics?**  
A: No. Metrics (Prometheus) and tracing (Jaeger) are independent.

**Q: Can I use Zipkin instead of Jaeger?**  
A: Yes. Change `otlp_endpoint` to your Zipkin collector's OTLP endpoint.

**Q: How do I export traces to AWS X-Ray / Google Cloud Trace?**  
A: Use OpenTelemetry Collector as a bridge. Point `otlp_endpoint` to the collector, which exports to your cloud provider.

**Q: Are metrics/traces stored forever?**  
A: No. Configure retention in Prometheus/Jaeger. Default: 15 days (Prometheus), in-memory (Jaeger).

---

## References

- [Prometheus Documentation](https://prometheus.io/docs/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/languages/go/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Grafana Dashboards](https://grafana.com/docs/grafana/latest/dashboards/)
- [PromQL Cheat Sheet](https://promlabs.com/promql-cheat-sheet/)
