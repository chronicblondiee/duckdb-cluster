# Environmental Performance Test Results

**Date:** 2026-02-17  
**Test Duration:** ~15 minutes (partial)

---

## ✅ Test 1: Local Binary + Middleware

**Status:** PASSED  
**Purpose:** Measure auth, rate limiting, and backpressure overhead

### Results

| Metric | Value | Notes |
|--------|-------|-------|
| **Average Latency** | **388µs** | Within acceptable range |
| **p50** | 365µs | Fast |
| **p95** | 531µs | Fast |
| **p99** | 763µs | Fast |
| **Verdict** | ✅ **PASS** | Middleware overhead < 1ms |

### Middleware Timing Breakdown

From instrumentation logs:

```
[TIMING] auth check=19.507µs total=274.529µs path=/query
[TIMING] backpressure acquire=2.715µs total=279.208µs path=/query
[TIMING] ratelimit check=491ns total=281.091µs path=/query
```

| Middleware Layer | Overhead | Analysis |
|------------------|----------|----------|
| **Auth Check** | ~20µs | Minimal - JWT verification |
| **Backpressure** | ~3µs | Negligible - semaphore acquire |
| **Rate Limit** | <1µs | Negligible - token bucket check |
| **Total Middleware** | ~23µs | Only 5.9% of total latency |

### Conclusion

**Middleware is NOT the bottleneck.** Total middleware overhead is only ~23µs out of 388µs total (5.9%).

---

## 🔍 Test 2: Docker + No Middleware (Partial Results)

**Status:** IN PROGRESS  
**Purpose:** Isolate Docker network stack and container overhead

### Manual Test Results

Single query test through Docker container:

| Measurement | Value | Comparison |
|-------------|-------|------------|
| **End-to-end latency** | 703µs | ~2x local (388µs) |
| **Container internal timing** | ~200-250µs | Same as local |
| **Docker network overhead** | **~450-500µs** | Bridge network latency |

### Container Internal Timing (from logs)

```
[TIMING] shard=0 QueryWithSchema total=207.953µs query=190.59µs
[TIMING] handleQuery total=222.951µs decode=5.24µs route=212.391µs
[TIMING] auth check=291ns total=230.777µs path=/query
[TIMING] backpressure acquire=90ns total=231.748µs path=/query
[TIMING] ratelimit check=141ns total=232.63µs path=/query
```

**Internal processing:** ~230µs (similar to Test 1)  
**Network overhead:** ~470µs (703µs total - 230µs internal)

### Analysis

Docker bridge network adds approximately **450-500µs** of overhead per request. This is significant but not the 8-second issue.

---

## 🔎 Key Findings So Far

### 1. Middleware is Fast ✅
- Auth: 20µs
- Backpressure: 3µs  
- Rate limit: <1µs
- **Total: 23µs overhead**

### 2. Docker Network Overhead 🟡
- Bridge network: ~450-500µs
- Acceptable for container deployment
- Can be reduced with host networking if needed

### 3. What's NOT the Problem ✅
- ❌ Authentication middleware
- ❌ Rate limiting
- ❌ Backpressure semaphores
- ❌ Basic Docker containerization

---

## 🎯 Next Steps

Based on results so far, the 8-second latency issue is likely caused by:

### Most Likely Culprits:

1. **HTTP Connection Overhead** (Test 4)
   - curl creates new TCP connection per request
   - No connection reuse/pooling
   - **Expected impact:** 100-500ms per request

2. **Load/Concurrency Issues** (Test 5)
   - Backpressure queue buildup under load
   - Rate limiting engaging too aggressively
   - Connection pool exhaustion

3. **Multi-Container Coordination** (Test 3)
   - Cross-container network hops
   - Load balancer overhead
   - Container orchestration latency

### Recommended Test Order:

1. ✅ **Test 1 Complete** - Middleware not the issue
2. 🟡 **Test 2 Partial** - Docker adds ~500µs (acceptable)
3. **Test 4 Next** - HTTP connection pooling (CRITICAL)
4. **Test 3** - Full production stack
5. **Test 5** - Concurrency stress test

---

## 📊 Performance Baseline

| Configuration | Latency | Verdict |
|---------------|---------|---------|
| **Instrumented tests (local)** | 200-850µs | ✅ Fast |
| **Local + Middleware** | 365-531µs | ✅ Fast |
| **Docker container** | ~700µs | ✅ Acceptable |
| **Stress test (reported)** | 8+ seconds | ❌ **ISSUE** |

**Gap:** 700µs → 8 seconds = **11,000x slower**

This massive gap suggests:
- **NOT** middleware overhead (only 23µs)
- **NOT** basic Docker overhead (only 500µs)
- **LIKELY** HTTP client configuration or concurrency issues

---

## 🔧 Quick Fix Recommendations

### 1. Use Proper HTTP Client (High Priority)
Instead of curl in loops:

```bash
# Bad (creates new connection each time)
for i in {1..100}; do
    curl http://localhost:8080/query
done

# Good (use wrk with connection pooling)
wrk -t4 -c20 -d10s --latency http://localhost:8080/query
```

### 2. Connection Keep-Alive Headers
```bash
curl -H "Connection: keep-alive" -H "Keep-Alive: timeout=30, max=100"
```

### 3. Check Concurrency Limits
Current config:
- Max concurrent reads: 1000
- Max concurrent writes: 100
- Rate limit: 200 req/s

If stress test exceeds these, expect 503/429 responses.

---

## 📈 Instrumentation Working ✅

All middleware now logs timing:

```
[TIMING] backpressure acquire=X total=Y path=/query
[TIMING] auth check=X total=Y path=/query
[TIMING] ratelimit check=X total=Y path=/query
```

Logs captured in:
- Local: `test-middleware.log`
- Docker: `docker compose logs <service>`

---

## Next Action

**Run Test 4 (HTTP Connection Pooling) to confirm the root cause.**

```bash
cd examples/demo
bash test-http-pooling.sh
```

This will compare:
- curl without keep-alive
- curl with keep-alive  
- wrk with proper connection pooling

Expected to find **100-1000ms difference** if this is the bottleneck.
