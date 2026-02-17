# wrk Load Testing Scripts

This directory contains Lua scripts for use with [wrk](https://github.com/wg/wrk), an HTTP benchmarking tool designed for accurate load testing.

## Why wrk?

The original `stress.sh` script used shell background jobs with individual curl processes, which introduced massive client-side overhead (~8 seconds measured, but only ~400µs actual server time). These wrk scripts use proper connection pooling and event-driven I/O for accurate measurements.

**Performance comparison:**
- Old `stress.sh`: ~8000ms latency, ~100 req/s
- New wrk-based: ~0.4-2ms latency, ~10,000-50,000 req/s

## Installation

```bash
# Ubuntu/Debian
sudo apt-get install wrk

# macOS
brew install wrk

# From source
git clone https://github.com/wg/wrk.git
cd wrk
make
sudo cp wrk /usr/local/bin/
```

## Available Scripts

### write-single.lua
Single document writes with unique IDs.

```bash
export AUTH_TOKEN="your-token-here"
wrk -t4 -c20 -d30s --latency \
  -s wrk-scripts/write-single.lua \
  http://localhost:8091/indices/stress-test/_doc
```

### write-bulk.lua
Bulk document writes (100 documents per request by default).

```bash
export AUTH_TOKEN="your-token-here"
wrk -t4 -c20 -d30s --latency \
  -s wrk-scripts/write-bulk.lua \
  http://localhost:8091/indices/stress-test/_bulk
```

### read-query.lua
Read query workload with 6 different query patterns (rotating).

```bash
export AUTH_TOKEN="your-token-here"
wrk -t4 -c50 -d30s --latency \
  -s wrk-scripts/read-query.lua \
  http://localhost:8091/query
```

Queries included:
- `SELECT COUNT(*)`
- `SELECT * LIMIT 10`
- `SELECT AVG(value)`
- `SELECT category, COUNT(*) GROUP BY category`
- `SELECT * WHERE value > 500 LIMIT 20`
- `SELECT category, AVG(value) GROUP BY category ORDER BY avg_val DESC`

### read-mixed.lua
Mixed read patterns (70% simple, 30% complex queries).

```bash
export AUTH_TOKEN="your-token-here"
wrk -t4 -c50 -d30s --latency \
  -s wrk-scripts/read-mixed.lua \
  http://localhost:8091/query
```

### read-fanout.lua
Cross-index fan-out queries (requires fanout-1, fanout-2, fanout-3 indices).

```bash
export AUTH_TOKEN="your-token-here"
wrk -t4 -c50 -d30s --latency \
  -s wrk-scripts/read-fanout.lua \
  http://localhost:8091/query
```

## Parameters

- `-t` (threads): Number of worker threads (default: 2, recommended: 4-8)
- `-c` (connections): Number of concurrent connections (default: 10, test with 20-200)
- `-d` (duration): Test duration (e.g., `30s`, `1m`, `5m`)
- `--latency`: Print detailed latency statistics (p50, p75, p90, p99)

## Understanding Output

```
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
Transfer/sec:     42.01MB
```

**Key metrics:**
- **Latency (Avg)**: Average request time (should be ~400µs-2ms)
- **p99**: 99th percentile latency (should be <5ms)
- **Requests/sec**: Total throughput (should be >10,000 req/s)

## Customization

To modify a script, edit the Lua file directly. Common modifications:

### Change bulk size (write-bulk.lua)
```lua
local bulk_size = 200  -- default is 100
```

### Add new query patterns (read-query.lua)
```lua
local queries = {
    '{"sql":"YOUR NEW QUERY","index":"stress-test"}',
    -- ... existing queries
}
```

### Adjust simple/complex ratio (read-mixed.lua)
```lua
-- 70% simple queries, 30% complex
if counter % 10 < 7 then  -- Change to < 8 for 80/20 split
    query = simple_queries[...]
```

## Best Practices

1. **Warm up the cluster** before measuring:
   ```bash
   wrk -t2 -c10 -d5s -s read-query.lua http://localhost:8091/query
   ```

2. **Start with low concurrency** and increase:
   - Start: `-c10`
   - Medium: `-c50`
   - High: `-c100`
   - Stress: `-c200`

3. **Use appropriate thread count**:
   - Low latency: 2-4 threads
   - High throughput: 4-8 threads
   - Don't use more threads than CPU cores

4. **Monitor server resources** during tests:
   ```bash
   docker stats  # if using Docker
   htop          # for CPU/memory
   ```

5. **Compare results** across configurations:
   - Save output to files: `wrk ... > results/test1.txt`
   - Track latency trends over time

## Troubleshooting

**Error: "connection refused"**
- Ensure the cluster is running: `curl http://localhost:8091/health`
- Check the port number

**Error: "authentication failed"**
- Set AUTH_TOKEN: `export AUTH_TOKEN="$(curl -sf ... | jq -r '.token')"`
- Verify token is valid

**Low throughput (<1000 req/s)**
- Check server logs for errors
- Increase connections: `-c100`
- Reduce complex query ratio

**High latency (>10ms)**
- Check server resources (CPU, memory)
- Reduce concurrency
- Review server logs for bottlenecks

## Integration with stress-wrk.sh

These scripts are used by `../stress-wrk.sh`, which orchestrates a complete stress test with multiple phases. Run the orchestrator for automated testing:

```bash
cd examples/demo
bash stress-wrk.sh
```

## Further Reading

- [wrk documentation](https://github.com/wg/wrk)
- [Lua scripting guide](https://github.com/wg/wrk/blob/master/SCRIPTING)
- [Performance investigation summary](../PERFORMANCE_INVESTIGATION_SUMMARY.md)
- [Checkpoint 027](../../checkpoint/CHECKPOINT_027.md)
