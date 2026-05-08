#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Test 1: Local Binary + Middleware
# Purpose: Measure auth, rate limiting, and backpressure overhead
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_FILE="$SCRIPT_DIR/test-middleware-config.yaml"
BINARY="${BINARY:-$SCRIPT_DIR/../../duckdb-cluster}"
PORT=8092
NUM_QUERIES="${NUM_QUERIES:-100}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}=== Test 1: Local Binary + Middleware ===${NC}"
echo ""

# Check if binary exists
if [[ ! -f "$BINARY" ]]; then
    echo -e "${RED}ERROR: Binary not found at $BINARY${NC}"
    echo "Build it first with: go build -o duckdb-cluster cmd/duckdb-cluster/main.go"
    exit 1
fi

# Start server in background
echo -e "Starting server with middleware config..."
rm -rf ./test-data-middleware
mkdir -p ./test-data-middleware
$BINARY start -config "$CONFIG_FILE" > test-middleware.log 2>&1 &
SERVER_PID=$!

cleanup() {
    echo ""
    echo -e "${BOLD}Cleaning up...${NC}"
    if [[ -n "${SERVER_PID:-}" ]]; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
    rm -rf ./test-data-middleware
}
trap cleanup EXIT

# Wait for health
echo -n "Waiting for server "
for i in {1..30}; do
    if curl -sf http://localhost:$PORT/health >/dev/null 2>&1; then
        echo -e " ${GREEN}ready${NC} (${i}s)"
        break
    fi
    sleep 1
    echo -n "."
    if [[ $i -eq 30 ]]; then
        echo -e " ${RED}TIMEOUT${NC}"
        cat test-middleware.log
        exit 1
    fi
done

# Obtain auth token
echo "Obtaining auth token..."
AUTH_TOKEN=$(curl -sf -X POST "http://localhost:$PORT/admin/auth/token" \
    -H "Content-Type: application/json" \
    -d '{"user_id":"test","username":"test-user","roles":["admin"],"tenant_id":"default"}' \
    | jq -r '.token')

if [[ -z "$AUTH_TOKEN" || "$AUTH_TOKEN" == "null" ]]; then
    echo -e "${RED}ERROR: Failed to obtain auth token${NC}"
    exit 1
fi
echo -e "Auth token: ${GREEN}obtained${NC}"

# Create test index
echo "Creating test index..."
curl -sf -X PUT "http://localhost:$PORT/indices/test-middleware" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"settings":{"shard_count":1,"partition_key_field":"id"}}' >/dev/null

# Insert test document
echo "Inserting test document..."
curl -sf -X POST "http://localhost:$PORT/indices/test-middleware/_doc" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"id":1,"value":100,"message":"test"}' >/dev/null

# Run queries with timing
echo ""
echo -e "${BOLD}Running $NUM_QUERIES queries...${NC}"
LATENCIES_FILE=$(mktemp)

for ((i=1; i<=NUM_QUERIES; i++)); do
    LATENCY=$(curl -sf -o /dev/null -w '%{time_total}' -X POST "http://localhost:$PORT/query" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"sql":"SELECT * FROM _docs LIMIT 10","index":"test-middleware"}' 2>/dev/null)
    echo "$LATENCY" >> "$LATENCIES_FILE"
    if [[ $((i % 10)) -eq 0 ]]; then
        echo -n "."
    fi
done
echo ""

# Calculate statistics
TOTAL=$(wc -l < "$LATENCIES_FILE")
P50=$(sort -n "$LATENCIES_FILE" | awk -v total="$TOTAL" 'NR==int(total*0.5){printf "%.6f",$1}')
P95=$(sort -n "$LATENCIES_FILE" | awk -v total="$TOTAL" 'NR==int(total*0.95){printf "%.6f",$1}')
P99=$(sort -n "$LATENCIES_FILE" | awk -v total="$TOTAL" 'NR==int(total*0.99){printf "%.6f",$1}')
AVG=$(awk '{s+=$1}END{printf "%.6f",s/NR}' "$LATENCIES_FILE")

# Extract timing logs from server output
echo ""
echo -e "${CYAN}${BOLD}=== Middleware Timing Breakdown ===${NC}"
grep "\[TIMING\]" test-middleware.log | tail -20

echo ""
echo -e "${CYAN}${BOLD}=== Query Latency Statistics ===${NC}"
echo -e "  Total queries: ${BOLD}$TOTAL${NC}"
echo -e "  Average:       ${BOLD}${AVG}s${NC} ($(awk "BEGIN{printf \"%.0f\", $AVG*1000000}")µs)"
echo -e "  p50:           ${BOLD}${P50}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P50*1000000}")µs)"
echo -e "  p95:           ${BOLD}${P95}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P95*1000000}")µs)"
echo -e "  p99:           ${BOLD}${P99}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P99*1000000}")µs)"

# Verdict
echo ""
AVG_US=$(awk "BEGIN{printf \"%.0f\", $AVG*1000000}")
if [[ $AVG_US -lt 1000 ]]; then
    echo -e "${GREEN}${BOLD}✓ PASS${NC} — Middleware overhead < 1ms (acceptable)"
elif [[ $AVG_US -lt 5000 ]]; then
    echo -e "${YELLOW}${BOLD}⚠ WARN${NC} — Middleware overhead 1-5ms (borderline)"
else
    echo -e "${RED}${BOLD}✗ FAIL${NC} — Middleware overhead > 5ms (needs optimization)"
fi

rm -f "$LATENCIES_FILE"
