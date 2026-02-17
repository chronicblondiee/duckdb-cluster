#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Test 3: Docker + Middleware (Full Production Config)
# Purpose: Reproduce stress test environment with baseline config
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SHARD_COUNT="${SHARD_COUNT:-1}"
DURATION="${DURATION:-10}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}=== Test 3: Docker + Middleware (Full Stack) ===${NC}"
echo ""

cd "$SCRIPT_DIR"

# Check if baseline config exists
if [[ ! -f "config-baseline.yaml" ]]; then
    echo -e "${RED}ERROR: config-baseline.yaml not found${NC}"
    exit 1
fi

# Start baseline cluster
echo "Starting baseline Docker cluster..."
docker compose -f docker-compose-baseline.yaml down -v 2>/dev/null || true
docker compose -f docker-compose-baseline.yaml up -d --build

cleanup() {
    echo ""
    echo -e "${BOLD}Cleaning up...${NC}"
    docker compose -f docker-compose-baseline.yaml down -v 2>/dev/null || true
}
trap cleanup EXIT

# Wait for health
echo -n "Waiting for cluster "
for i in {1..120}; do
    if curl -sf http://localhost:8083/health >/dev/null 2>&1; then
        echo -e " ${GREEN}ready${NC} (${i}s)"
        break
    fi
    sleep 1
    echo -n "."
    if [[ $i -eq 120 ]]; then
        echo -e " ${RED}TIMEOUT${NC}"
        docker compose -f docker-compose-baseline.yaml logs
        exit 1
    fi
done

# Run stress test with reduced duration
echo ""
echo -e "${BOLD}Running stress test (SHARD_COUNT=$SHARD_COUNT, DURATION=$DURATION)...${NC}"
echo ""

SHARD_COUNT=$SHARD_COUNT DURATION=$DURATION bash stress.sh

# Extract timing logs from all containers
echo ""
echo -e "${CYAN}${BOLD}=== Timing Logs from All Containers ===${NC}"
for service in write-node read-node all-node; do
    echo ""
    echo -e "${BOLD}--- $service ---${NC}"
    docker compose -f docker-compose-baseline.yaml logs $service 2>&1 | grep "\[TIMING\]" | tail -20 || echo "(no timing logs)"
done

echo ""
echo -e "${CYAN}${BOLD}=== Test Complete ===${NC}"
echo "Review timing logs above to identify bottlenecks."
echo "Expected: Should show 8+ second latencies if environmental issue exists."
