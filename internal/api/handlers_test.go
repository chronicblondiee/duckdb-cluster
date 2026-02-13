package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/cluster"
	"github.com/chronicblondiee/duckdb-cluster/internal/config"
)

func setupServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	clusterCfg := &cluster.Config{
		DataDir:   dir,
		NumShards: 3,
	}
	c, err := cluster.NewCluster(clusterCfg)
	if err != nil {
		t.Fatalf("NewCluster: %v", err)
	}
	if err := c.Start(); err != nil {
		// Start expects existing shards, so init first
		c2, _ := cluster.NewCluster(&cluster.Config{
			DataDir:   dir,
			NumShards: 3,
		})
		if err := c2.Init(); err != nil {
			t.Fatalf("Init: %v", err)
		}
		c2.Shutdown()
		if err := c.Start(); err != nil {
			t.Fatalf("Start: %v", err)
		}
	}
	t.Cleanup(func() { c.Shutdown() })
	
	// Create config with security disabled for tests
	cfg := config.Default()
	cfg.Security.Authentication.Enabled = false
	cfg.Security.RateLimit.Enabled = false
	
	srv, err := NewServer(c, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func setupServerWithInit(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	clusterCfg := &cluster.Config{
		DataDir:   dir,
		NumShards: 3,
	}

	// Init to create shard files
	c, _ := cluster.NewCluster(clusterCfg)
	if err := c.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Start fresh cluster
	c2, _ := cluster.NewCluster(clusterCfg)
	if err := c2.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { c2.Shutdown() })
	
	// Create config with security disabled for tests
	cfg := config.Default()
	cfg.Security.Authentication.Enabled = false
	cfg.Security.RateLimit.Enabled = false
	
	srv, err := NewServer(c2, cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

func TestHealthEndpoint(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "healthy" {
		t.Errorf("expected healthy, got %v", resp["status"])
	}
}

func TestQueryDDL(t *testing.T) {
	srv := setupServerWithInit(t)

	body := `{"sql": "CREATE TABLE test (id INTEGER, name VARCHAR)"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
}

func TestQueryInsertAndSelect(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create table
	doQuery(t, srv, `{"sql": "CREATE TABLE users (id INTEGER, name VARCHAR)"}`)

	// Insert
	doQuery(t, srv, `{"sql": "INSERT INTO users VALUES (1, 'alice')", "partition_key": "1"}`)
	doQuery(t, srv, `{"sql": "INSERT INTO users VALUES (2, 'bob')", "partition_key": "2"}`)

	// Select
	body := `{"sql": "SELECT * FROM users"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("SELECT failed: %s", resp.Error)
	}
	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(resp.Rows))
	}
}

func TestListShards(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("GET", "/admin/shards", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string][]shardInfo
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp["shards"]) != 3 {
		t.Errorf("expected 3 shards, got %d", len(resp["shards"]))
	}
}

func doQuery(t *testing.T, srv *Server, body string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("query failed (%d): %s", w.Code, w.Body.String())
	}
}

// Phase 2 Tests

func TestPagination(t *testing.T) {
	srv := setupServerWithInit(t)
	
	// Create table and insert 20 rows
	doQuery(t, srv, `{"sql": "CREATE TABLE items (id INTEGER, value VARCHAR)"}`)
	for i := 0; i < 20; i++ {
		doQuery(t, srv, fmt.Sprintf(`{"sql": "INSERT INTO items VALUES (%d, 'item%d')", "partition_key": "%d"}`, i, i, i))
	}
	
	// Test limit
	body := `{"sql": "SELECT * FROM items ORDER BY id", "limit": 5}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 5 {
		t.Errorf("expected 5 rows with limit, got %d", len(resp.Rows))
	}
	
	// Test offset
	body = `{"sql": "SELECT * FROM items ORDER BY id", "offset": 10, "limit": 5}`
	req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Rows) != 5 {
		t.Errorf("expected 5 rows with offset+limit, got %d", len(resp.Rows))
	}
	
	// Test offset beyond results
	body = `{"sql": "SELECT * FROM items", "offset": 100}`
	req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Rows) != 0 {
		t.Errorf("expected 0 rows with large offset, got %d", len(resp.Rows))
	}
}

func TestBulkInsert(t *testing.T) {
	srv := setupServerWithInit(t)
	
	// Create table
	doQuery(t, srv, `{"sql": "CREATE TABLE products (id INTEGER, name VARCHAR)"}`)
	
	// Bulk insert
	bulkReq := bulkRequest{
		Statements: []bulkStatement{
			{SQL: "INSERT INTO products VALUES (1, 'widget')", PartitionKey: "1"},
			{SQL: "INSERT INTO products VALUES (2, 'gadget')", PartitionKey: "2"},
			{SQL: "INSERT INTO products VALUES (3, 'doohickey')", PartitionKey: "3"},
			{SQL: "INSERT INTO products VALUES (4, 'thingamajig')", PartitionKey: "4"},
			{SQL: "INSERT INTO products VALUES (5, 'whatsit')", PartitionKey: "5"},
		},
	}
	
	body, _ := json.Marshal(bulkReq)
	req := httptest.NewRequest("POST", "/bulk", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	
	var resp bulkResponse
	json.NewDecoder(w.Body).Decode(&resp)
	
	if resp.Succeeded != 5 {
		t.Errorf("expected 5 succeeded, got %d", resp.Succeeded)
	}
	if resp.Failed != 0 {
		t.Errorf("expected 0 failed, got %d", resp.Failed)
	}
	if len(resp.Results) != 5 {
		t.Errorf("expected 5 results, got %d", len(resp.Results))
	}
	
	// Verify inserts
	queryReq := httptest.NewRequest("POST", "/query", bytes.NewBufferString(`{"sql": "SELECT COUNT(*) as cnt FROM products"}`))
	queryW := httptest.NewRecorder()
	srv.mux.ServeHTTP(queryW, queryReq)
	
	var queryResp queryResponse
	json.NewDecoder(queryW.Body).Decode(&queryResp)
	if len(queryResp.Rows) != 1 {
		t.Fatalf("expected 1 row in count, got %d", len(queryResp.Rows))
	}
}

func TestBulkMixedTables(t *testing.T) {
	srv := setupServerWithInit(t)
	
	// Create tables
	doQuery(t, srv, `{"sql": "CREATE TABLE users (id INTEGER, name VARCHAR)"}`)
	doQuery(t, srv, `{"sql": "CREATE TABLE orders (id INTEGER, user_id INTEGER)"}`)
	
	// Bulk insert into different tables
	bulkReq := bulkRequest{
		Statements: []bulkStatement{
			{SQL: "INSERT INTO users VALUES (1, 'alice')", PartitionKey: "user1"},
			{SQL: "INSERT INTO orders VALUES (101, 1)", PartitionKey: "order101"},
			{SQL: "INSERT INTO users VALUES (2, 'bob')", PartitionKey: "user2"},
			{SQL: "INSERT INTO orders VALUES (102, 2)", PartitionKey: "order102"},
		},
	}
	
	body, _ := json.Marshal(bulkReq)
	req := httptest.NewRequest("POST", "/bulk", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	var resp bulkResponse
	json.NewDecoder(w.Body).Decode(&resp)
	
	if resp.Succeeded != 4 {
		t.Errorf("expected 4 succeeded, got %d", resp.Succeeded)
	}
}

func TestMultiQuery(t *testing.T) {
	srv := setupServerWithInit(t)
	
	// Setup data
	doQuery(t, srv, `{"sql": "CREATE TABLE metrics (id INTEGER, value INTEGER)"}`)
	for i := 1; i <= 10; i++ {
		doQuery(t, srv, fmt.Sprintf(`{"sql": "INSERT INTO metrics VALUES (%d, %d)", "partition_key": "%d"}`, i, i*10, i))
	}
	
	// Multi-query
	multiReq := multiQueryRequest{
		Queries: []queryRequest{
			{SQL: "SELECT COUNT(*) as cnt FROM metrics"},
			{SQL: "SELECT SUM(value) as total FROM metrics"},
			{SQL: "SELECT AVG(value) as avg FROM metrics"},
			{SQL: "SELECT MIN(value) as min FROM metrics"},
			{SQL: "SELECT MAX(value) as max FROM metrics"},
		},
	}
	
	body, _ := json.Marshal(multiReq)
	req := httptest.NewRequest("POST", "/multi-query", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	
	var resp multiQueryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	
	if len(resp.Results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(resp.Results))
	}
	
	// Check all queries succeeded
	for i, result := range resp.Results {
		if !result.Success {
			t.Errorf("query %d failed: %s", i, result.Error)
		}
		if len(result.Rows) != 1 {
			t.Errorf("query %d: expected 1 row, got %d", i, len(result.Rows))
		}
	}
	
	if resp.TookMs <= 0 {
		t.Errorf("expected positive took_ms, got %d", resp.TookMs)
	}
}

func TestMultiQueryPartialFailure(t *testing.T) {
	srv := setupServerWithInit(t)
	
	doQuery(t, srv, `{"sql": "CREATE TABLE test (id INTEGER)"}`)
	
	// Multi-query with one invalid query
	multiReq := multiQueryRequest{
		Queries: []queryRequest{
			{SQL: "SELECT COUNT(*) FROM test"},
			{SQL: "SELECT * FROM nonexistent_table"}, // This should fail
			{SQL: "SELECT 1 as num"},
		},
	}
	
	body, _ := json.Marshal(multiReq)
	req := httptest.NewRequest("POST", "/multi-query", bytes.NewBuffer(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	var resp multiQueryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	
	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Results))
	}
	
	// First and third should succeed, second should fail
	if !resp.Results[0].Success {
		t.Error("expected first query to succeed")
	}
	if resp.Results[1].Success {
		t.Error("expected second query to fail")
	}
	if !resp.Results[2].Success {
		t.Error("expected third query to succeed")
	}
}

func TestListTables(t *testing.T) {
	srv := setupServerWithInit(t)
	
	// Create multiple tables
	doQuery(t, srv, `{"sql": "CREATE TABLE users (id INTEGER)"}`)
	doQuery(t, srv, `{"sql": "CREATE TABLE products (id INTEGER)"}`)
	doQuery(t, srv, `{"sql": "CREATE TABLE orders (id INTEGER)"}`)
	
	req := httptest.NewRequest("GET", "/admin/tables", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	
	var resp map[string][]string
	json.NewDecoder(w.Body).Decode(&resp)
	
	tables := resp["tables"]
	if len(tables) != 3 {
		t.Errorf("expected 3 tables, got %d", len(tables))
	}
	
	// Check expected tables exist
	expectedTables := map[string]bool{"users": false, "products": false, "orders": false}
	for _, table := range tables {
		if _, exists := expectedTables[table]; exists {
			expectedTables[table] = true
		}
	}
	
	for table, found := range expectedTables {
		if !found {
			t.Errorf("expected table %s not found", table)
		}
	}
}

func TestTableSchema(t *testing.T) {
	srv := setupServerWithInit(t)
	
	doQuery(t, srv, `{"sql": "CREATE TABLE employees (id INTEGER, name VARCHAR, age INTEGER, salary DECIMAL)"}`)
	
	req := httptest.NewRequest("GET", "/admin/tables/employees", nil)
	req.SetPathValue("name", "employees")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	
	if resp["name"] != "employees" {
		t.Errorf("expected table name 'employees', got %v", resp["name"])
	}
	
	columns := resp["columns"].([]any)
	if len(columns) != 4 {
		t.Errorf("expected 4 columns, got %d", len(columns))
	}
}

func TestTableSchemaNotFound(t *testing.T) {
	srv := setupServerWithInit(t)
	
	req := httptest.NewRequest("GET", "/admin/tables/nonexistent", nil)
	req.SetPathValue("name", "nonexistent")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestStats(t *testing.T) {
	srv := setupServerWithInit(t)
	
	// Create table and add data
	doQuery(t, srv, `{"sql": "CREATE TABLE data (id INTEGER, value VARCHAR)"}`)
	for i := 0; i < 30; i++ {
		doQuery(t, srv, fmt.Sprintf(`{"sql": "INSERT INTO data VALUES (%d, 'value%d')", "partition_key": "%d"}`, i, i, i))
	}
	
	req := httptest.NewRequest("GET", "/admin/stats", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	
	cluster := resp["cluster"].(map[string]any)
	if cluster["total_shards"].(float64) != 3 {
		t.Errorf("expected 3 shards, got %v", cluster["total_shards"])
	}
	if cluster["total_tables"].(float64) != 1 {
		t.Errorf("expected 1 table, got %v", cluster["total_tables"])
	}
	if cluster["total_rows"].(float64) != 30 {
		t.Errorf("expected 30 total rows, got %v", cluster["total_rows"])
	}
	
	shards := resp["shards"].([]any)
	if len(shards) != 3 {
		t.Errorf("expected 3 shard stats, got %d", len(shards))
	}
}

