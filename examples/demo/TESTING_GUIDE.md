# Environmental Performance Testing Guide

This directory contains 9 test scripts to systematically identify performance bottlenecks by testing environmental factors incrementally.

## 📋 Test Execution Order

Run tests in this order for optimal insight:

### 1️⃣ Test 1: Local + Middleware (5 minutes)
**Purpose:** Measure auth, rate limiting, and backpressure overhead

```bash
# Build binary first
go build -o duckdb-cluster cmd/duckdb-cluster/main.go

# Run test
bash examples/demo/test-middleware.sh
```

**Expected:** 300µs → 500-1000µs (2-3x slowdown is acceptable)

---

### 2️⃣ Test 4: HTTP Connection Pooling (10 minutes)
**Purpose:** Test if curl is the bottleneck (connection reuse)

```bash
bash examples/demo/test-http-pooling.sh
```

**Expected:** Significant latency difference between curl with/without keep-alive

**Optional:** Install `wrk` for better benchmarking:
```bash
# macOS
brew install wrk

# Ubuntu/Debian
apt-get install wrk
```

---

### 3️⃣ Test 2: Docker + No Middleware (15 minutes)
**Purpose:** Isolate Docker network stack and container overhead

```bash
bash examples/demo/test-docker.sh
```

**Expected:** ~1ms → 2-10ms (Docker bridge network overhead)

---

### 4️⃣ Test 3: Full Docker Stack (20 minutes)
**Purpose:** Reproduce stress test environment with all middleware

```bash
bash examples/demo/test-docker-full.sh
```

**Expected:** Should reproduce 8-second issue if environmental

---

### 5️⃣ Test 5: Incremental Concurrency (30 minutes)
**Purpose:** Find at what concurrency level performance degrades

```bash
bash examples/demo/test-concurrency.sh
```

**Expected:** Identify concurrency threshold where backpressure/rate-limiting engages

---

## 📊 What Each Test Reveals

| Test | Measures | Pass Criteria | Fail Indicates |
|------|----------|---------------|----------------|
| Test 1 | Middleware overhead | < 1ms | Auth/rate-limit/backpressure bottleneck |
| Test 4 | HTTP connection | < 20% improvement with keep-alive | curl creating new TCP conn per request |
| Test 2 | Docker network | < 5ms overhead | Docker bridge network latency |
| Test 3 | Full stack | Reproduces 8s latency | Environmental issue confirmed |
| Test 5 | Concurrency limits | Linear scaling | Backpressure/rate-limit threshold |

---

## 🔍 Instrumentation Added

All middleware now logs timing to stderr:

```
[TIMING] backpressure acquire=50µs total=120µs path=/query
[TIMING] auth check=30µs total=80µs path=/query
[TIMING] ratelimit check=10µs total=40µs path=/query
```

Logs are captured in:
- **Local tests:** `test-middleware.log`
- **Docker tests:** `docker compose logs <service>`

---

## 📁 Files Created

### Configuration Files
- `test-middleware-config.yaml` - Local server with middleware enabled
- `test-config-docker.yaml` - Docker config without middleware
- `docker-compose-local.yaml` - Single-container Docker setup

### Test Scripts
- `test-middleware.sh` - Test 1: Local + Middleware
- `test-docker.sh` - Test 2: Docker + No Middleware
- `test-docker-full.sh` - Test 3: Full Docker Stack
- `test-http-pooling.sh` - Test 4: HTTP Connection Config
- `test-concurrency.sh` - Test 5: Incremental Concurrency

### Utilities
- `query.lua` - wrk script for proper load testing

---

## 🎯 Quick Start

Run all tests sequentially:

```bash
cd examples/demo

# Build binary
go build -o ../../duckdb-cluster ../../cmd/duckdb-cluster/main.go

# Run tests in order
bash test-middleware.sh          # Test 1
bash test-http-pooling.sh        # Test 4 (requires cluster running)
bash test-docker.sh              # Test 2
bash test-docker-full.sh         # Test 3
bash test-concurrency.sh         # Test 5
```

---

## 🐛 Troubleshooting

### Binary not found
```bash
# Build from repo root
go build -o duckdb-cluster cmd/duckdb-cluster/main.go
```

### Docker cluster not starting
```bash
# Check logs
docker compose -f docker-compose-baseline.yaml logs

# Restart fresh
docker compose -f docker-compose-baseline.yaml down -v
docker compose -f docker-compose-baseline.yaml up -d --build
```

### Port conflicts
```bash
# Check what's using ports
lsof -i :8080
lsof -i :8092
lsof -i :8093

# Kill processes
kill -9 <PID>
```

---

## 📈 Expected Results Matrix

After all tests, you should have a clear picture:

| Scenario | Expected Latency | If Higher → Root Cause |
|----------|------------------|----------------------|
| Local no middleware | 200-300µs | Baseline |
| Local + middleware | 500-1000µs | Middleware overhead |
| Docker no middleware | 2-10ms | Docker network |
| Docker + middleware | Varies | Combined factors |
| With connection pooling | Same as above | HTTP client |

---

## 🎓 Next Steps

Based on results:

1. **If Test 1 fails (>5ms):** Optimize middleware (auth/rate-limit/backpressure)
2. **If Test 4 shows >50% improvement:** Use proper HTTP client with connection pooling
3. **If Test 2 fails (>50ms):** Docker network config needs tuning (host network mode?)
4. **If Test 5 shows early degradation:** Adjust concurrency limits in config
5. **If Test 3 reproduces issue:** Environmental confirmed, fix based on above

---

## 📚 References

- Original Plan: `environmental_performance_testing_plan_0076869c.plan.md`
- Instrumentation: `internal/api/server.go`, `internal/security/middleware.go`
- Stress Test: `stress.sh`
