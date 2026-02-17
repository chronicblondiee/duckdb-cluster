#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Test 4: HTTP Connection Configuration
# Purpose: Test if curl connection reuse is the bottleneck
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PORT="${PORT:-8083}"
NUM_QUERIES="${NUM_QUERIES:-100}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}=== Test 4: HTTP Connection Pooling ===${NC}"
echo ""

cd "$SCRIPT_DIR"

# Check if cluster is running
if ! curl -sf http://localhost:$PORT/health >/dev/null 2>&1; then
    echo -e "${YELLOW}Cluster not running. Starting baseline cluster...${NC}"
    docker compose -f docker-compose-baseline.yaml up -d
    echo -n "Waiting for cluster "
    for i in {1..60}; do
        if curl -sf http://localhost:$PORT/health >/dev/null 2>&1; then
            echo -e " ${GREEN}ready${NC}"
            break
        fi
        sleep 1
        echo -n "."
    done
fi

# Get auth token
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

# Ensure test index exists
curl -sf -X PUT "http://localhost:$PORT/indices/http-pooling-test" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"settings":{"shard_count":1,"partition_key_field":"id"}}' >/dev/null 2>&1 || true

curl -sf -X POST "http://localhost:$PORT/indices/http-pooling-test/_doc" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"id":1,"value":100,"message":"test"}' >/dev/null 2>&1 || true

# Test 1: curl without keep-alive (default)
echo ""
echo -e "${BOLD}Test 1: curl without keep-alive (default)${NC}"
LATENCIES_FILE=$(mktemp)

for ((i=1; i<=NUM_QUERIES; i++)); do
    LATENCY=$(curl -sf -o /dev/null -w '%{time_total}' -X POST "http://localhost:$PORT/query" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"sql":"SELECT * FROM _docs LIMIT 10","index":"http-pooling-test"}' 2>/dev/null)
    echo "$LATENCY" >> "$LATENCIES_FILE"
done

AVG_NO_KA=$(awk '{s+=$1}END{printf "%.6f",s/NR}' "$LATENCIES_FILE")
P50_NO_KA=$(sort -n "$LATENCIES_FILE" | awk -v total="$NUM_QUERIES" 'NR==int(total*0.5){printf "%.6f",$1}')
echo -e "  Average: ${BOLD}${AVG_NO_KA}s${NC} ($(awk "BEGIN{printf \"%.0f\", $AVG_NO_KA*1000}")ms)"
echo -e "  p50:     ${BOLD}${P50_NO_KA}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P50_NO_KA*1000}")ms)"
rm -f "$LATENCIES_FILE"

# Test 2: curl with keep-alive
echo ""
echo -e "${BOLD}Test 2: curl with keep-alive headers${NC}"
LATENCIES_FILE=$(mktemp)

for ((i=1; i<=NUM_QUERIES; i++)); do
    LATENCY=$(curl -sf -o /dev/null -w '%{time_total}' -X POST "http://localhost:$PORT/query" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -H "Connection: keep-alive" \
        -H "Keep-Alive: timeout=30, max=100" \
        -d '{"sql":"SELECT * FROM _docs LIMIT 10","index":"http-pooling-test"}' 2>/dev/null)
    echo "$LATENCY" >> "$LATENCIES_FILE"
done

AVG_WITH_KA=$(awk '{s+=$1}END{printf "%.6f",s/NR}' "$LATENCIES_FILE")
P50_WITH_KA=$(sort -n "$LATENCIES_FILE" | awk -v total="$NUM_QUERIES" 'NR==int(total*0.5){printf "%.6f",$1}')
echo -e "  Average: ${BOLD}${AVG_WITH_KA}s${NC} ($(awk "BEGIN{printf \"%.0f\", $AVG_WITH_KA*1000}")ms)"
echo -e "  p50:     ${BOLD}${P50_WITH_KA}s${NC} ($(awk "BEGIN{printf \"%.0f\", $P50_WITH_KA*1000}")ms)"
rm -f "$LATENCIES_FILE"

# Test 3: wrk (if available)
echo ""
if command -v wrk &> /dev/null; then
    echo -e "${BOLD}Test 3: wrk with connection pooling${NC}"
    
    # Create wrk script
    cat > /tmp/query.lua <<'EOF'
wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"
wrk.headers["Authorization"] = "Bearer " .. os.getenv("AUTH_TOKEN")
wrk.body = '{"sql":"SELECT * FROM _docs LIMIT 10","index":"http-pooling-test"}'
EOF
    
    export AUTH_TOKEN
    WRK_OUTPUT=$(wrk -t4 -c20 -d10s --latency -s /tmp/query.lua http://localhost:$PORT/query 2>&1)
    echo "$WRK_OUTPUT"
    rm -f /tmp/query.lua
else
    echo -e "${YELLOW}wrk not installed, skipping Test 3${NC}"
    echo "Install with: brew install wrk (macOS) or apt-get install wrk (Linux)"
fi

# Analysis
echo ""
echo -e "${CYAN}${BOLD}=== Analysis ===${NC}"

IMPROVEMENT=$(awk "BEGIN{printf \"%.1f\", ($AVG_NO_KA - $AVG_WITH_KA) / $AVG_NO_KA * 100}")
echo -e "  Keep-alive improvement: ${BOLD}${IMPROVEMENT}%${NC}"

if (( $(echo "$IMPROVEMENT > 50" | bc -l) )); then
    echo -e "  Verdict: ${RED}${BOLD}✗ CRITICAL${NC} — HTTP connection overhead is the bottleneck!"
    echo "           Use proper HTTP client with connection pooling in production."
elif (( $(echo "$IMPROVEMENT > 20" | bc -l) )); then
    echo -e "  Verdict: ${YELLOW}${BOLD}⚠ WARN${NC} — HTTP connection overhead is significant (20-50%)"
    echo "           Consider using connection pooling for better performance."
else
    echo -e "  Verdict: ${GREEN}${BOLD}✓ PASS${NC} — HTTP connection overhead is minimal (<20%)"
fi

# Cleanup
curl -sf -X DELETE "http://localhost:$PORT/indices/http-pooling-test" \
    -H "Authorization: Bearer $AUTH_TOKEN" >/dev/null 2>&1 || true
