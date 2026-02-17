#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# DuckDB Cluster — Benchmark Comparison
#
# Runs the stress test against 4 configurations:
#   1. Standalone DuckDB (no cluster layer)
#   2. duckdb-cluster monolithic, 1 shard
#   3. duckdb-cluster monolithic, 2 shards
#   4. duckdb-cluster monolithic, 3 shards
#
# Prerequisites: docker, curl, jq, awk
# Usage: bash examples/demo/benchmark.sh
#
# Tunable via environment variables:
#   DURATION            — seconds per test phase      (default: 30)
#   WRITE_CONCURRENCY   — parallel writer processes   (default: 10)
#   READ_CONCURRENCY    — parallel reader processes   (default: 20)
#   BULK_SIZE           — documents per bulk request   (default: 100)
#   SKIP_STANDALONE     — set to 1 to skip standalone (default: 0)
#   SKIP_CLUSTER        — set to 1 to skip cluster    (default: 0)
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

DURATION="${DURATION:-30}"
WRITE_CONCURRENCY="${WRITE_CONCURRENCY:-10}"
READ_CONCURRENCY="${READ_CONCURRENCY:-20}"
BULK_SIZE="${BULK_SIZE:-100}"
SKIP_STANDALONE="${SKIP_STANDALONE:-0}"
SKIP_CLUSTER="${SKIP_CLUSTER:-0}"

COMPOSE_FILE="$SCRIPT_DIR/docker-compose-baseline.yaml"
STRESS_SCRIPT="$SCRIPT_DIR/stress.sh"

MAX_WAIT=120

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Collected results: RESULTS[config_name:phase_name]="total|ok|limited|fail|rps|p50|p95|p99"
declare -A RESULTS
CONFIG_NAMES=()

# --- Helpers ---

wait_for_health() {
    local url="$1" name="$2" max="$3"
    local elapsed=0
    echo -n "  Waiting for $name "
    while ! curl -sf "$url/health" >/dev/null 2>&1; do
        sleep 1
        elapsed=$((elapsed + 1))
        echo -n "."
        if [[ $elapsed -ge $max ]]; then
            echo -e " ${RED}TIMEOUT${NC}"
            echo "ERROR: $name did not become healthy within ${max}s"
            return 1
        fi
    done
    echo -e " ${GREEN}ready${NC} (${elapsed}s)"
}

# Parse the summary table from stress.sh output.
# Looks for lines matching: "  Phase Name          total   ok  limited  fail   rps   p50   p95   p99"
parse_stress_output() {
    local config_name="$1" output_file="$2"

    # Extract summary table lines (after the header separator)
    local in_table=0
    while IFS= read -r line; do
        # Strip ANSI codes
        local clean
        clean=$(echo "$line" | sed 's/\x1b\[[0-9;]*m//g')

        # Detect the separator line
        if [[ "$clean" =~ ^[[:space:]]*-{20} ]]; then
            in_table=1
            continue
        fi

        # Detect "Phases passed" which ends the table
        if [[ "$clean" =~ "Phases passed" ]]; then
            in_table=0
            continue
        fi

        if [[ $in_table -eq 1 && -n "$clean" ]]; then
            # Parse: "  Phase Name         total   ok  limited  fail   rps   p50   p95   p99"
            local phase total ok limited fail rps p50 p95 p99
            # The phase name can have spaces, so we parse from the right
            # Format: 8-char fields right-aligned after the phase name (22 chars)
            phase=$(echo "$clean" | sed 's/^  //' | cut -c1-22 | sed 's/[[:space:]]*$//')
            local rest
            rest=$(echo "$clean" | sed 's/^  //' | cut -c23-)
            read -r total ok limited fail rps p50 p95 p99 <<< "$rest" || true

            if [[ -n "$phase" && -n "$total" ]]; then
                RESULTS["${config_name}:${phase}"]="${total}|${ok}|${limited}|${fail}|${rps}|${p50}|${p95}|${p99}"
            fi
        fi
    done < "$output_file"
}

# Run a single benchmark round
run_round() {
    local config_name="$1" url="$2" shard_count="$3"

    echo ""
    echo -e "${CYAN}${BOLD}================================================================${NC}"
    echo -e "${CYAN}${BOLD}  ROUND: $config_name${NC}"
    echo -e "${CYAN}${BOLD}================================================================${NC}"
    echo ""

    local output_file
    output_file=$(mktemp)

    WRITE_URL="$url" READ_URL="$url" ALL_URL="$url" \
    SHARD_COUNT="$shard_count" \
    DURATION="$DURATION" \
    WRITE_CONCURRENCY="$WRITE_CONCURRENCY" \
    READ_CONCURRENCY="$READ_CONCURRENCY" \
    BULK_SIZE="$BULK_SIZE" \
    bash "$STRESS_SCRIPT" 2>&1 | tee "$output_file" || true

    parse_stress_output "$config_name" "$output_file"
    rm -f "$output_file"

    CONFIG_NAMES+=("$config_name")
}

# --- Phase names in display order ---
PHASE_NAMES=("Write Stress" "Read Stress" "Mixed Read/Write" "Cross-Index Fan-out" "Rate Limit" "Backpressure")

# Print the final comparison table
print_comparison() {
    echo ""
    echo -e "${CYAN}${BOLD}================================================================${NC}"
    echo -e "${CYAN}${BOLD}  BENCHMARK COMPARISON${NC}"
    echo -e "${CYAN}${BOLD}================================================================${NC}"
    echo ""

    # Header
    printf "  ${BOLD}%-22s" "Phase"
    for cfg in "${CONFIG_NAMES[@]}"; do
        printf " │ %-20s" "$cfg"
    done
    printf "${NC}\n"

    printf "  %-22s" ""
    for cfg in "${CONFIG_NAMES[@]}"; do
        printf " │ %8s %10s" "req/s" "p50"
    done
    printf "\n"

    # Separator
    printf "  "
    printf '─%.0s' {1..22}
    for cfg in "${CONFIG_NAMES[@]}"; do
        printf "─┼─"
        printf '─%.0s' {1..20}
    done
    printf "\n"

    # Data rows
    for phase in "${PHASE_NAMES[@]}"; do
        printf "  %-22s" "$phase"
        for cfg in "${CONFIG_NAMES[@]}"; do
            local key="${cfg}:${phase}"
            if [[ -n "${RESULTS[$key]+x}" ]]; then
                IFS='|' read -r total ok limited fail rps p50 p95 p99 <<< "${RESULTS[$key]}"
                printf " │ %8s %10s" "$rps" "${p50}s"
            else
                printf " │ %8s %10s" "—" "—"
            fi
        done
        printf "\n"
    done

    echo ""

    # Detailed table with all metrics
    echo -e "${BOLD}  Detailed Results:${NC}"
    echo ""
    printf "  ${BOLD}%-22s %-14s %8s %8s %8s %8s %10s %8s %8s %8s${NC}\n" \
        "Phase" "Config" "Total" "OK" "429" "Err" "Req/s" "p50" "p95" "p99"
    printf "  %-22s %-14s %8s %8s %8s %8s %10s %8s %8s %8s\n" \
        "──────────────────────" "──────────────" "────────" "────────" "────────" "────────" "──────────" "────────" "────────" "────────"

    for phase in "${PHASE_NAMES[@]}"; do
        for cfg in "${CONFIG_NAMES[@]}"; do
            local key="${cfg}:${phase}"
            if [[ -n "${RESULTS[$key]+x}" ]]; then
                IFS='|' read -r total ok limited fail rps p50 p95 p99 <<< "${RESULTS[$key]}"
                printf "  %-22s %-14s %8s %8s %8s %8s %10s %8s %8s %8s\n" \
                    "$phase" "$cfg" "$total" "$ok" "$limited" "$fail" "$rps" "$p50" "$p95" "$p99"
            fi
        done
    done
    echo ""
}

# ============================================================
# Main
# ============================================================

echo -e "${CYAN}${BOLD}================================================================${NC}"
echo -e "${CYAN}${BOLD}  DuckDB Cluster — Benchmark Comparison${NC}"
echo -e "${CYAN}${BOLD}================================================================${NC}"
echo ""
echo -e "  Config: duration=${BOLD}${DURATION}s${NC}  writers=${BOLD}$WRITE_CONCURRENCY${NC}  readers=${BOLD}$READ_CONCURRENCY${NC}  bulk=${BOLD}$BULK_SIZE${NC}"
echo ""

# ---- Round 1: Standalone DuckDB ----
if [[ "$SKIP_STANDALONE" != "1" ]]; then
    echo -e "${BOLD}Building and starting standalone DuckDB...${NC}"
    docker compose -f "$COMPOSE_FILE" up -d --build standalone 2>&1 | tail -5
    wait_for_health "http://localhost:8090" "standalone" "$MAX_WAIT"

    run_round "Standalone" "http://localhost:8090" 1

    echo ""
    echo -e "${BOLD}Stopping standalone...${NC}"
    docker compose -f "$COMPOSE_FILE" stop standalone 2>&1 | tail -3
    docker compose -f "$COMPOSE_FILE" rm -f standalone 2>&1 | tail -3
fi

# ---- Rounds 2-4: Cluster with 1/2/3 shards ----
if [[ "$SKIP_CLUSTER" != "1" ]]; then
    echo ""
    echo -e "${BOLD}Building and starting cluster baseline...${NC}"
    docker compose -f "$COMPOSE_FILE" up -d --build cluster-baseline 2>&1 | tail -5
    wait_for_health "http://localhost:8091" "cluster-baseline" "$MAX_WAIT"

    for shards in 1 2 3; do
        run_round "${shards}-Shard" "http://localhost:8091" "$shards"
    done

    echo ""
    echo -e "${BOLD}Stopping cluster baseline...${NC}"
    docker compose -f "$COMPOSE_FILE" down -v 2>&1 | tail -3
fi

# ---- Comparison ----
print_comparison

echo -e "${GREEN}${BOLD}BENCHMARK COMPLETE${NC}"
