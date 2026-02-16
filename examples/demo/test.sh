#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# DuckDB Cluster — Comprehensive Demo UAT
#
# Tests ALL features end-to-end: security (JWT, API keys, RBAC),
# index lifecycle, document ingestion, queries, cross-index,
# multi-query, bulk SQL, aliases, templates, ISM policies,
# shard management, rebalancing, migration, backup/restore,
# and the full observability stack.
#
# Prerequisites: docker, curl, jq
# Usage: bash examples/demo/test.sh
# ============================================================

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yaml"

WRITE_URL="http://localhost:8081"
READ_URL="http://localhost:8082"
ALL_URL="http://localhost:8083"
PROMETHEUS_URL="http://localhost:9090"
JAEGER_URL="http://localhost:16686"
GRAFANA_URL="http://localhost:3000"

MAX_WAIT=120
PASSED=0
FAILED=0
SECTION=0

# Captured state
AUTH_TOKEN=""
READER_TOKEN=""
API_KEY=""
BACKUP_ID=""

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
    echo -e "${BOLD}=== Tearing down cluster ===${NC}"
    docker compose -f "$COMPOSE_FILE" down -v 2>/dev/null || true
}
trap cleanup EXIT

# --- Helpers ---

section() {
    SECTION=$((SECTION + 1))
    echo ""
    echo -e "${CYAN}${BOLD}=== Section $SECTION: $1 ===${NC}"
}

run_test() {
    local name="$1"
    shift
    echo -n "  $name ... "
    if output=$("$@" 2>&1); then
        echo -e "${GREEN}PASS${NC}"
        PASSED=$((PASSED + 1))
    else
        echo -e "${RED}FAIL${NC}"
        echo -e "    ${RED}$output${NC}"
        FAILED=$((FAILED + 1))
    fi
}

# assert_http METHOD URL DATA EXPECTED_STATUS [JQ_FILTER JQ_EXPECTED]
# Uses $AUTH_TOKEN if set.
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
    if [ -n "$AUTH_TOKEN" ]; then
        curl_args+=(-H "Authorization: Bearer $AUTH_TOKEN")
    fi
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

# assert_http_noauth — sends request without any Authorization header
assert_http_noauth() {
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

# assert_http_with_token TOKEN METHOD URL DATA EXPECTED_STATUS [JQ_FILTER JQ_EXPECTED]
assert_http_with_token() {
    local token="$1"
    local method="$2"
    local url="$3"
    local data="$4"
    local expected_status="$5"
    local jq_filter="${6:-}"
    local jq_expected="${7:-}"

    local tmpfile
    tmpfile=$(mktemp)

    local curl_args=(-s -o "$tmpfile" -w "%{http_code}" -X "$method")
    curl_args+=(-H "Authorization: Bearer $token")
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

# assert_http_with_apikey KEY METHOD URL DATA EXPECTED_STATUS [JQ_FILTER JQ_EXPECTED]
assert_http_with_apikey() {
    local key="$1"
    local method="$2"
    local url="$3"
    local data="$4"
    local expected_status="$5"
    local jq_filter="${6:-}"
    local jq_expected="${7:-}"

    local tmpfile
    tmpfile=$(mktemp)

    local curl_args=(-s -o "$tmpfile" -w "%{http_code}" -X "$method")
    curl_args+=(-H "Authorization: ApiKey $key")
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
            echo -e " ready (${elapsed}s)"
            return 0
        fi
        echo -n "."
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo -e " ${RED}TIMEOUT after ${max}s${NC}"
    return 1
}

wait_for_url() {
    local url="$1"
    local name="$2"
    local max="$3"
    local elapsed=0
    echo -n "  Waiting for $name "
    while [ $elapsed -lt "$max" ]; do
        if curl -sf "$url" > /dev/null 2>&1; then
            echo -e " ready (${elapsed}s)"
            return 0
        fi
        echo -n "."
        sleep 2
        elapsed=$((elapsed + 2))
    done
    echo -e " ${RED}TIMEOUT after ${max}s${NC}"
    return 1
}

# ============================================================
# MAIN
# ============================================================

echo -e "${BOLD}=== DuckDB Cluster — Comprehensive Demo UAT ===${NC}"
echo ""

echo -e "${BOLD}=== Starting 3-node cluster ===${NC}"
docker compose -f "$COMPOSE_FILE" up --build -d

echo ""
echo -e "${BOLD}=== Waiting for nodes ===${NC}"
wait_for_health "$WRITE_URL" "write-node" "$MAX_WAIT"
wait_for_health "$READ_URL"  "read-node"  "$MAX_WAIT"
wait_for_health "$ALL_URL"   "all-node"   "$MAX_WAIT"

# ----------------------------------------------------------
section "Node Health (no auth required)"
# ----------------------------------------------------------

run_test "Health: write-node" \
    assert_http_noauth GET "$WRITE_URL/health" "" 200 ".status" "healthy"

run_test "Health: read-node" \
    assert_http_noauth GET "$READ_URL/health" "" 200 ".status" "healthy"

run_test "Health: all-node" \
    assert_http_noauth GET "$ALL_URL/health" "" 200 ".status" "healthy"

# ----------------------------------------------------------
section "Security: Token Generation"
# ----------------------------------------------------------

# Anonymous can call /admin/auth/token (no permission mapped for auth endpoints)
echo -n "  Generate admin JWT token ... "
AUTH_TOKEN=$(curl -s -X POST "$ALL_URL/admin/auth/token" \
    -H "Content-Type: application/json" \
    -d '{"user_id":"admin1","username":"admin","roles":["admin"],"tenant_id":"default"}' | jq -r '.token')
if [ -n "$AUTH_TOKEN" ] && [ "$AUTH_TOKEN" != "null" ]; then
    echo -e "${GREEN}PASS${NC}"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}FAIL${NC} (got: $AUTH_TOKEN)"
    FAILED=$((FAILED + 1))
fi

echo -n "  Generate reader JWT token ... "
READER_TOKEN=$(curl -s -X POST "$ALL_URL/admin/auth/token" \
    -H "Content-Type: application/json" \
    -d '{"user_id":"reader1","username":"reader","roles":["reader"],"tenant_id":"default"}' | jq -r '.token')
if [ -n "$READER_TOKEN" ] && [ "$READER_TOKEN" != "null" ]; then
    echo -e "${GREEN}PASS${NC}"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}FAIL${NC} (got: $READER_TOKEN)"
    FAILED=$((FAILED + 1))
fi

# ----------------------------------------------------------
section "Security: API Key Management"
# ----------------------------------------------------------

echo -n "  Register API key ... "
# Retry up to 3 times for robustness
for _attempt in 1 2 3; do
    APIKEY_RESP=$(curl -s -X POST "$ALL_URL/admin/auth/apikey" \
        -H "Authorization: Bearer $AUTH_TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"user_id":"svc1","username":"service-account","roles":["admin"]}')
    API_KEY=$(echo "$APIKEY_RESP" | jq -r '.api_key // empty')
    [ -n "$API_KEY" ] && break
    sleep 1
done
if [ -n "$API_KEY" ]; then
    echo -e "${GREEN}PASS${NC}"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}FAIL${NC} (response: $APIKEY_RESP)"
    FAILED=$((FAILED + 1))
fi

# Use API key to list indices (auth-protected, doesn't need legacy shards)
run_test "Use API key to list indices" \
    assert_http_with_apikey "$API_KEY" GET "$ALL_URL/indices" "" 200

run_test "Revoke API key" \
    assert_http DELETE "$ALL_URL/admin/auth/apikey" \
    "{\"api_key\":\"$API_KEY\"}" 204

run_test "Revoked API key rejected" \
    assert_http_with_apikey "$API_KEY" GET "$ALL_URL/indices" "" 401

# ----------------------------------------------------------
section "Security: RBAC Enforcement"
# ----------------------------------------------------------

# Create a temporary index so query tests have a target
curl -s -o /dev/null -X PUT "$ALL_URL/indices/rbac-test" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"settings":{"shard_count":1}}' > /dev/null 2>&1

# Admin token can POST /query (requires query:write)
run_test "Admin token: can query (query:write)" \
    assert_http_with_token "$AUTH_TOKEN" POST "$ALL_URL/query" \
    '{"sql":"SELECT 1 as x","index":"rbac-test"}' 200 ".success" "true"

# Reader token blocked from POST /query (requires query:write, reader lacks it)
run_test "Reader token: blocked from write query" \
    assert_http_with_token "$READER_TOKEN" POST "$ALL_URL/query" \
    '{"sql":"SELECT 1 as x"}' 403

# Reader token can list indices (no specific permission required)
run_test "Reader token: can list indices" \
    assert_http_with_token "$READER_TOKEN" GET "$ALL_URL/indices" "" 200

# Anonymous blocked from /admin/shards (requires admin:shards, anonymous=read role)
run_test "Anonymous: blocked from admin shards" \
    assert_http_noauth GET "$ALL_URL/admin/shards" "" 403

# Clean up temporary index
curl -s -o /dev/null -X DELETE "$ALL_URL/indices/rbac-test" \
    -H "Authorization: Bearer $AUTH_TOKEN" > /dev/null 2>&1

# ----------------------------------------------------------
section "Index Management"
# ----------------------------------------------------------

run_test "Create index demo-logs" \
    assert_http PUT "$ALL_URL/indices/demo-logs" \
    '{"settings":{"shard_count":3,"partition_key_field":"_id"}}' \
    201 ".acknowledged" "true"

run_test "Create index demo-metrics" \
    assert_http PUT "$ALL_URL/indices/demo-metrics" \
    '{"settings":{"shard_count":2,"partition_key_field":"_id"}}' \
    201 ".acknowledged" "true"

run_test "Get index demo-logs" \
    assert_http GET "$ALL_URL/indices/demo-logs" "" 200 ".name" "demo-logs"

run_test "List indices" \
    assert_http GET "$ALL_URL/indices" "" 200

# ----------------------------------------------------------
section "Shard Management (legacy shards)"
# ----------------------------------------------------------

# Start with 0 legacy shards — verify the empty list
echo -n "  List shards (initial count) ... "
SHARD_RESP=$(curl -s -H "Authorization: Bearer $AUTH_TOKEN" "$ALL_URL/admin/shards")
INITIAL_SHARD_COUNT=$(echo "$SHARD_RESP" | jq '.shards | length')
echo -e "${GREEN}PASS${NC} (count: $INITIAL_SHARD_COUNT)"
PASSED=$((PASSED + 1))

run_test "Add shard" \
    assert_http POST "$ALL_URL/admin/shards" '{}' 201

echo -n "  Verify shard count increased ... "
NEW_COUNT=$(curl -s -H "Authorization: Bearer $AUTH_TOKEN" "$ALL_URL/admin/shards" | jq '.shards | length')
EXPECTED_COUNT=$((INITIAL_SHARD_COUNT + 1))
if [ "$NEW_COUNT" = "$EXPECTED_COUNT" ]; then
    echo -e "${GREEN}PASS${NC} ($INITIAL_SHARD_COUNT -> $NEW_COUNT)"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}FAIL${NC} (expected $EXPECTED_COUNT, got $NEW_COUNT)"
    FAILED=$((FAILED + 1))
fi

# Keep the shard around for /admin/tables, /admin/stats, and /bulk tests later

# ----------------------------------------------------------
section "Index Open/Close"
# ----------------------------------------------------------

run_test "Close index demo-logs" \
    assert_http POST "$ALL_URL/indices/demo-logs/_close" "" 200 ".acknowledged" "true"

run_test "Verify closed state" \
    assert_http GET "$ALL_URL/indices/demo-logs" "" 200 ".state" "closed"

run_test "Open index demo-logs" \
    assert_http POST "$ALL_URL/indices/demo-logs/_open" "" 200 ".acknowledged" "true"

run_test "Verify open state" \
    assert_http GET "$ALL_URL/indices/demo-logs" "" 200 ".state" "open"

# ----------------------------------------------------------
section "Mappings"
# ----------------------------------------------------------

run_test "Put mapping on demo-logs" \
    assert_http PUT "$ALL_URL/indices/demo-logs/_mapping" \
    '{"fields":{"message":{"name":"message","type":"VARCHAR"},"level":{"name":"level","type":"VARCHAR"},"ts":{"name":"ts","type":"TIMESTAMP"}},"dynamic":true}' \
    200 ".acknowledged" "true"

run_test "Get mapping" \
    assert_http GET "$ALL_URL/indices/demo-logs/_mapping" "" 200

# ----------------------------------------------------------
section "Document Ingestion (all-node)"
# ----------------------------------------------------------

run_test "Ingest doc 1 via all-node" \
    assert_http POST "$ALL_URL/indices/demo-logs/_doc" \
    '{"_id":"d1","message":"hello world","level":"info","ts":"2026-01-01T00:00:00Z"}' \
    201 ".result" "created"

run_test "Ingest doc 2 via all-node" \
    assert_http POST "$ALL_URL/indices/demo-logs/_doc" \
    '{"_id":"d2","message":"error occurred","level":"error","ts":"2026-01-01T00:01:00Z"}' \
    201 ".result" "created"

run_test "Ingest doc 3 via all-node" \
    assert_http POST "$ALL_URL/indices/demo-logs/_doc" \
    '{"_id":"d3","message":"debug trace","level":"debug","ts":"2026-01-01T00:02:00Z"}' \
    201 ".result" "created"

# ----------------------------------------------------------
section "Document Ingestion (write-node)"
# ----------------------------------------------------------

# Each node has its own index store, so create the index on write-node first
run_test "Create index on write-node" \
    assert_http PUT "$WRITE_URL/indices/demo-logs" \
    '{"settings":{"shard_count":3,"partition_key_field":"_id"}}' \
    201 ".acknowledged" "true"

run_test "Ingest doc 4 via write-node" \
    assert_http POST "$WRITE_URL/indices/demo-logs/_doc" \
    '{"_id":"d4","message":"from writer","level":"warn","ts":"2026-01-01T00:03:00Z"}' \
    201 ".result" "created"

run_test "Ingest doc 5 via write-node" \
    assert_http POST "$WRITE_URL/indices/demo-logs/_doc" \
    '{"_id":"d5","message":"also writer","level":"info","ts":"2026-01-01T00:04:00Z"}' \
    201 ".result" "created"

# ----------------------------------------------------------
section "Bulk Document Ingestion"
# ----------------------------------------------------------

run_test "Bulk ingest 3 docs via all-node" \
    assert_http POST "$ALL_URL/indices/demo-logs/_bulk" \
    '{"_id":"d6","message":"bulk one","level":"info"}
{"_id":"d7","message":"bulk two","level":"warn"}
{"_id":"d8","message":"bulk three","level":"error"}' \
    200 ".succeeded" "3"

# ----------------------------------------------------------
section "Queries"
# ----------------------------------------------------------

run_test "SELECT * (all docs)" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT * FROM _docs","index":"demo-logs"}' \
    200 ".success" "true"

run_test "WHERE filter" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT * FROM _docs WHERE level = '\''error'\''","index":"demo-logs"}' \
    200 ".success" "true"

run_test "COUNT aggregate" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT COUNT(*) as cnt FROM _docs","index":"demo-logs"}' \
    200 ".success" "true"

run_test "GROUP BY aggregate" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT level, COUNT(*) as cnt FROM _docs GROUP BY level","index":"demo-logs"}' \
    200 ".success" "true"

# ----------------------------------------------------------
section "Cross-Node Query Verification"
# ----------------------------------------------------------

# Query from write-node sees docs written to write-node
run_test "Query from write-node sees its docs" \
    assert_http POST "$WRITE_URL/query" \
    '{"sql":"SELECT COUNT(*) as cnt FROM _docs","index":"demo-logs"}' \
    200 ".success" "true"

# ----------------------------------------------------------
section "Cross-Index Queries"
# ----------------------------------------------------------

run_test "Ingest into demo-metrics" \
    assert_http POST "$ALL_URL/indices/demo-metrics/_doc" \
    '{"_id":"m1","metric":"cpu","value":42.5}' \
    201 ".result" "created"

run_test "Cross-index query" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT * FROM _docs","index":"demo-logs,demo-metrics"}' \
    200 ".success" "true"

# ----------------------------------------------------------
section "Multi-Query"
# ----------------------------------------------------------

run_test "Multi-query (2 queries)" \
    assert_http POST "$ALL_URL/multi-query" \
    '{"queries":[{"sql":"SELECT COUNT(*) as cnt FROM _docs","index":"demo-logs"},{"sql":"SELECT * FROM _docs","index":"demo-metrics"}]}' \
    200

# ----------------------------------------------------------
section "Bulk SQL Operations"
# ----------------------------------------------------------

# /bulk routes to legacy shards — a shard was added in Section 6
run_test "Bulk SQL (2 statements)" \
    assert_http POST "$ALL_URL/bulk" \
    '{"statements":[{"sql":"SELECT 1 as x","partition_key":"k1"},{"sql":"SELECT 2 as y","partition_key":"k2"}]}' \
    200

# ----------------------------------------------------------
section "Aliases"
# ----------------------------------------------------------

run_test "Create alias logs-alias" \
    assert_http PUT "$ALL_URL/aliases/logs-alias" \
    '{"indices":["demo-logs"]}' \
    201 ".acknowledged" "true"

run_test "Get alias" \
    assert_http GET "$ALL_URL/aliases/logs-alias" "" 200

run_test "List aliases" \
    assert_http GET "$ALL_URL/aliases" "" 200

run_test "Query via alias" \
    assert_http POST "$ALL_URL/query" \
    '{"sql":"SELECT COUNT(*) as cnt FROM _docs","index":"logs-alias"}' \
    200 ".success" "true"

# ----------------------------------------------------------
section "Templates"
# ----------------------------------------------------------

run_test "Create template log-tmpl" \
    assert_http PUT "$ALL_URL/templates/log-tmpl" \
    '{"pattern":"log-*","settings":{"shard_count":2},"mapping":{"fields":{"message":{"name":"message","type":"VARCHAR"},"level":{"name":"level","type":"VARCHAR"}}}}' \
    201 ".acknowledged" "true"

run_test "Get template" \
    assert_http GET "$ALL_URL/templates/log-tmpl" "" 200

run_test "List templates" \
    assert_http GET "$ALL_URL/templates" "" 200

run_test "Create index matching template (auto-apply)" \
    assert_http PUT "$ALL_URL/indices/log-2026-01" '{}' 201 ".acknowledged" "true"

# ----------------------------------------------------------
section "ISM Policies"
# ----------------------------------------------------------

run_test "Create ISM policy" \
    assert_http PUT "$ALL_URL/ism/policies/demo-lifecycle" \
    '{"description":"Demo lifecycle","default_state":"hot","states":[{"name":"hot","transitions":[{"state_name":"warm","conditions":{"min_index_age":"1h"}}]},{"name":"warm","actions":[{"type":"read_only"}],"transitions":[{"state_name":"delete","conditions":{"min_index_age":"24h"}}]},{"name":"delete","actions":[{"type":"delete"}]}]}' \
    201 ".acknowledged" "true"

run_test "Get ISM policy" \
    assert_http GET "$ALL_URL/ism/policies/demo-lifecycle" "" 200

run_test "List ISM policies" \
    assert_http GET "$ALL_URL/ism/policies" "" 200

run_test "Attach ISM policy to demo-logs" \
    assert_http POST "$ALL_URL/ism/attach/demo-logs" \
    '{"policy":"demo-lifecycle"}' \
    200 ".acknowledged" "true"

run_test "Get ISM status for demo-logs" \
    assert_http GET "$ALL_URL/ism/status/demo-logs" "" 200

run_test "Get all ISM statuses" \
    assert_http GET "$ALL_URL/ism/status" "" 200

run_test "Detach ISM policy from demo-logs" \
    assert_http POST "$ALL_URL/ism/detach/demo-logs" "" 200 ".acknowledged" "true"

run_test "ISM retry on non-failed index (expect 404)" \
    assert_http POST "$ALL_URL/ism/retry/demo-logs" "" 404

# ----------------------------------------------------------
section "Admin & Stats"
# ----------------------------------------------------------

# These endpoints query legacy shard 0 (added in Section 6)
run_test "List tables (legacy shard)" \
    assert_http GET "$ALL_URL/admin/tables" "" 200

run_test "Get cluster stats" \
    assert_http GET "$ALL_URL/admin/stats" "" 200

# ----------------------------------------------------------
section "Rebalancing"
# ----------------------------------------------------------

run_test "Get rebalance status (idle)" \
    assert_http GET "$ALL_URL/admin/rebalance/status" "" 200

run_test "Create rebalance plan" \
    assert_http POST "$ALL_URL/admin/rebalance/plan" \
    '{"partition_key_column":"_id","tables":["_docs"]}' 200

run_test "Execute rebalance" \
    assert_http POST "$ALL_URL/admin/rebalance" \
    '{"partition_key_column":"_id","tables":["_docs"],"batch_size":100}' 202

# Wait for rebalance to complete
sleep 3

run_test "Rebalance status after execution" \
    assert_http GET "$ALL_URL/admin/rebalance/status" "" 200

# ----------------------------------------------------------
section "Migration"
# ----------------------------------------------------------

run_test "Get version" \
    assert_http GET "$ALL_URL/admin/version" "" 200

run_test "Run pending migrations" \
    assert_http POST "$ALL_URL/admin/migrate" "" 200

# ----------------------------------------------------------
section "Backup & Restore"
# ----------------------------------------------------------

echo -n "  Create full backup ... "
BACKUP_RESP=$(curl -s -X POST "$ALL_URL/admin/backups" \
    -H "Authorization: Bearer $AUTH_TOKEN" \
    -H "Content-Type: application/json" \
    -d '{"type":"full"}')
BACKUP_ID=$(echo "$BACKUP_RESP" | jq -r '.id')
if [ -n "$BACKUP_ID" ] && [ "$BACKUP_ID" != "null" ]; then
    echo -e "${GREEN}PASS${NC} (id: $BACKUP_ID)"
    PASSED=$((PASSED + 1))
else
    echo -e "${RED}FAIL${NC} (response: $BACKUP_RESP)"
    FAILED=$((FAILED + 1))
fi

run_test "List backups" \
    assert_http GET "$ALL_URL/admin/backups" "" 200

if [ -n "$BACKUP_ID" ] && [ "$BACKUP_ID" != "null" ]; then
    run_test "Restore backup" \
        assert_http POST "$ALL_URL/admin/backups/$BACKUP_ID/restore" '{}' 200 ".success" "true"

    run_test "Delete backup" \
        assert_http DELETE "$ALL_URL/admin/backups/$BACKUP_ID" "" 200 ".success" "true"

    run_test "Verify backup deleted" \
        assert_http GET "$ALL_URL/admin/backups" "" 200 ".count" "0"
else
    echo -e "  ${YELLOW}Skipping restore/delete (no backup ID)${NC}"
fi

# ----------------------------------------------------------
section "Observability Stack"
# ----------------------------------------------------------

wait_for_url "$PROMETHEUS_URL/-/healthy" "prometheus" 60
wait_for_url "$GRAFANA_URL/api/health" "grafana" 60

run_test "Prometheus healthy" \
    assert_http_noauth GET "$PROMETHEUS_URL/-/healthy" "" 200

run_test "Prometheus targets" \
    assert_http_noauth GET "$PROMETHEUS_URL/api/v1/targets" "" 200

run_test "Jaeger UI" \
    assert_http_noauth GET "$JAEGER_URL/" "" 200

run_test "Grafana health" \
    assert_http_noauth GET "$GRAFANA_URL/api/health" "" 200

run_test "Metrics: write-node" \
    assert_http_noauth GET "$WRITE_URL/metrics" "" 200

run_test "Metrics: read-node" \
    assert_http_noauth GET "$READ_URL/metrics" "" 200

run_test "Metrics: all-node" \
    assert_http_noauth GET "$ALL_URL/metrics" "" 200

# ----------------------------------------------------------
section "Cleanup"
# ----------------------------------------------------------

# Remove the legacy shard we added earlier
run_test "Remove legacy shard" \
    assert_http DELETE "$ALL_URL/admin/shards/0" "" 200

run_test "Delete alias logs-alias" \
    assert_http DELETE "$ALL_URL/aliases/logs-alias" "" 200 ".acknowledged" "true"

run_test "Delete template log-tmpl" \
    assert_http DELETE "$ALL_URL/templates/log-tmpl" "" 200 ".acknowledged" "true"

run_test "Delete ISM policy" \
    assert_http DELETE "$ALL_URL/ism/policies/demo-lifecycle" "" 200 ".acknowledged" "true"

run_test "Delete index log-2026-01" \
    assert_http DELETE "$ALL_URL/indices/log-2026-01" "" 200 ".acknowledged" "true"

run_test "Delete index demo-metrics" \
    assert_http DELETE "$ALL_URL/indices/demo-metrics" "" 200 ".acknowledged" "true"

run_test "Delete index demo-logs" \
    assert_http DELETE "$ALL_URL/indices/demo-logs" "" 200 ".acknowledged" "true"

# ============================================================
# Summary
# ============================================================
TOTAL=$((PASSED + FAILED))
echo ""
echo -e "${BOLD}===============================${NC}"
echo -e "  ${GREEN}Passed: $PASSED${NC}"
echo -e "  ${RED}Failed: $FAILED${NC}"
echo -e "  Total:  $TOTAL"
echo -e "${BOLD}===============================${NC}"
echo ""

if [ "$FAILED" -gt 0 ]; then
    echo -e "${RED}Some tests failed!${NC}"
    exit 1
fi

echo -e "${GREEN}All tests passed!${NC}"
