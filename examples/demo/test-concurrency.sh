#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Test 5: Incremental Concurrency Testing
# Purpose: Find at what concurrency level performance degrades
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DURATION="${DURATION:-10}"
CONCURRENCY_LEVELS="${CONCURRENCY_LEVELS:-1 2 5 10 20 50}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "${CYAN}${BOLD}=== Test 5: Incremental Concurrency Testing ===${NC}"
echo ""

cd "$SCRIPT_DIR"

# Check if cluster is running
if ! curl -sf http://localhost:8083/health >/dev/null 2>&1; then
    echo -e "${YELLOW}Cluster not running. Starting baseline cluster...${NC}"
    docker compose -f docker-compose-baseline.yaml up -d
    echo -n "Waiting for cluster "
    for i in {1..60}; do
        if curl -sf http://localhost:8083/health >/dev/null 2>&1; then
            echo -e " ${GREEN}ready${NC}"
            break
        fi
        sleep 1
        echo -n "."
    done
fi

# Run stress test with varying concurrency
RESULTS_FILE=$(mktemp)
echo "concurrency,phase,total_requests,ok,rate_limited,failed,rps,p50_ms,p95_ms,p99_ms" > "$RESULTS_FILE"

for concurrency in $CONCURRENCY_LEVELS; do
    echo ""
    echo -e "${BOLD}Testing with READ_CONCURRENCY=$concurrency...${NC}"
    echo ""
    
    # Run stress test with specific concurrency
    READ_CONCURRENCY=$concurrency WRITE_CONCURRENCY=5 DURATION=$DURATION bash stress.sh 2>&1 | tee "/tmp/stress_c${concurrency}.log" || true
    
    # Extract results from log
    # Parse the summary table from stress.sh output
    tail -20 "/tmp/stress_c${concurrency}.log" | grep -E "Read Stress|Mixed Read" | while read -r line; do
        # Extract metrics using awk
        phase=$(echo "$line" | awk '{print $1" "$2}' | tr -d ' ')
        total=$(echo "$line" | awk '{print $3}')
        ok=$(echo "$line" | awk '{print $4}')
        limited=$(echo "$line" | awk '{print $5}')
        failed=$(echo "$line" | awk '{print $6}')
        rps=$(echo "$line" | awk '{print $7}')
        p50=$(echo "$line" | awk '{print $8}' | sed 's/s$//')
        p95=$(echo "$line" | awk '{print $9}' | sed 's/s$//')
        p99=$(echo "$line" | awk '{print $10}' | sed 's/s$//')
        
        # Convert to milliseconds
        p50_ms=$(awk "BEGIN{printf \"%.3f\", $p50 * 1000}")
        p95_ms=$(awk "BEGIN{printf \"%.3f\", $p95 * 1000}")
        p99_ms=$(awk "BEGIN{printf \"%.3f\", $p99 * 1000}")
        
        echo "$concurrency,$phase,$total,$ok,$limited,$failed,$rps,$p50_ms,$p95_ms,$p99_ms" >> "$RESULTS_FILE"
    done
    
    # Cooldown between tests
    sleep 5
done

# Generate report
echo ""
echo -e "${CYAN}${BOLD}=== Concurrency Test Results ===${NC}"
echo ""

# Print header
printf "${BOLD}%-12s %-16s %-8s %-8s %-8s %-8s %-10s %-10s %-10s %-10s${NC}\n" \
    "Concurrency" "Phase" "Total" "OK" "Limited" "Failed" "Req/s" "p50(ms)" "p95(ms)" "p99(ms)"
printf "%-12s %-16s %-8s %-8s %-8s %-8s %-10s %-10s %-10s %-10s\n" \
    "------------" "----------------" "--------" "--------" "--------" "--------" "----------" "----------" "----------" "----------"

# Print results
tail -n +2 "$RESULTS_FILE" | while IFS=, read -r conc phase total ok limited failed rps p50 p95 p99; do
    # Color coding based on performance
    if (( $(echo "$p50 < 100" | bc -l) )); then
        color=$GREEN
    elif (( $(echo "$p50 < 500" | bc -l) )); then
        color=$YELLOW
    else
        color=$RED
    fi
    
    printf "${color}%-12s${NC} %-16s %-8s %-8s %-8s %-8s %-10s %-10s %-10s %-10s\n" \
        "$conc" "$phase" "$total" "$ok" "$limited" "$failed" "$rps" "$p50" "$p95" "$p99"
done

echo ""
echo -e "${CYAN}${BOLD}=== Analysis ===${NC}"
echo ""

# Find degradation threshold
echo "Performance degradation analysis:"
tail -n +2 "$RESULTS_FILE" | awk -F, '
    BEGIN {
        prev_p50 = 0
        threshold_found = 0
    }
    {
        if ($2 == "ReadStress") {
            conc = $1
            p50 = $8
            
            if (prev_p50 > 0) {
                increase = (p50 - prev_p50) / prev_p50 * 100
                if (increase > 100 && !threshold_found) {
                    print "  ⚠ Significant degradation at concurrency " conc " (p50 latency +", int(increase) "% vs previous)"
                    threshold_found = 1
                }
            }
            prev_p50 = p50
        }
    }
    END {
        if (!threshold_found) {
            print "  ✓ No significant degradation detected within tested concurrency levels"
        }
    }'

# Identify when rate limiting/backpressure kicked in
echo ""
echo "Rate limiting / backpressure engagement:"
tail -n +2 "$RESULTS_FILE" | awk -F, '
    {
        if ($5 > 0 || $6 > 0) {
            printf "  Concurrency %s: %s rate-limited, %s failed\n", $1, $5, $6
        }
    }'

echo ""
echo -e "${GREEN}Full results saved to: $RESULTS_FILE${NC}"
echo "You can analyze further with: column -t -s, $RESULTS_FILE"
