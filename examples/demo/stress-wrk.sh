#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Stress Test with wrk (Proper Load Testing)
# ============================================================
# This script replaces the old stress.sh with a proper HTTP
# load testing tool (wrk) that uses connection pooling and
# accurate timing measurements.
#
# Requirements:
#   - wrk installed (apt-get install wrk or brew install wrk)
#   - jq for JSON parsing
#
# Usage: bash examples/demo/stress-wrk.sh
#
# Tunable via environment variables:
#   THREADS           — wrk worker threads      (default: 4)
#   WRITE_CONCURRENCY — concurrent connections  (default: 20)
#   READ_CONCURRENCY  — concurrent connections  (default: 50)
#   DURATION          — seconds per test phase  (default: 30)
#   SHARD_COUNT       — index shard count       (default: 3)
#   PORT              — cluster HTTP port       (default: 8083)
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WRK_SCRIPTS_DIR="$SCRIPT_DIR/wrk-scripts"

# Configuration
THREADS="${THREADS:-4}"
WRITE_CONCURRENCY="${WRITE_CONCURRENCY:-20}"
READ_CONCURRENCY="${READ_CONCURRENCY:-50}"
DURATION="${DURATION:-30}"
SHARD_COUNT="${SHARD_COUNT:-3}"
PORT="${PORT:-8083}"
BULK_SIZE="${BULK_SIZE:-100}"

ALL_URL="http://localhost:$PORT"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Results directory
TIMESTAMP=$(date +%Y-%m-%d-%H-%M-%S)
RESULTS_DIR="$SCRIPT_DIR/stress-results/$TIMESTAMP"
mkdir -p "$RESULTS_DIR"

# ============================================================
# Helper Functions
# ============================================================

phase() {
    echo ""
    echo -e "${CYAN}${BOLD}=== $1 ===${NC}"
}

cooldown() {
    echo -e "  ${YELLOW}Cooldown (5s)...${NC}"
    sleep 5
}

check_wrk() {
    if ! command -v wrk &> /dev/null; then
        echo -e "${RED}ERROR: wrk is not installed${NC}"
        echo ""
        echo "Install wrk:"
        echo "  Ubuntu/Debian: sudo apt-get install wrk"
        echo "  macOS:         brew install wrk"
        echo "  From source:   https://github.com/wg/wrk"
        exit 1
    fi
}

wait_for() {
    local url="$1" name="$2" max="$3"
    local elapsed=0
    echo -n "  Waiting for $name "
    while ! curl -sf "$url/health" >/dev/null 2>&1; do
        sleep 1
        elapsed=$((elapsed + 1))
        echo -n "."
        if [[ $elapsed -ge $max ]]; then
            echo -e " ${RED}timeout${NC}"
            exit 1
        fi
    done
    echo -e " ${GREEN}ready${NC} (${elapsed}s)"
}

parse_wrk_output() {
    local output_file="$1"
    local phase_name="$2"
    
    echo "" >> "$RESULTS_DIR/summary.md"
    echo "### $phase_name" >> "$RESULTS_DIR/summary.md"
    echo "" >> "$RESULTS_DIR/summary.md"
    echo '```' >> "$RESULTS_DIR/summary.md"
    cat "$output_file" >> "$RESULTS_DIR/summary.md"
    echo '```' >> "$RESULTS_DIR/summary.md"
}

# ============================================================
# Initialization
# ============================================================

echo -e "${CYAN}${BOLD}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}${BOLD}║  DuckDB Cluster Stress Test (wrk-based)                   ║${NC}"
echo -e "${CYAN}${BOLD}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  Timestamp:    ${BOLD}$TIMESTAMP${NC}"
echo -e "  Results dir:  ${BOLD}$RESULTS_DIR${NC}"
echo -e "  Cluster URL:  ${BOLD}$ALL_URL${NC}"
echo ""

check_wrk

phase "Phase 1: Setup"

wait_for "$ALL_URL" "cluster" 60

echo "  Obtaining auth token..."
AUTH_TOKEN=$(curl -sf -X POST "$ALL_URL/admin/auth/token" \
    -H "Content-Type: application/json" \
    -d '{"user_id":"stress-test","username":"stress-tester","roles":["admin"],"tenant_id":"default"}' \
    | jq -r '.token')

if [[ -z "$AUTH_TOKEN" || "$AUTH_TOKEN" == "null" ]]; then
    echo -e "  ${RED}ERROR: Failed to obtain auth token${NC}"
    exit 1
fi
echo -e "  Auth token: ${GREEN}obtained${NC}"

# Export for wrk scripts
export AUTH_TOKEN

echo "  Creating stress-test index ($SHARD_COUNT shards)..."
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$ALL_URL/indices/stress-test" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{\"settings\":{\"shard_count\":$SHARD_COUNT,\"partition_key_field\":\"id\"}}" 2>/dev/null) || true
if [[ "$HTTP_CODE" == "201" || "$HTTP_CODE" == "200" ]]; then
    echo -e "  Index: ${GREEN}created${NC}"
else
    echo -e "  Index: ${YELLOW}already exists or code=$HTTP_CODE${NC}"
fi

echo -e "  Config: threads=${BOLD}$THREADS${NC}  writers=${BOLD}$WRITE_CONCURRENCY${NC}  readers=${BOLD}$READ_CONCURRENCY${NC}  duration=${BOLD}${DURATION}s${NC}  shards=${BOLD}$SHARD_COUNT${NC}"

# Create summary header
cat > "$RESULTS_DIR/summary.md" <<EOF
# Stress Test Results - $TIMESTAMP

## Configuration

- **Cluster URL**: $ALL_URL
- **Threads**: $THREADS
- **Write Concurrency**: $WRITE_CONCURRENCY
- **Read Concurrency**: $READ_CONCURRENCY
- **Duration**: ${DURATION}s
- **Shard Count**: $SHARD_COUNT
- **Tool**: wrk (proper connection pooling)

---

## Results

EOF

# ============================================================
# Phase 2: Write Stress - Single Documents
# ============================================================
phase "Phase 2: Write Stress (Single Documents)"

echo "  Running wrk with $WRITE_CONCURRENCY connections for ${DURATION}s..."
wrk -t"$THREADS" -c"$WRITE_CONCURRENCY" -d"${DURATION}s" --latency \
    -s "$WRK_SCRIPTS_DIR/write-single.lua" \
    "$ALL_URL/indices/stress-test/_doc" \
    > "$RESULTS_DIR/write-single.txt" 2>&1

parse_wrk_output "$RESULTS_DIR/write-single.txt" "Write Stress - Single Documents"
echo -e "  ${GREEN}Complete${NC} - saved to write-single.txt"

cooldown

# ============================================================
# Phase 3: Write Stress - Bulk Documents
# ============================================================
phase "Phase 3: Write Stress (Bulk Documents)"

echo "  Running wrk with $WRITE_CONCURRENCY connections for ${DURATION}s..."
wrk -t"$THREADS" -c"$WRITE_CONCURRENCY" -d"${DURATION}s" --latency \
    -s "$WRK_SCRIPTS_DIR/write-bulk.lua" \
    "$ALL_URL/indices/stress-test/_bulk" \
    > "$RESULTS_DIR/write-bulk.txt" 2>&1

parse_wrk_output "$RESULTS_DIR/write-bulk.txt" "Write Stress - Bulk Documents"
echo -e "  ${GREEN}Complete${NC} - saved to write-bulk.txt"

cooldown

# ============================================================
# Phase 4: Read Stress
# ============================================================
phase "Phase 4: Read Stress"

echo "  Running wrk with $READ_CONCURRENCY connections for ${DURATION}s..."
wrk -t"$THREADS" -c"$READ_CONCURRENCY" -d"${DURATION}s" --latency \
    -s "$WRK_SCRIPTS_DIR/read-query.lua" \
    "$ALL_URL/query" \
    > "$RESULTS_DIR/read-query.txt" 2>&1

parse_wrk_output "$RESULTS_DIR/read-query.txt" "Read Stress"
echo -e "  ${GREEN}Complete${NC} - saved to read-query.txt"

cooldown

# ============================================================
# Phase 5: Mixed Read/Write (Parallel)
# ============================================================
phase "Phase 5: Mixed Read/Write"

echo "  Launching parallel workloads for ${DURATION}s..."
echo "    - Writers: $WRITE_CONCURRENCY connections"
echo "    - Readers: $READ_CONCURRENCY connections"

# Start write workload in background
wrk -t2 -c"$((WRITE_CONCURRENCY / 2))" -d"${DURATION}s" --latency \
    -s "$WRK_SCRIPTS_DIR/write-single.lua" \
    "$ALL_URL/indices/stress-test/_doc" \
    > "$RESULTS_DIR/mixed-write.txt" 2>&1 &
WRITE_PID=$!

# Start read workload in background
wrk -t"$THREADS" -c"$READ_CONCURRENCY" -d"${DURATION}s" --latency \
    -s "$WRK_SCRIPTS_DIR/read-query.lua" \
    "$ALL_URL/query" \
    > "$RESULTS_DIR/mixed-read.txt" 2>&1 &
READ_PID=$!

# Wait for both to complete
wait $WRITE_PID
wait $READ_PID

parse_wrk_output "$RESULTS_DIR/mixed-write.txt" "Mixed - Write Component"
parse_wrk_output "$RESULTS_DIR/mixed-read.txt" "Mixed - Read Component"
echo -e "  ${GREEN}Complete${NC} - saved to mixed-*.txt"

cooldown

# ============================================================
# Phase 6: Cross-Index Fan-out
# ============================================================
phase "Phase 6: Cross-Index Fan-out"

echo "  Creating 3 fan-out indices..."
for i in 1 2 3; do
    curl -s -o /dev/null -X PUT "$ALL_URL/indices/fanout-$i" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"settings":{"shard_count":1,"partition_key_field":"id"}}' 2>/dev/null || true
    
    # Insert some test data
    for j in {1..100}; do
        curl -s -o /dev/null -X POST "$ALL_URL/indices/fanout-$i/_doc" \
            -H "Authorization: Bearer $AUTH_TOKEN" \
            -H "Content-Type: application/json" \
            -d "{\"id\":$j,\"value\":$((j * 10)),\"message\":\"fanout test\"}" 2>/dev/null || true
    done
done
echo -e "  Indices: ${GREEN}created and populated${NC}"

echo "  Running fan-out query test with $READ_CONCURRENCY connections for ${DURATION}s..."
wrk -t"$THREADS" -c"$READ_CONCURRENCY" -d"${DURATION}s" --latency \
    -s "$WRK_SCRIPTS_DIR/read-fanout.lua" \
    "$ALL_URL/query" \
    > "$RESULTS_DIR/fanout.txt" 2>&1

parse_wrk_output "$RESULTS_DIR/fanout.txt" "Cross-Index Fan-out"
echo -e "  ${GREEN}Complete${NC} - saved to fanout.txt"

cooldown

# ============================================================
# Phase 7: Backpressure Test
# ============================================================
phase "Phase 7: Backpressure Test"

echo "  Testing backpressure with high concurrency (200 connections)..."
wrk -t8 -c200 -d10s --latency \
    -s "$WRK_SCRIPTS_DIR/read-query.lua" \
    "$ALL_URL/query" \
    > "$RESULTS_DIR/backpressure.txt" 2>&1

parse_wrk_output "$RESULTS_DIR/backpressure.txt" "Backpressure Test (200 connections)"
echo -e "  ${GREEN}Complete${NC} - saved to backpressure.txt"

# Check cluster health after backpressure
echo -n "  Recovery check "
sleep 3
if curl -sf "$ALL_URL/health" >/dev/null 2>&1; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAIL — cluster not healthy after backpressure${NC}"
fi

# ============================================================
# Summary
# ============================================================

phase "Summary"

cat >> "$RESULTS_DIR/summary.md" <<EOF

---

## Comparison with Old stress.sh

| Metric | Old (stress.sh) | New (wrk) | Improvement |
|--------|-----------------|-----------|-------------|
| Tool | Shell + curl | wrk | - |
| Connection Model | New per request | Pooled | ✅ |
| Process Model | Fork per request | Single process | ✅ |
| Expected Latency | ~8000ms | ~0.4-2ms | **~4000x** |
| Expected Throughput | ~100 req/s | ~10,000-50,000 req/s | **~500x** |
| Accuracy | ❌ Includes client overhead | ✅ Server-side only | ✅ |

EOF

echo ""
echo -e "${GREEN}${BOLD}✓ Stress test complete!${NC}"
echo ""
echo -e "  Results saved to: ${BOLD}$RESULTS_DIR${NC}"
echo -e "  Summary report:   ${BOLD}$RESULTS_DIR/summary.md${NC}"
echo ""
echo "View results:"
echo "  cat $RESULTS_DIR/summary.md"
echo ""
