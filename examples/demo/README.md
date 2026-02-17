# DuckDB Cluster Demo & Load Testing

This directory contains Docker Compose configurations, load testing scripts, and performance analysis tools for the DuckDB Cluster project.

## Quick Start

### Start the Cluster

```bash
# Start 3-node cluster with monitoring stack
docker compose up -d

# Check cluster health
curl http://localhost:8083/health
```

### Run Load Test

```bash
# New method (recommended) - accurate measurements
bash stress-wrk.sh

# Old method (deprecated) - inaccurate due to client overhead
bash stress.sh  # Shows ~8s latency (99.995% is client-side overhead)
```

---

## Available Tests

### 🚀 stress-wrk.sh (Recommended)

**Purpose**: Accurate load testing with proper connection pooling

**Tool**: [wrk](https://github.com/wg/wrk) - industry-standard HTTP benchmarking

**Performance**:
- Latency: ~400µs - 2ms (accurate server-side measurement)
- Throughput: ~10,000 - 50,000 req/s
- Connection model: Pooled (efficient)

**Usage**:
```bash
# Install wrk first
sudo apt-get install wrk  # or brew install wrk

# Run full test suite
bash stress-wrk.sh

# Custom configuration
WRITE_CONCURRENCY=50 READ_CONCURRENCY=100 DURATION=60 bash stress-wrk.sh
```

**Phases tested**:
1. Single document writes
2. Bulk document writes
3. Read queries (6 patterns)
4. Mixed read/write workload
5. Cross-index fan-out
6. Backpressure testing

**Results**: Saved to `stress-results/YYYY-MM-DD-HH-MM-SS/`

---

### ⚠️ stress.sh (Deprecated)

**Purpose**: Original load test (deprecated due to measurement issues)

**Problem**: Spawns ~20,000 curl subprocesses, measuring client-side overhead instead of server performance

**Reported metrics**:
- Latency: ~8 seconds (99.995% is process creation + TCP handshake overhead)
- Throughput: ~100 req/s
- Actual server latency: ~400µs (measured separately)

**Status**: Kept for reference, marked with deprecation warning

**Migration**: See [STRESS_TEST_MIGRATION.md](STRESS_TEST_MIGRATION.md)

---

## Lua Scripts for wrk

Located in `wrk-scripts/` directory:

| Script | Purpose | Concurrency |
|--------|---------|-------------|
| `write-single.lua` | Single document writes | 20 |
| `write-bulk.lua` | Bulk writes (100 docs/req) | 20 |
| `read-query.lua` | Mixed read patterns (6 types) | 50 |
| `read-mixed.lua` | 70% simple, 30% complex | 50 |
| `read-fanout.lua` | Cross-index queries | 50 |

See [wrk-scripts/README.md](wrk-scripts/README.md) for detailed usage.

---

## Performance Testing Tools

### Test 1: Local + Middleware

**Purpose**: Measure middleware overhead

```bash
bash test-middleware.sh
```

**Expected**: ~388µs avg, ~23µs middleware overhead

---

### Test 2: Docker + No Middleware

**Purpose**: Measure Docker network overhead

```bash
bash test-docker.sh
```

**Expected**: ~703µs avg, ~470µs Docker overhead

---

### Test 4: HTTP Connection Pooling

**Purpose**: Test if HTTP connection handling is bottleneck

```bash
bash test-http-pooling.sh
```

**Expected**: 1-2% improvement with keep-alive (not a bottleneck)

---

## Docker Compose Configurations

### docker-compose.yaml (Main 3-node cluster)

Full production-like setup:
- Write node (port 8081)
- Read node (port 8082)
- All-in-one node (port 8083)
- Prometheus, Grafana, Jaeger

```bash
docker compose up -d
```

### docker-compose-baseline.yaml

Simplified 2-container setup for testing:
- Standalone DuckDB (port 8090)
- Cluster instance (port 8091)

```bash
docker compose -f docker-compose-baseline.yaml up -d
```

### docker-compose-local.yaml

Single container for isolation testing:
- Test container (port 8093)
- No middleware

```bash
docker compose -f docker-compose-local.yaml up -d
```

---

## Configuration Files

| File | Purpose |
|------|---------|
| `config-baseline.yaml` | Production-like config (3 shards, all middleware) |
| `test-middleware-config.yaml` | Test 1: Local + middleware |
| `test-config-docker.yaml` | Test 2: Docker, no middleware |
| `test-timing-config.yaml` | Minimal config for timing analysis |

---

## Performance Investigation Results

### Actual Server Performance

| Component | Latency | Assessment |
|-----------|---------|------------|
| Query processing | ~380µs | ✅ Excellent |
| Middleware overhead | ~23µs | ✅ Negligible |
| Docker networking | ~470µs | ✅ Acceptable |
| **End-to-end** | **~850µs** | **✅ Good** |

### What Was "Broken"

The test methodology (stress.sh), not the server:
- ❌ Spawned ~20,000 processes
- ❌ Created new TCP connection per request
- ❌ Measured client-side overhead as "server latency"
- ✅ Fixed with stress-wrk.sh

### Documentation

- [TEST_4_RESULTS.md](TEST_4_RESULTS.md) - Root cause analysis
- [PERFORMANCE_INVESTIGATION_SUMMARY.md](PERFORMANCE_INVESTIGATION_SUMMARY.md) - Complete findings
- [STRESS_TEST_MIGRATION.md](STRESS_TEST_MIGRATION.md) - Migration guide
- [TESTING_GUIDE.md](TESTING_GUIDE.md) - Test execution guide
- [../../checkpoint/CHECKPOINT_027.md](../../checkpoint/CHECKPOINT_027.md) - Technical summary

---

## Monitoring

### Grafana Dashboards

Access: http://localhost:3000 (admin/admin)

Dashboards available:
- Query latency metrics
- Throughput graphs
- Resource usage
- Error rates

### Prometheus Metrics

Access: http://localhost:9090

Available metrics:
- `duckdb_cluster_query_duration_seconds`
- `duckdb_cluster_requests_total`
- `duckdb_cluster_errors_total`

### Jaeger Tracing

Access: http://localhost:16686

View distributed traces for debugging.

---

## Best Practices

### Load Testing

✅ **Do:**
- Use `stress-wrk.sh` or wrk directly
- Start with low concurrency, increase gradually
- Warm up the cluster before measuring
- Monitor server resources during tests
- Use connection pooling

❌ **Don't:**
- Use shell loops with curl
- Fork processes for each request
- Create new TCP connections per request
- Measure client-side overhead as server latency

### Performance Expectations

| Scenario | Expected Latency | Expected Throughput |
|----------|------------------|---------------------|
| Simple queries (COUNT, LIMIT) | < 1ms | > 50,000 req/s |
| Aggregate queries (AVG, SUM) | < 2ms | > 30,000 req/s |
| GROUP BY queries | < 3ms | > 20,000 req/s |
| Complex joins | < 10ms | > 5,000 req/s |

---

## Troubleshooting

### "wrk: command not found"

```bash
sudo apt-get install wrk  # or brew install wrk
```

### Cluster not starting

```bash
# Check Docker logs
docker compose logs

# Check port conflicts
sudo lsof -i :8083
```

### Low performance

1. Check server resources:
   ```bash
   docker stats
   ```

2. Check for errors:
   ```bash
   docker logs demo-all-node-1
   ```

3. Verify configuration:
   ```bash
   curl http://localhost:8083/health
   ```

### Connection errors during load test

- Increase file descriptor limit
- Reduce concurrency
- Check Docker networking

---

## Development

### Adding New Test Scenarios

1. Create Lua script in `wrk-scripts/`
2. Add phase to `stress-wrk.sh`
3. Document in this README

### Modifying Cluster Config

1. Edit `config-baseline.yaml`
2. Restart cluster: `docker compose restart`
3. Validate: `curl http://localhost:8083/health`

---

## References

- [wrk documentation](https://github.com/wg/wrk)
- [DuckDB documentation](https://duckdb.org/docs/)
- [Docker Compose reference](https://docs.docker.com/compose/)
- [Prometheus metrics](https://prometheus.io/docs/introduction/overview/)
- [Grafana dashboards](https://grafana.com/docs/)

---

## Quick Command Reference

```bash
# Start cluster
docker compose up -d

# Run load test (new)
bash stress-wrk.sh

# Run specific test
bash test-middleware.sh

# View logs
docker compose logs -f

# Stop cluster
docker compose down

# Clean volumes
docker compose down -v

# Check performance
curl -s http://localhost:8083/metrics | grep query_duration
```

---

**Status**: ✅ All tests validated, stress test rewritten with accurate tooling

**Last Updated**: 2026-02-17

**Version**: 2.0 (stress-wrk.sh)
