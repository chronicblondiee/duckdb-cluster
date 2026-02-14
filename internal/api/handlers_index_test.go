package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateIndex(t *testing.T) {
	srv := setupServerWithInit(t)

	body := `{"settings":{"shard_count":2}}`
	req := httptest.NewRequest("PUT", "/indices/logs", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["index"] != "logs" {
		t.Errorf("expected index name 'logs', got %v", resp["index"])
	}
	if resp["shards"].(float64) != 2 {
		t.Errorf("expected 2 shards, got %v", resp["shards"])
	}
}

func TestCreateIndexDuplicate(t *testing.T) {
	srv := setupServerWithInit(t)

	body := `{"settings":{"shard_count":2}}`
	req := httptest.NewRequest("PUT", "/indices/myidx", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first create failed: %d", w.Code)
	}

	// Duplicate
	req = httptest.NewRequest("PUT", "/indices/myidx", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestCreateIndexInvalidName(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("PUT", "/indices/INVALID!", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListIndices(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create two indices
	for _, name := range []string{"idx-a", "idx-b"} {
		req := httptest.NewRequest("PUT", "/indices/"+name, bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d", name, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/indices", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	indices := resp["indices"].([]any)
	// _default + idx-a + idx-b
	if len(indices) < 2 {
		t.Errorf("expected at least 2 indices, got %d", len(indices))
	}
}

func TestGetIndex(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create
	req := httptest.NewRequest("PUT", "/indices/test-get", bytes.NewBufferString(`{"settings":{"shard_count":2}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Get
	req = httptest.NewRequest("GET", "/indices/test-get", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["name"] != "test-get" {
		t.Errorf("expected name 'test-get', got %v", resp["name"])
	}
	if resp["state"] != "open" {
		t.Errorf("expected state 'open', got %v", resp["state"])
	}
}

func TestGetIndexNotFound(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("GET", "/indices/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestDeleteIndex(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create
	req := httptest.NewRequest("PUT", "/indices/to-delete", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Delete
	req = httptest.NewRequest("DELETE", "/indices/to-delete", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify gone
	req = httptest.NewRequest("GET", "/indices/to-delete", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestDeleteDefaultIndex(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("DELETE", "/indices/_default", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestCloseOpenIndex(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create
	req := httptest.NewRequest("PUT", "/indices/lifecycle", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Close
	req = httptest.NewRequest("POST", "/indices/lifecycle/_close", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("close: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify closed
	req = httptest.NewRequest("GET", "/indices/lifecycle", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["state"] != "closed" {
		t.Errorf("expected state 'closed', got %v", resp["state"])
	}

	// Open
	req = httptest.NewRequest("POST", "/indices/lifecycle/_open", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("open: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify open
	req = httptest.NewRequest("GET", "/indices/lifecycle", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["state"] != "open" {
		t.Errorf("expected state 'open', got %v", resp["state"])
	}
}

func TestPutGetMapping(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create index
	req := httptest.NewRequest("PUT", "/indices/mapped", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Put mapping
	mapping := `{"fields":{"name":{"name":"name","type":"VARCHAR"},"age":{"name":"age","type":"INTEGER"}},"dynamic":false}`
	req = httptest.NewRequest("PUT", "/indices/mapped/_mapping", bytes.NewBufferString(mapping))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put mapping: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Get mapping
	req = httptest.NewRequest("GET", "/indices/mapped/_mapping", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get mapping: expected 200, got %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	fields := resp["fields"].(map[string]any)
	if _, ok := fields["name"]; !ok {
		t.Error("expected 'name' field in mapping")
	}
	if _, ok := fields["age"]; !ok {
		t.Error("expected 'age' field in mapping")
	}
}

func TestIndexDoc(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create index
	req := httptest.NewRequest("PUT", "/indices/docs-test", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Index a document
	doc := `{"_id":"doc1","title":"Hello","count":42}`
	req = httptest.NewRequest("POST", "/indices/docs-test/_doc", bytes.NewBufferString(doc))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("index doc: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["result"] != "created" {
		t.Errorf("expected result 'created', got %v", resp["result"])
	}
}

func TestBulkDocs(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create index
	req := httptest.NewRequest("PUT", "/indices/bulk-test", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Bulk index
	docs := `{"_id":"d1","name":"alice"}
{"_id":"d2","name":"bob"}
{"_id":"d3","name":"charlie"}`
	req = httptest.NewRequest("POST", "/indices/bulk-test/_bulk", bytes.NewBufferString(docs))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("bulk: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["succeeded"].(float64) != 3 {
		t.Errorf("expected 3 succeeded, got %v", resp["succeeded"])
	}
}

func TestQueryWithIndex(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create index and table within it
	req := httptest.NewRequest("PUT", "/indices/query-idx", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	// Create table and insert via query endpoint with index field
	body := `{"sql": "CREATE TABLE events (id INTEGER, msg VARCHAR)", "index": "query-idx"}`
	req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create table: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Insert
	body = `{"sql": "INSERT INTO events VALUES (1, 'hello')", "partition_key": "1", "index": "query-idx"}`
	req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("insert: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Select
	body = `{"sql": "SELECT * FROM events", "index": "query-idx"}`
	req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("select failed: %s", resp.Error)
	}
	if len(resp.Rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(resp.Rows))
	}
}
