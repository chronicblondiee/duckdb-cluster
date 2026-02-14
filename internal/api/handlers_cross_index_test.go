package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCrossIndexQueryCommaSeparated(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create two indices with tables
	for _, name := range []string{"idx-a", "idx-b"} {
		req := httptest.NewRequest("PUT", "/indices/"+name, bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d: %s", name, w.Code, w.Body.String())
		}

		// Create table and insert data
		body := `{"sql": "CREATE TABLE items (id INTEGER, source VARCHAR)", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("create table on %s: %d: %s", name, w.Code, w.Body.String())
		}

		body = `{"sql": "INSERT INTO items VALUES (1, '` + name + `')", "partition_key": "1", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("insert on %s: %d: %s", name, w.Code, w.Body.String())
		}
	}

	// Cross-index query
	body := `{"sql": "SELECT * FROM items", "index": "idx-a,idx-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("cross-index query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows from cross-index query, got %d", len(resp.Rows))
	}
	if resp.ShardID != -1 {
		t.Errorf("expected shard_id -1 for cross-index, got %d", resp.ShardID)
	}
}

func TestCrossIndexQueryWildcard(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create indices matching a pattern
	for _, name := range []string{"logs-a", "logs-b"} {
		req := httptest.NewRequest("PUT", "/indices/"+name, bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d", name, w.Code)
		}

		body := `{"sql": "CREATE TABLE data (id INTEGER)", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)

		body = `{"sql": "INSERT INTO data VALUES (1)", "partition_key": "1", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
	}

	// Wildcard query
	body := `{"sql": "SELECT * FROM data", "index": "logs-*"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("wildcard query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows from wildcard query, got %d", len(resp.Rows))
	}
}

func TestCrossIndexWriteRejected(t *testing.T) {
	srv := setupServerWithInit(t)

	body := `{"sql": "INSERT INTO t VALUES (1)", "index": "idx-a,idx-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-index write, got %d", w.Code)
	}
}
