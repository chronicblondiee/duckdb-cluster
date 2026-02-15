package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
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

func TestTemplateMappingAutoApplied(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create a template with a mapping and pattern "logs-*"
	tmplBody := `{
		"pattern": "logs-*",
		"priority": 10,
		"settings": {"shard_count": 2},
		"mapping": {
			"fields": {
				"timestamp": {"name": "timestamp", "type": "TIMESTAMP"},
				"level":     {"name": "level", "type": "VARCHAR"},
				"message":   {"name": "message", "type": "VARCHAR"}
			},
			"dynamic": false
		}
	}`
	req := httptest.NewRequest("PUT", "/templates/log-tmpl", bytes.NewBufferString(tmplBody))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code/100 != 2 {
		t.Fatalf("create template: expected 2xx, got %d: %s", w.Code, w.Body.String())
	}

	// Create an index matching the template pattern
	req = httptest.NewRequest("PUT", "/indices/logs-2026", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create index: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get the index mapping and verify template fields were applied
	req = httptest.NewRequest("GET", "/indices/logs-2026/_mapping", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get mapping: expected 200, got %d", w.Code)
	}

	var mapping index.Mapping
	json.NewDecoder(w.Body).Decode(&mapping)

	for _, field := range []string{"timestamp", "level", "message"} {
		if _, ok := mapping.Fields[field]; !ok {
			t.Errorf("expected field %q from template mapping, not found", field)
		}
	}
	if mapping.Dynamic {
		t.Error("expected dynamic=false from template, got true")
	}
}

func TestTemplateMappingWithExplicitOverride(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create template with mapping
	tmplBody := `{
		"pattern": "metrics-*",
		"priority": 5,
		"settings": {"shard_count": 1},
		"mapping": {
			"fields": {
				"host":  {"name": "host", "type": "VARCHAR"},
				"value": {"name": "value", "type": "DOUBLE"},
				"tag":   {"name": "tag", "type": "VARCHAR"}
			},
			"dynamic": true
		}
	}`
	req := httptest.NewRequest("PUT", "/templates/metrics-tmpl", bytes.NewBufferString(tmplBody))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code/100 != 2 {
		t.Fatalf("create template: expected 2xx, got %d: %s", w.Code, w.Body.String())
	}

	// Create index with explicit mapping that overrides "value" and adds "region"
	idxBody := `{
		"mappings": {
			"fields": {
				"value":  {"name": "value", "type": "BIGINT"},
				"region": {"name": "region", "type": "VARCHAR"}
			},
			"dynamic": false
		}
	}`
	req = httptest.NewRequest("PUT", "/indices/metrics-cpu", bytes.NewBufferString(idxBody))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create index: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify merged mapping
	req = httptest.NewRequest("GET", "/indices/metrics-cpu/_mapping", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var mapping index.Mapping
	json.NewDecoder(w.Body).Decode(&mapping)

	// Template-only field preserved
	if _, ok := mapping.Fields["host"]; !ok {
		t.Error("expected template field 'host' preserved")
	}
	if _, ok := mapping.Fields["tag"]; !ok {
		t.Error("expected template field 'tag' preserved")
	}
	// Explicit field overrides template
	if f, ok := mapping.Fields["value"]; !ok {
		t.Error("expected field 'value'")
	} else if f.Type != "BIGINT" {
		t.Errorf("expected 'value' type BIGINT (explicit override), got %s", f.Type)
	}
	// Explicit-only field added
	if _, ok := mapping.Fields["region"]; !ok {
		t.Error("expected explicit field 'region' added")
	}
	// Dynamic overridden to false
	if mapping.Dynamic {
		t.Error("expected dynamic=false from explicit override, got true")
	}
}

func TestNoTemplateMatchDefaultMapping(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create template that won't match
	tmplBody := `{
		"pattern": "logs-*",
		"priority": 1,
		"settings": {"shard_count": 2},
		"mapping": {
			"fields": {"level": {"name": "level", "type": "VARCHAR"}},
			"dynamic": false
		}
	}`
	req := httptest.NewRequest("PUT", "/templates/log-tmpl", bytes.NewBufferString(tmplBody))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code/100 != 2 {
		t.Fatalf("create template: expected 2xx, got %d", w.Code)
	}

	// Create index that does NOT match "logs-*"
	req = httptest.NewRequest("PUT", "/indices/events-2026", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create index: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify default empty dynamic mapping
	req = httptest.NewRequest("GET", "/indices/events-2026/_mapping", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var mapping index.Mapping
	json.NewDecoder(w.Body).Decode(&mapping)

	if len(mapping.Fields) != 0 {
		t.Errorf("expected empty fields for non-matching index, got %d fields", len(mapping.Fields))
	}
	if !mapping.Dynamic {
		t.Error("expected dynamic=true for default mapping")
	}
}

func TestTemplateMappingCloneIsolation(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create template with mapping
	tmplBody := `{
		"pattern": "iso-*",
		"priority": 1,
		"settings": {"shard_count": 1},
		"mapping": {
			"fields": {"name": {"name": "name", "type": "VARCHAR"}},
			"dynamic": true
		}
	}`
	req := httptest.NewRequest("PUT", "/templates/iso-tmpl", bytes.NewBufferString(tmplBody))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code/100 != 2 {
		t.Fatalf("create template: expected 2xx, got %d", w.Code)
	}

	// Create two indices matching the template
	for _, name := range []string{"iso-a", "iso-b"} {
		req = httptest.NewRequest("PUT", "/indices/"+name, nil)
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d: %s", name, w.Code, w.Body.String())
		}
	}

	// Modify iso-a's mapping by adding a field via PUT _mapping
	newMapping := `{"fields":{"name":{"name":"name","type":"VARCHAR"},"extra":{"name":"extra","type":"BIGINT"}},"dynamic":true}`
	req = httptest.NewRequest("PUT", "/indices/iso-a/_mapping", bytes.NewBufferString(newMapping))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("put mapping iso-a: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify iso-b still has only the original template field
	req = httptest.NewRequest("GET", "/indices/iso-b/_mapping", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var mapping index.Mapping
	json.NewDecoder(w.Body).Decode(&mapping)

	if _, ok := mapping.Fields["extra"]; ok {
		t.Error("iso-b should not have 'extra' field — template mapping was not properly cloned")
	}
	if _, ok := mapping.Fields["name"]; !ok {
		t.Error("iso-b should have 'name' field from template")
	}
}
