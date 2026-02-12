package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brown/duckdb-cluster/internal/cluster"
)

func setupServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &cluster.Config{
		DataDir:   dir,
		NumShards: 3,
	}
	c, err := cluster.NewCluster(cfg)
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
	return NewServer(c)
}

func setupServerWithInit(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := &cluster.Config{
		DataDir:   dir,
		NumShards: 3,
	}

	// Init to create shard files
	c, _ := cluster.NewCluster(cfg)
	if err := c.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Start fresh cluster
	c2, _ := cluster.NewCluster(cfg)
	if err := c2.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { c2.Shutdown() })
	return NewServer(c2)
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
