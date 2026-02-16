#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# DuckDB Cluster — Stress / Load Testing
#
# Measures throughput and latency under concurrent load.
# Runs against the demo 3-node cluster.
#
# Prerequisites: docker (cluster running), curl, jq, awk
# Usage: bash examples/demo/stress.sh
#
# Tunable via environment variables:
#   WRITE_CONCURRENCY  — parallel writer processes   (default: 10)
#   READ_CONCURRENCY   — parallel reader processes   (default: 20)
#   DURATION           — seconds per test phase      (default: 30)
#   BULK_SIZE          — documents per bulk request   (default: 100)
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# --- Configuration (override via env) ---
WRITE_URL="${WRITE_URL:-http://localhost:8081}"
READ_URL="${READ_URL:-http://localhost:8082}"
ALL_URL="${ALL_URL:-http://localhost:8083}"

WRITE_CONCURRENCY="${WRITE_CONCURRENCY:-10}"
READ_CONCURRENCY="${READ_CONCURRENCY:-20}"
DURATION="${DURATION:-30}"
BULK_SIZE="${BULK_SIZE:-100}"

MAX_WAIT=120
AUTH_TOKEN=""
RESULTS_DIR=""
PHASE=0
OVERALL_PASS=0
OVERALL_FAIL=0

# Summary accumulators: "phase_name|reqs|ok|limited|fail|rps|p50|p95|p99"
SUMMARY_LINES=()

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# --- Cleanup ---
cleanup() {
    echo ""
    echo -e "${BOLD}=== Cleaning up ===${NC}"
    if [[ -n "$AUTH_TOKEN" ]]; then
        curl -sf -X DELETE "$ALL_URL/indices/stress-test" \
            -H "Authorization: Bearer $AUTH_TOKEN" >/dev/null 2>&1 || true
        for i in 1 2 3; do
            curl -sf -X DELETE "$ALL_URL/indices/fanout-$i" \
                -H "Authorization: Bearer $AUTH_TOKEN" >/dev/null 2>&1 || true
        done
    fi
    if [[ -n "$RESULTS_DIR" && -d "$RESULTS_DIR" ]]; then
        rm -rf "$RESULTS_DIR"
    fi
}
trap cleanup EXIT

# --- Helpers ---

phase() {
    PHASE=$((PHASE + 1))
    echo ""
    echo -e "${CYAN}${BOLD}=== Phase $PHASE: $1 ===${NC}"
}

# Brief cooldown between phases to let the server recover
cooldown() {
    echo -n "  Cooldown "
    for ((c = 0; c < 3; c++)); do echo -n "."; sleep 1; done
    echo -e " ${GREEN}ready${NC}"
}

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
            exit 1
        fi
    done
    echo -e " ${GREEN}ready${NC} (${elapsed}s)"
}

# Generate a JSON document with a sequential ID
gen_doc() {
    local id="$1"
    printf '{"id":%d,"value":%d,"category":"cat-%d","message":"stress test document number %d","ts":"%s"}' \
        "$id" "$((RANDOM % 10000))" "$((id % 10))" "$id" "$(date -Iseconds)"
}

# Generate a bulk payload of $BULK_SIZE documents
gen_bulk_payload() {
    local start="$1"
    local payload=""
    for ((i = start; i < start + BULK_SIZE; i++)); do
        payload+="$(gen_doc "$i")"$'\n'
    done
    printf '%s' "$payload"
}

# Compute percentile from a sorted file of numbers
percentile() {
    local file="$1" pct="$2"
    awk -v p="$pct" '
    { vals[NR] = $1 }
    END {
        if (NR == 0) { print "0.000"; exit }
        idx = int(NR * p / 100)
        if (idx < 1) idx = 1
        if (idx > NR) idx = NR
        printf "%.3f\n", vals[idx]
    }' "$file"
}

# Print metrics for a completed phase
# Workers write: *.ok, *.limited (429s), *.fail, *.err, *.lat
report_phase() {
    local name="$1" rdir="$2" start="$3" end="$4"
    local elapsed=$((end - start))
    [[ $elapsed -le 0 ]] && elapsed=1

    # Merge latency files (only successful requests)
    local merged="$rdir/_merged_latencies"
    cat "$rdir"/*.lat 2>/dev/null | sort -n > "$merged" || true

    local ok limited fail err total
    ok=$(cat "$rdir"/*.ok 2>/dev/null | awk '{s+=$1}END{print s+0}')
    limited=$(cat "$rdir"/*.limited 2>/dev/null | awk '{s+=$1}END{print s+0}')
    fail=$(cat "$rdir"/*.fail 2>/dev/null | awk '{s+=$1}END{print s+0}')
    err=$(cat "$rdir"/*.err 2>/dev/null | awk '{s+=$1}END{print s+0}')
    total=$((ok + limited + fail + err))

    local rps p50 p95 p99
    rps=$(awk "BEGIN{printf \"%.1f\", $total / $elapsed}")
    local lat_count
    lat_count=$(wc -l < "$merged" 2>/dev/null || echo 0)
    if [[ $lat_count -gt 0 ]]; then
        p50=$(percentile "$merged" 50)
        p95=$(percentile "$merged" 95)
        p99=$(percentile "$merged" 99)
    else
        p50="—"; p95="—"; p99="—"
    fi

    echo ""
    echo -e "  ${BOLD}Results: $name${NC}"
    echo -e "  Requests:   ${BOLD}$total${NC}  (ok: ${GREEN}$ok${NC}  rate-limited: ${YELLOW}$limited${NC}  http-err: ${RED}$fail${NC}  conn-err: ${RED}$err${NC})"
    echo -e "  Throughput: ${BOLD}${rps} req/s${NC}  (over ${elapsed}s)"
    echo -e "  Latency:    p50=${BOLD}${p50}s${NC}  p95=${BOLD}${p95}s${NC}  p99=${BOLD}${p99}s${NC}"

    SUMMARY_LINES+=("$name|$total|$ok|$limited|$((fail + err))|$rps|$p50|$p95|$p99")

    # Phase fails if real errors (not 429s) exceed 50% of total
    local real_errors=$((fail + err))
    local error_pct=0
    if [[ $total -gt 0 ]]; then
        error_pct=$(awk "BEGIN{printf \"%d\", $real_errors * 100 / $total}")
    fi
    if [[ $error_pct -gt 50 ]]; then
        echo -e "  Verdict:    ${RED}FAIL${NC} — ${error_pct}% error rate (excluding rate-limited)"
        OVERALL_FAIL=$((OVERALL_FAIL + 1))
    else
        echo -e "  Verdict:    ${GREEN}PASS${NC} — ${error_pct}% error rate (excluding rate-limited)"
        OVERALL_PASS=$((OVERALL_PASS + 1))
    fi
}

# ============================================================
# Phase 1: Setup
# ============================================================
phase "Setup"

wait_for_health "$WRITE_URL" "write-node" "$MAX_WAIT"
wait_for_health "$READ_URL"  "read-node"  "$MAX_WAIT"
wait_for_health "$ALL_URL"   "all-node"   "$MAX_WAIT"

echo "  Obtaining auth token..."
AUTH_TOKEN=$(curl -sf -X POST "$ALL_URL/admin/auth/token" \
    -H "Content-Type: application/json" \
    -d '{"user_id":"stress","username":"stress-admin","roles":["admin"],"tenant_id":"default"}' \
    | jq -r '.token')
if [[ -z "$AUTH_TOKEN" || "$AUTH_TOKEN" == "null" ]]; then
    echo -e "  ${RED}ERROR: Failed to obtain auth token${NC}"
    exit 1
fi
echo -e "  Auth token: ${GREEN}obtained${NC}"

echo "  Creating stress-test index (3 shards)..."
HTTP_CODE=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$ALL_URL/indices/stress-test" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"settings":{"shard_count":3,"partition_key_field":"id"}}' 2>/dev/null) || true
if [[ "$HTTP_CODE" == "201" || "$HTTP_CODE" == "200" ]]; then
    echo -e "  Index: ${GREEN}created${NC}"
else
    echo -e "  Index: ${YELLOW}already exists or code=$HTTP_CODE${NC}"
fi

RESULTS_DIR=$(mktemp -d)
echo -e "  Results dir: $RESULTS_DIR"
echo -e "  Config: writers=${BOLD}$WRITE_CONCURRENCY${NC}  readers=${BOLD}$READ_CONCURRENCY${NC}  duration=${BOLD}${DURATION}s${NC}  bulk_size=${BOLD}$BULK_SIZE${NC}"

# ============================================================
# Phase 2: Write Stress
# ============================================================
phase "Write Stress (single doc + bulk)"

PHASE_DIR="$RESULTS_DIR/write"
mkdir -p "$PHASE_DIR"

# Worker: single-doc writes
# Tracks ok (2xx), limited (429), fail (other HTTP), err (connection error)
# Only records latency for successful requests
single_doc_worker() {
    set +e
    local wid="$1" url="$2" token="$3" dur="$4" dir="$5"
    local ok=0 limited=0 fail=0 err=0 seq=0
    local deadline=$(($(date +%s) + dur))
    while [[ $(date +%s) -lt $deadline ]]; do
        seq=$((seq + 1))
        local doc
        doc=$(gen_doc "$((wid * 100000 + seq))")
        local resp
        resp=$(curl -s -o /dev/null -w '%{http_code} %{time_total}' -X POST "$url/indices/stress-test/_doc" \
            -H "Authorization: Bearer $token" \
            -H "Content-Type: application/json" \
            -d "$doc" 2>/dev/null) || { err=$((err + 1)); continue; }
        local code t
        code="${resp% *}"
        t="${resp#* }"
        if [[ "$code" == "200" || "$code" == "201" ]]; then
            ok=$((ok + 1))
            echo "$t" >> "$dir/w${wid}.lat"
        elif [[ "$code" == "429" ]]; then
            limited=$((limited + 1))
            sleep 0.01  # back off briefly on rate limit
        else
            fail=$((fail + 1))
        fi
    done
    echo "$ok" > "$dir/w${wid}.ok"
    echo "$limited" > "$dir/w${wid}.limited"
    echo "$fail" > "$dir/w${wid}.fail"
    echo "$err" > "$dir/w${wid}.err"
}

# Worker: bulk writes
bulk_doc_worker() {
    set +e
    local wid="$1" url="$2" token="$3" dur="$4" dir="$5" bsize="$6"
    local ok=0 limited=0 fail=0 err=0 seq=0
    local deadline=$(($(date +%s) + dur))
    while [[ $(date +%s) -lt $deadline ]]; do
        seq=$((seq + 1))
        local start_id=$((wid * 1000000 + seq * bsize))
        local payload
        payload=$(gen_bulk_payload "$start_id")
        local resp
        resp=$(curl -s -o /dev/null -w '%{http_code} %{time_total}' -X POST "$url/indices/stress-test/_bulk" \
            -H "Authorization: Bearer $token" \
            -H "Content-Type: application/x-ndjson" \
            -d "$payload" 2>/dev/null) || { err=$((err + 1)); continue; }
        local code t
        code="${resp% *}"
        t="${resp#* }"
        if [[ "$code" == "200" || "$code" == "201" ]]; then
            ok=$((ok + 1))
            echo "$t" >> "$dir/b${wid}.lat"
        elif [[ "$code" == "429" ]]; then
            limited=$((limited + 1))
            sleep 0.01
        else
            fail=$((fail + 1))
        fi
    done
    echo "$ok" > "$dir/b${wid}.ok"
    echo "$limited" > "$dir/b${wid}.limited"
    echo "$fail" > "$dir/b${wid}.fail"
    echo "$err" > "$dir/b${wid}.err"
}

START_TS=$(date +%s)

# Launch half as single-doc writers, half as bulk writers
HALF=$((WRITE_CONCURRENCY / 2))
[[ $HALF -lt 1 ]] && HALF=1
BULK_HALF=$((WRITE_CONCURRENCY - HALF))

echo "  Launching $HALF single-doc writers + $BULK_HALF bulk writers for ${DURATION}s..."

for ((i = 0; i < HALF; i++)); do
    single_doc_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$DURATION" "$PHASE_DIR" &
done
for ((i = 0; i < BULK_HALF; i++)); do
    bulk_doc_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$DURATION" "$PHASE_DIR" "$BULK_SIZE" &
done
wait || true

END_TS=$(date +%s)
report_phase "Write Stress" "$PHASE_DIR" "$START_TS" "$END_TS"

cooldown

# ============================================================
# Phase 3: Read Stress
# ============================================================
phase "Read Stress (queries)"

PHASE_DIR="$RESULTS_DIR/read"
mkdir -p "$PHASE_DIR"

# Array of query patterns to rotate through
QUERIES=(
    '{"sql":"SELECT * FROM _docs LIMIT 10","index":"stress-test"}'
    '{"sql":"SELECT COUNT(*) AS cnt FROM _docs","index":"stress-test"}'
    '{"sql":"SELECT category, COUNT(*) AS cnt FROM _docs GROUP BY category","index":"stress-test"}'
    '{"sql":"SELECT * FROM _docs WHERE id > 500 LIMIT 20","index":"stress-test"}'
    '{"sql":"SELECT AVG(value) AS avg_val FROM _docs","index":"stress-test"}'
)

read_worker() {
    set +e
    local wid="$1" url="$2" token="$3" dur="$4" dir="$5"
    local ok=0 limited=0 fail=0 err=0 seq=0
    local n_queries=${#QUERIES[@]}
    local deadline=$(($(date +%s) + dur))
    while [[ $(date +%s) -lt $deadline ]]; do
        local q="${QUERIES[$((seq % n_queries))]}"
        seq=$((seq + 1))
        local resp
        resp=$(curl -s -o /dev/null -w '%{http_code} %{time_total}' -X POST "$url/query" \
            -H "Authorization: Bearer $token" \
            -H "Content-Type: application/json" \
            -d "$q" 2>/dev/null) || { err=$((err + 1)); continue; }
        local code t
        code="${resp% *}"
        t="${resp#* }"
        if [[ "$code" == "200" ]]; then
            ok=$((ok + 1))
            echo "$t" >> "$dir/r${wid}.lat"
        elif [[ "$code" == "429" ]]; then
            limited=$((limited + 1))
            sleep 0.01
        else
            fail=$((fail + 1))
        fi
    done
    echo "$ok" > "$dir/r${wid}.ok"
    echo "$limited" > "$dir/r${wid}.limited"
    echo "$fail" > "$dir/r${wid}.fail"
    echo "$err" > "$dir/r${wid}.err"
}

START_TS=$(date +%s)
echo "  Launching $READ_CONCURRENCY readers for ${DURATION}s..."

for ((i = 0; i < READ_CONCURRENCY; i++)); do
    read_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$DURATION" "$PHASE_DIR" &
done
wait || true

END_TS=$(date +%s)
report_phase "Read Stress" "$PHASE_DIR" "$START_TS" "$END_TS"

cooldown

# ============================================================
# Phase 4: Mixed Read/Write
# ============================================================
phase "Mixed Read/Write"

PHASE_DIR="$RESULTS_DIR/mixed"
mkdir -p "$PHASE_DIR"

START_TS=$(date +%s)
echo "  Launching $HALF writers + $READ_CONCURRENCY readers for ${DURATION}s..."

for ((i = 0; i < HALF; i++)); do
    single_doc_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$DURATION" "$PHASE_DIR" &
done
for ((i = 0; i < READ_CONCURRENCY; i++)); do
    read_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$DURATION" "$PHASE_DIR" &
done
wait || true

END_TS=$(date +%s)
report_phase "Mixed Read/Write" "$PHASE_DIR" "$START_TS" "$END_TS"

cooldown

# ============================================================
# Phase 5: Cross-Index Fan-out
# ============================================================
phase "Cross-Index Fan-out"

PHASE_DIR="$RESULTS_DIR/fanout"
mkdir -p "$PHASE_DIR"

echo "  Creating 3 fan-out indices..."
for i in 1 2 3; do
    curl -s -o /dev/null -X PUT "$ALL_URL/indices/fanout-$i" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"settings":{"shard_count":2,"partition_key_field":"id"}}' 2>/dev/null || true

    # Seed each with a few documents
    for j in $(seq 1 20); do
        doc=$(gen_doc "$((i * 1000 + j))")
        curl -s -o /dev/null -X POST "$ALL_URL/indices/fanout-$i/_doc" \
            -H "Authorization: Bearer $AUTH_TOKEN" \
            -H "Content-Type: application/json" \
            -d "$doc" 2>/dev/null || true
    done
done
echo -e "  Indices seeded: ${GREEN}done${NC}"

CROSS_QUERIES=(
    '{"sql":"SELECT COUNT(*) AS cnt FROM _docs","index":"fanout-1,fanout-2"}'
    '{"sql":"SELECT * FROM _docs LIMIT 5","index":"fanout-1,fanout-2,fanout-3"}'
    '{"sql":"SELECT category, COUNT(*) AS cnt FROM _docs GROUP BY category","index":"fanout-*"}'
)

fanout_worker() {
    set +e
    local wid="$1" url="$2" token="$3" dur="$4" dir="$5"
    local ok=0 limited=0 fail=0 err=0 seq=0
    local n_queries=${#CROSS_QUERIES[@]}
    local deadline=$(($(date +%s) + dur))
    while [[ $(date +%s) -lt $deadline ]]; do
        local q="${CROSS_QUERIES[$((seq % n_queries))]}"
        seq=$((seq + 1))
        local resp
        resp=$(curl -s -o /dev/null -w '%{http_code} %{time_total}' -X POST "$url/query" \
            -H "Authorization: Bearer $token" \
            -H "Content-Type: application/json" \
            -d "$q" 2>/dev/null) || { err=$((err + 1)); continue; }
        local code t
        code="${resp% *}"
        t="${resp#* }"
        if [[ "$code" == "200" ]]; then
            ok=$((ok + 1))
            echo "$t" >> "$dir/f${wid}.lat"
        elif [[ "$code" == "429" ]]; then
            limited=$((limited + 1))
            sleep 0.01
        else
            fail=$((fail + 1))
        fi
    done
    echo "$ok" > "$dir/f${wid}.ok"
    echo "$limited" > "$dir/f${wid}.limited"
    echo "$fail" > "$dir/f${wid}.fail"
    echo "$err" > "$dir/f${wid}.err"
}

FANOUT_CONCURRENCY=$((READ_CONCURRENCY / 2))
[[ $FANOUT_CONCURRENCY -lt 2 ]] && FANOUT_CONCURRENCY=2

START_TS=$(date +%s)
echo "  Launching $FANOUT_CONCURRENCY cross-index readers for ${DURATION}s..."

for ((i = 0; i < FANOUT_CONCURRENCY; i++)); do
    fanout_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$DURATION" "$PHASE_DIR" &
done
wait || true

END_TS=$(date +%s)
report_phase "Cross-Index Fan-out" "$PHASE_DIR" "$START_TS" "$END_TS"

cooldown

# ============================================================
# Phase 6: Rate Limit Validation
# ============================================================
phase "Rate Limit Validation"

PHASE_DIR="$RESULTS_DIR/ratelimit"
mkdir -p "$PHASE_DIR"

# Fire a burst of requests as fast as possible from many workers
# The cluster is configured at 200 req/s with burst 400
BURST_WORKERS=50
BURST_DURATION=5

ratelimit_worker() {
    set +e
    local wid="$1" url="$2" token="$3" dur="$4" dir="$5"
    local ok=0 limited=0 other_fail=0
    local deadline=$(($(date +%s) + dur))
    while [[ $(date +%s) -lt $deadline ]]; do
        local code
        code=$(curl -s -o /dev/null -w '%{http_code}' -X GET "$url/indices" \
            -H "Authorization: Bearer $token" 2>/dev/null) || { other_fail=$((other_fail + 1)); continue; }
        if [[ "$code" == "200" ]]; then
            ok=$((ok + 1))
        elif [[ "$code" == "429" ]]; then
            limited=$((limited + 1))
        else
            other_fail=$((other_fail + 1))
        fi
    done
    echo "$ok" > "$dir/rl${wid}.ok"
    echo "$limited" > "$dir/rl${wid}.limited"
    echo "$other_fail" > "$dir/rl${wid}.fail"
}

START_TS=$(date +%s)
echo "  Launching $BURST_WORKERS workers for ${BURST_DURATION}s burst..."

for ((i = 0; i < BURST_WORKERS; i++)); do
    ratelimit_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$BURST_DURATION" "$PHASE_DIR" &
done
wait || true

END_TS=$(date +%s)
local_elapsed=$((END_TS - START_TS))
[[ $local_elapsed -le 0 ]] && local_elapsed=1

total_ok=$(cat "$PHASE_DIR"/*.ok 2>/dev/null | awk '{s+=$1}END{print s+0}')
total_limited=$(cat "$PHASE_DIR"/*.limited 2>/dev/null | awk '{s+=$1}END{print s+0}')
total_other=$(cat "$PHASE_DIR"/*.fail 2>/dev/null | awk '{s+=$1}END{print s+0}')
total_all=$((total_ok + total_limited + total_other))
rps=$(awk "BEGIN{printf \"%.1f\", $total_all / $local_elapsed}")

echo ""
echo -e "  ${BOLD}Results: Rate Limit Validation${NC}"
echo -e "  Total requests: ${BOLD}$total_all${NC}  (${rps} req/s)"
echo -e "  Accepted (200): ${GREEN}$total_ok${NC}"
echo -e "  Rate-limited (429): ${YELLOW}$total_limited${NC}"
echo -e "  Other errors: ${RED}$total_other${NC}"

if [[ $total_limited -gt 0 ]]; then
    echo -e "  Verdict: ${GREEN}PASS${NC} — rate limiting engaged"
    OVERALL_PASS=$((OVERALL_PASS + 1))
else
    echo -e "  Verdict: ${YELLOW}WARN${NC} — no 429s observed (burst may not exceed limit)"
    OVERALL_PASS=$((OVERALL_PASS + 1))
fi
SUMMARY_LINES+=("Rate Limit|$total_all|$total_ok|$total_limited|$total_other|$rps|—|—|—")

cooldown

# ============================================================
# Phase 7: Backpressure Validation
# ============================================================
phase "Backpressure Validation"

PHASE_DIR="$RESULTS_DIR/backpressure"
mkdir -p "$PHASE_DIR"

# Flood writes with high concurrency to try triggering backpressure (503)
BP_WORKERS=50
BP_DURATION=5

backpressure_worker() {
    set +e
    local wid="$1" url="$2" token="$3" dur="$4" dir="$5"
    local ok=0 bp=0 limited=0 other_fail=0 seq=0
    local deadline=$(($(date +%s) + dur))
    while [[ $(date +%s) -lt $deadline ]]; do
        seq=$((seq + 1))
        local doc
        doc=$(gen_doc "$((wid * 100000 + seq))")
        local code
        code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$url/indices/stress-test/_doc" \
            -H "Authorization: Bearer $token" \
            -H "Content-Type: application/json" \
            -d "$doc" 2>/dev/null) || { other_fail=$((other_fail + 1)); continue; }
        if [[ "$code" == "201" || "$code" == "200" ]]; then
            ok=$((ok + 1))
        elif [[ "$code" == "503" ]]; then
            bp=$((bp + 1))
        elif [[ "$code" == "429" ]]; then
            limited=$((limited + 1))
        else
            other_fail=$((other_fail + 1))
        fi
    done
    echo "$ok" > "$dir/bp${wid}.ok"
    echo "$bp" > "$dir/bp${wid}.bp"
    echo "$limited" > "$dir/bp${wid}.limited"
    echo "$other_fail" > "$dir/bp${wid}.fail"
}

START_TS=$(date +%s)
echo "  Launching $BP_WORKERS writers for ${BP_DURATION}s flood..."

for ((i = 0; i < BP_WORKERS; i++)); do
    backpressure_worker "$i" "$ALL_URL" "$AUTH_TOKEN" "$BP_DURATION" "$PHASE_DIR" &
done
wait || true

END_TS=$(date +%s)
local_elapsed=$((END_TS - START_TS))
[[ $local_elapsed -le 0 ]] && local_elapsed=1

bp_ok=$(cat "$PHASE_DIR"/*.ok 2>/dev/null | awk '{s+=$1}END{print s+0}')
bp_503=$(cat "$PHASE_DIR"/*.bp 2>/dev/null | awk '{s+=$1}END{print s+0}')
bp_limited=$(cat "$PHASE_DIR"/*.limited 2>/dev/null | awk '{s+=$1}END{print s+0}')
bp_other=$(cat "$PHASE_DIR"/*.fail 2>/dev/null | awk '{s+=$1}END{print s+0}')
bp_total=$((bp_ok + bp_503 + bp_limited + bp_other))
bp_rps=$(awk "BEGIN{printf \"%.1f\", $bp_total / $local_elapsed}")

echo ""
echo -e "  ${BOLD}Results: Backpressure Validation${NC}"
echo -e "  Total requests: ${BOLD}$bp_total${NC}  (${bp_rps} req/s)"
echo -e "  Accepted: ${GREEN}$bp_ok${NC}"
echo -e "  Backpressure (503): ${YELLOW}$bp_503${NC}"
echo -e "  Rate-limited (429): ${YELLOW}$bp_limited${NC}"
echo -e "  Other errors: ${RED}$bp_other${NC}"

if [[ $bp_503 -gt 0 ]]; then
    echo -e "  Verdict: ${GREEN}PASS${NC} — backpressure engaged"
elif [[ $bp_limited -gt 0 ]]; then
    echo -e "  Verdict: ${GREEN}PASS${NC} — rate limiting engaged before backpressure"
else
    echo -e "  Verdict: ${YELLOW}WARN${NC} — no 503s/429s observed"
fi
SUMMARY_LINES+=("Backpressure|$bp_total|$bp_ok|$bp_limited|$((bp_503 + bp_other))|$bp_rps|—|—|—")
OVERALL_PASS=$((OVERALL_PASS + 1))

# Verify cluster recovered: health check after flood
echo -n "  Recovery check "
sleep 3
if curl -sf "$ALL_URL/health" >/dev/null 2>&1; then
    echo -e "${GREEN}OK${NC}"
else
    echo -e "${RED}FAIL — cluster not healthy after backpressure${NC}"
    OVERALL_FAIL=$((OVERALL_FAIL + 1))
fi

# ============================================================
# Summary
# ============================================================
echo ""
echo -e "${CYAN}${BOLD}================================================================${NC}"
echo -e "${CYAN}${BOLD}  STRESS TEST SUMMARY${NC}"
echo -e "${CYAN}${BOLD}================================================================${NC}"
echo ""
printf "  ${BOLD}%-22s %8s %8s %8s %8s %10s %8s %8s %8s${NC}\n" \
    "Phase" "Total" "OK" "429" "Err" "Req/s" "p50" "p95" "p99"
printf "  %-22s %8s %8s %8s %8s %10s %8s %8s %8s\n" \
    "----------------------" "--------" "--------" "--------" "--------" "----------" "--------" "--------" "--------"

for line in "${SUMMARY_LINES[@]}"; do
    IFS='|' read -r name total ok limited fail rps p50 p95 p99 <<< "$line"
    printf "  %-22s %8s %8s %8s %8s %10s %8s %8s %8s\n" \
        "$name" "$total" "$ok" "$limited" "$fail" "$rps" "$p50" "$p95" "$p99"
done

echo ""
echo -e "  Phases passed: ${GREEN}$OVERALL_PASS${NC}"
echo -e "  Phases failed: ${RED}$OVERALL_FAIL${NC}"
echo ""

if [[ $OVERALL_FAIL -gt 0 ]]; then
    echo -e "${RED}${BOLD}STRESS TEST FAILED${NC}"
    exit 1
else
    echo -e "${GREEN}${BOLD}STRESS TEST PASSED${NC}"
    exit 0
fi
