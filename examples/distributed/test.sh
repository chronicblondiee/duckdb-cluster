#!/usr/bin/env bash
set -euo pipefail

# DuckDB Cluster — Distributed UAT
#
# Prerequisites: docker, curl, jq
# Usage: bash examples/distributed/test.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yaml"

WRITE_URL="http://localhost:8081"
READ_URL="http://localhost:8082"
ALL_URL="http://localhost:8083"

MAX_WAIT=120
PASSED=0
FAILED=0

# --- Cleanup ---
cleanup() {
    echo ""
    echo "=== Tearing down cluster ==="
    docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
}
trap cleanup EXIT

# --- Helpers ---

run_test() {
    local name="$1"
    shift
    echo -n "  $name ... "
    if output=$("$@" 2>&1); then
        echo "PASS"
        PASSED=$((PASSED + 1))
    else
        echo "FAIL"
        echo "    $output"
        FAILED=$((FAILED + 1))
    fi
}

# assert_http METHOD URL DATA EXPECTED_STATUS [JQ_FILTER JQ_EXPECTED]
assert_http() {
    local method="$1"
    local url="$2"
    local data="$3"
    local expected_status="$4"
    local jq_filter="${5:-}"
    local jq_expected="${6:-}"

    local tmpfile
    tmpfile=$(mktemp)

    local curl_args=(-s -o "$tmpfile" -w "%{http_code}" -X "$method")
    if [ -n "$data" ]; then
        curl_args+=(-H "Content-Type: application/json" -d "$data")
    fi
    curl_args+=("$url")

    local status_code
    status_code=$(curl "${curl_args[@]}")

    if [ "$status_code" != "$expected_status" ]; then
        local body
        body=$(cat "$tmpfile")
        rm -f "$tmpfile"
        echo "expected HTTP $expected_status, got $status_code: $body"
        return 1
    fi

    if [ -n "$jq_filter" ] && [ -n "$jq_expected" ]; then
        local actual
        actual=$(jq -r "$jq_filter" "$tmpfile")
        if [ "$actual" != "$jq_expected" ]; then
            rm -f "$tmpfile"
            echo "expected $jq_filter = '$jq_expected', got '$actual'"
            return 1
        fi
    fi

    rm -f "$tmpfile"
    return 0
}

wait_for_health() {
    local url="$1"
    local name="$2"
    local max="$3"
    local elapsed=0
    echo -n "  Waiting for $name "
    while [ $elapsed -lt "$max" ]; do
        if curl -sf "$url/health" > /dev/null 2>&1; then
            echo " ready (${elapsed}s)"
            return 0
        fi
        echo -n "."
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo " TIMEOUT after ${max}s"
    return 1
}

# --- Main ---
echo "=== DuckDB Cluster — Distributed UAT ==="
echo ""

echo "=== Starting 3-node cluster ==="
docker compose -f "$COMPOSE_FILE" up --build -d

echo ""
echo "=== Waiting for nodes ==="
wait_for_health "$WRITE_URL" "write-node" "$MAX_WAIT"
wait_for_health "$READ_URL"  "read-node"  "$MAX_WAIT"
wait_for_health "$ALL_URL"   "all-node"   "$MAX_WAIT"

echo ""
echo "=== Health checks ==="

run_test "Health: write-node" \
    assert_http GET "$WRITE_URL/health" "" 200 ".status" "healthy"

run_test "Health: read-node" \
    assert_http GET "$READ_URL/health" "" 200 ".status" "healthy"

run_test "Health: all-node" \
    assert_http GET "$ALL_URL/health" "" 200 ".status" "healthy"

echo ""
echo "=== Index management ==="

run_test "Create index test-logs" \
    assert_http PUT "$ALL_URL/indices/test-logs" \
    '{"settings":{"shard_count":3,"partition_key_field":"_id"}}' \
    201 ".acknowledged" "true"

run_test "Get index test-logs" \
    assert_http GET "$ALL_URL/indices/test-logs" "" 200 ".name" "test-logs"

run_test "List indices" \
    assert_http GET "$ALL_URL/indices" "" 200

echo ""
echo "=== Document ingestion ==="

run_test "Ingest doc 1" \
    assert_http POST "$ALL_URL/indices/test-logs/_doc" \
    '{"message":"hello world","level":"info","timestamp":"2026-01-01T00:00:00Z"}' \
    201 ".result" "created"

run_test "Ingest doc 2" \
    assert_http POST "$ALL_URL/indices/test-logs/_doc" \
    '{"message":"error occurred","level":"error","timestamp":"2026-01-01T00:01:00Z"}' \
    201 ".result" "created"

run_test "Ingest doc 3" \
    assert_http POST "$ALL_URL/indices/test-logs/_doc" \
    '{"message":"debug trace","level":"debug","timestamp":"2026-01-01T00:02:00Z"}' \
    201 ".result" "created"

run_test "Bulk ingest (2 docs)" \
    assert_http POST "$ALL_URL/indices/test-logs/_bulk" \
    '[{"message":"bulk msg 1","level":"info"},{"message":"bulk msg 2","level":"warn"}]' \
    200 ".succeeded" "2"

echo ""
echo "=== Queries ==="

run_test "Query all docs" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT * FROM _docs","index":"test-logs"}' \
    200 ".success" "true"

run_test "Query with filter" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT * FROM _docs WHERE level = '\''error'\''","index":"test-logs"}' \
    200 ".success" "true"

run_test "Count query" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT COUNT(*) as cnt FROM _docs","index":"test-logs"}' \
    200 ".success" "true"

echo ""
echo "=== Cross-index queries ==="

run_test "Create index test-metrics" \
    assert_http PUT "$ALL_URL/indices/test-metrics" \
    '{"settings":{"shard_count":2}}' \
    201 ".acknowledged" "true"

run_test "Ingest into test-metrics" \
    assert_http POST "$ALL_URL/indices/test-metrics/_doc" \
    '{"metric":"cpu","value":42.5}' \
    201 ".result" "created"

run_test "Cross-index query" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT * FROM _docs","index":"test-logs,test-metrics"}' \
    200 ".success" "true"

echo ""
echo "=== Aliases ==="

run_test "Create alias logs-alias" \
    assert_http PUT "$ALL_URL/aliases/logs-alias" \
    '{"indices":["test-logs"]}' \
    201 ".acknowledged" "true"

run_test "Get alias" \
    assert_http GET "$ALL_URL/aliases/logs-alias" "" 200

run_test "List aliases" \
    assert_http GET "$ALL_URL/aliases" "" 200

echo ""
echo "=== Templates ==="

run_test "Create template log-template" \
    assert_http PUT "$ALL_URL/templates/log-template" \
    '{"index_patterns":["log-*"],"settings":{"shard_count":2},"mappings":{"fields":{"message":{"type":"VARCHAR"},"level":{"type":"VARCHAR"}}}}' \
    201 ".acknowledged" "true"

run_test "Get template" \
    assert_http GET "$ALL_URL/templates/log-template" "" 200

run_test "List templates" \
    assert_http GET "$ALL_URL/templates" "" 200

echo ""
echo "=== Metrics ==="

run_test "Prometheus metrics endpoint" \
    assert_http GET "$ALL_URL/metrics" "" 200

echo ""
echo "=== Cleanup ==="

run_test "Delete alias" \
    assert_http DELETE "$ALL_URL/aliases/logs-alias" "" 200 ".acknowledged" "true"

run_test "Delete template" \
    assert_http DELETE "$ALL_URL/templates/log-template" "" 200 ".acknowledged" "true"

run_test "Delete index test-metrics" \
    assert_http DELETE "$ALL_URL/indices/test-metrics" "" 200 ".acknowledged" "true"

run_test "Delete index test-logs" \
    assert_http DELETE "$ALL_URL/indices/test-logs" "" 200 ".acknowledged" "true"

# --- Summary ---
echo ""
echo "==============================="
echo "  Passed: $PASSED"
echo "  Failed: $FAILED"
echo "  Total:  $((PASSED + FAILED))"
echo "==============================="
echo ""

if [ "$FAILED" -gt 0 ]; then
    exit 1
fi

echo "All tests passed!"
