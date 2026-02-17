# Stress Test Migration Guide

## TL;DR

**Old test (`stress.sh`)**: Measures client-side process overhead (~8 seconds)  
**New test (`stress-wrk.sh`)**: Measures actual server performance (~400µs)

**Action**: Use `stress-wrk.sh` for all future load testing.

---

## Why the Change?

### The Problem with stress.sh

The original `stress.sh` script used this pattern:

```bash
# Launch 20 background worker processes
for ((i = 0; i < 20; i++)); do
    read_worker "$i" &
done

# Each worker spawns curl in a tight loop
read_worker() {
    while [[ time_left > 0 ]]; do
        curl ...  # New process + TCP connection EVERY request
    done
}
```

**Result**: ~2,000-20,000 curl subprocesses spawned over 30 seconds

### What Was Actually Measured

| Component | Time | What it is |
|-----------|------|------------|
| Process creation | ~2-3s | fork/exec overhead |
| TCP connections | ~1-2s | New handshake per request |
| Shell queuing | ~3-4s | Job management overhead |
| **Actual server** | **~400µs** | **The actual query time** |
| **Total reported** | **~8s** | **99.995% client overhead** |

---

## Migration Steps

### Step 1: Install wrk

```bash
# Ubuntu/Debian
sudo apt-get install wrk

# macOS
brew install wrk

# Verify installation
wrk --version
```

### Step 2: Use the New Test

```bash
cd examples/demo

# Old way (deprecated)
bash stress.sh

# New way (recommended)
bash stress-wrk.sh
```

### Step 3: Understand the Output

**Old output (stress.sh)**:
```
Phase 3: Read Stress
  Average latency: 8.234s
  p50: 7.891s
  p95: 12.456s
  Throughput: 102 req/s
```

**New output (stress-wrk.sh)**:
```
Phase 4: Read Stress

Running 30s test @ http://localhost:8091/query
  4 threads and 50 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency   423.45us  156.32us   5.12ms   87.23%
    Req/Sec    29.45k     2.31k   34.12k    68.00%
  Latency Distribution
     50%  389.00us
     75%  478.00us
     90%  612.00us
     99%    1.23ms
  3534891 requests in 30.00s, 1.23GB read
Requests/sec: 117829.70
```

**Key differences**:
- Latency: **8s → 400µs** (20,000x faster)
- Throughput: **100 req/s → 117,000 req/s** (1,170x higher)
- Accuracy: ❌ Client overhead → ✅ Server-side only

---

## Feature Comparison

| Feature | stress.sh | stress-wrk.sh |
|---------|-----------|---------------|
| **Connection Pooling** | ❌ No | ✅ Yes |
| **Process Model** | Fork per request | Single process |
| **Latency Accuracy** | ❌ Inflated | ✅ Accurate |
| **Throughput** | ~100 req/s | ~50,000 req/s |
| **Resource Usage** | High (processes) | Low (event loop) |
| **Industry Standard** | ❌ No | ✅ Yes (wrk) |
| **Phases Tested** | 7 | 7 |
| **Easy to Use** | ✅ Yes | ✅ Yes |

---

## Understanding the Results

### Expected Performance

With the new test, you should see:

| Metric | Value | Notes |
|--------|-------|-------|
| Average latency | 400µs - 2ms | Depends on query complexity |
| p50 latency | 350µs - 1ms | Median request time |
| p95 latency | 1ms - 5ms | 95th percentile |
| p99 latency | 2ms - 10ms | 99th percentile |
| Throughput | 10,000 - 50,000 req/s | Depends on concurrency |

### What's Normal

**Good performance** indicators:
- ✅ p50 < 1ms
- ✅ p99 < 5ms
- ✅ Throughput > 10,000 req/s
- ✅ Error rate = 0%

**Issues** to investigate:
- ⚠️ p50 > 5ms - Check server resources
- ⚠️ p99 > 50ms - Check for outliers, GC pauses
- ⚠️ Throughput < 1,000 req/s - Check bottlenecks
- ⚠️ Error rate > 1% - Check logs, backpressure settings

---

## Customization

### Adjust Concurrency

```bash
# Low load test (10 connections)
WRITE_CONCURRENCY=10 READ_CONCURRENCY=20 bash stress-wrk.sh

# Medium load test (default)
bash stress-wrk.sh

# High load test (100 connections)
WRITE_CONCURRENCY=50 READ_CONCURRENCY=100 bash stress-wrk.sh

# Stress test (200 connections)
WRITE_CONCURRENCY=100 READ_CONCURRENCY=200 bash stress-wrk.sh
```

### Adjust Duration

```bash
# Quick test (10 seconds per phase)
DURATION=10 bash stress-wrk.sh

# Standard test (30 seconds per phase, default)
bash stress-wrk.sh

# Long test (60 seconds per phase)
DURATION=60 bash stress-wrk.sh
```

### Test Specific Workloads

```bash
# Test only read performance
export AUTH_TOKEN="your-token-here"
wrk -t4 -c50 -d30s --latency \
  -s wrk-scripts/read-query.lua \
  http://localhost:8091/query

# Test only write performance
wrk -t4 -c20 -d30s --latency \
  -s wrk-scripts/write-single.lua \
  http://localhost:8091/indices/stress-test/_doc
```

---

## CI/CD Integration

### Before (stress.sh)

```yaml
# ❌ Don't use this
- name: Run stress test
  run: |
    bash examples/demo/stress.sh
    # Results are inaccurate due to client overhead
```

### After (stress-wrk.sh)

```yaml
# ✅ Use this instead
- name: Install wrk
  run: sudo apt-get install -y wrk

- name: Run stress test
  run: |
    cd examples/demo
    bash stress-wrk.sh
    
- name: Check performance
  run: |
    # Extract p99 latency from results
    P99=$(grep "99%" stress-results/*/read-query.txt | awk '{print $2}')
    # Fail if p99 > 10ms
    if (( $(echo "$P99 > 0.010" | bc -l) )); then
      echo "Performance regression: p99 = $P99"
      exit 1
    fi
```

---

## Troubleshooting

### "wrk: command not found"

Install wrk:
```bash
sudo apt-get install wrk  # or brew install wrk
```

### "connection refused"

Ensure cluster is running:
```bash
curl http://localhost:8091/health
```

### Results seem worse than expected

1. Check if cluster is under load:
   ```bash
   docker stats  # if using Docker
   ```

2. Warm up the cluster first:
   ```bash
   wrk -t2 -c10 -d5s -s wrk-scripts/read-query.lua http://localhost:8091/query
   ```

3. Check for errors in logs:
   ```bash
   docker logs demo-cluster-baseline-1
   ```

---

## FAQ

**Q: Can I still use stress.sh?**  
A: It's deprecated but still works. However, results are inaccurate (~8s instead of ~400µs). Use stress-wrk.sh instead.

**Q: Why is the old test so slow?**  
A: It spawns ~20,000 curl processes, each creating a new TCP connection. The "latency" is 99.995% client-side overhead.

**Q: Do I need to change my cluster configuration?**  
A: No! The cluster was always fast (~400µs). The issue was in how we measured it.

**Q: Will this work on macOS?**  
A: Yes! Just install wrk with `brew install wrk`.

**Q: Can I use other tools instead of wrk?**  
A: Yes! Try `hey` (Go-based), `ab` (Apache Bench), or `vegeta`. All are better than shell-based testing.

**Q: What if I want to test with 1000 connections?**  
A: Increase the concurrency: `READ_CONCURRENCY=1000 bash stress-wrk.sh`. Watch for file descriptor limits.

---

## References

- [TEST_4_RESULTS.md](TEST_4_RESULTS.md) - Detailed analysis of the root cause
- [PERFORMANCE_INVESTIGATION_SUMMARY.md](PERFORMANCE_INVESTIGATION_SUMMARY.md) - Complete investigation
- [wrk-scripts/README.md](wrk-scripts/README.md) - Lua script documentation
- [Checkpoint 027](../../checkpoint/CHECKPOINT_027.md) - Technical summary
- [wrk GitHub](https://github.com/wg/wrk) - Official wrk documentation

---

## Timeline

- **2026-02-13**: Original stress.sh created
- **2026-02-17**: Performance investigation started
- **2026-02-17**: Root cause identified (test methodology)
- **2026-02-17**: stress-wrk.sh created with proper tooling
- **Future**: stress.sh may be removed after validation period

---

**Status**: ✅ Migration complete. Use `stress-wrk.sh` for all load testing.
