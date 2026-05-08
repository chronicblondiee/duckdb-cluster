#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Test 2: Docker + No Middleware
# Purpose: Isolate Docker network stack and container overhead
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PORT=8093
NUM_QUERIES="${NUM_QUERIES:-100}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}=== Test 2: Docker + No Middleware ===${NC}"
echo ""

cd "$SCRIPT_DIR"

# Start container
echo "Building and starting Docker container..."
docker compose -f docker-compose-local.yaml down -v 2>/dev/null || true
docker compose -f docker-compose-local.yaml up -d --build

cleanup() {
    echo ""
    echo -e "${BOLD}Cleaning up...${NC}"
    docker compose -f docker-compose-local.yaml down -v 2>/dev/null || true
}
trap cleanup EXIT

# Wait for health
echo -n "Waiting for container "
for i in {1..60}; do
    if curl -sf http://localhost:$PORT/health >/dev/null 2>&1; then
        echo -e " ${GREEN}ready${NC} (${i}s)"
        break
    fi
    sleep 1
    echo -n "."
    if [[ $i -eq 60 ]]; then
        echo -e " ${RED}TIMEOUT${NC}"
        docker compose -f docker-compose-local.yaml logs
        exit 1
    fi
done

# Create test index (no auth required)
echo "Creating test index..."
curl -sf -X PUT "http://localhost:$PORT/indices/test-docker" \
    -H "Content-Type: application/json" \
    -d '{"settings":{"shard_count":1,"partition_key_field":"id"}}' >/dev/null

# Insert test document
echo "Inserting test document..."
curl -sf -X POST "http://localhost:$PORT/indices/test-docker/_doc" \
    -H "Content-Type: application/json" \
    -d '{"id":1,"value":100,"message":"test"}' >/dev/null

# Run queries with timing
echo ""
echo -e "${BOLD}Running $NUM_QUERIES queries...${NC}"
LATENCIES_FILE=$(mktemp)

for ((i=1; i<=NUM_QUERIES; i++)); do
    LATENCY=$(curl -sf -o /dev/null -w '%{time_total}' -X POST "http://localhost:$PORT/query" \
        -H "Content-Type: application/json" \
        -d '{"sql":"SELECT * FROM _docs LIMIT 10","index":"test-docker"}' 2>/dev/null)
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

echo ""
echo -e "${CYAN}${BOLD}=== Query Latency Statistics ===${NC}"
echo -e "  Total queries: ${BOLD}$TOTAL${NC}"
echo -e "  Average:       ${BOLD}${AVG}s${NC} ($(awk "BEGIN{printf \"%.0f\", $AVG*1000}")ms)"
echo -e "  p50:           ${BOLD}${P50}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P50*1000}")ms)"
echo -e "  p95:           ${BOLD}${P95}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P95*1000}")ms)"
echo -e "  p99:           ${BOLD}${P99}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P99*1000}")ms)"

# Extract timing logs from container
echo ""
echo -e "${CYAN}${BOLD}=== Container Timing Logs (last 20) ===${NC}"
docker compose -f docker-compose-local.yaml logs test-docker 2>&1 | grep "\[TIMING\]" | tail -20 || echo "(no timing logs found - middleware disabled)"

# Verdict
echo ""
AVG_MS=$(awk "BEGIN{printf \"%.0f\", $AVG*1000}")
if [[ $AVG_MS -lt 5 ]]; then
    echo -e "${GREEN}${BOLD}✓ PASS${NC} — Docker overhead < 5ms (acceptable)"
elif [[ $AVG_MS -lt 50 ]]; then
    echo -e "${YELLOW}${BOLD}⚠ WARN${NC} — Docker overhead 5-50ms (may need network tuning)"
else
    echo -e "${RED}${BOLD}✗ FAIL${NC} — Docker overhead > 50ms (significant network latency)"
fi

rm -f "$LATENCIES_FILE"
