package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestISMPolicyCRUD(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create policy
	policy := `{
		"name": "test-policy",
		"description": "test",
		"default_state": "hot",
		"states": [
			{"name": "hot", "transitions": [{"state_name": "warm"}]},
			{"name": "warm"}
		]
	}`
	req := httptest.NewRequest("PUT", "/ism/policies/test-policy", bytes.NewBufferString(policy))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Get policy
	req = httptest.NewRequest("GET", "/ism/policies/test-policy", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	json.NewDecoder(w.Body).Decode(&got)
	if got["name"] != "test-policy" {
		t.Errorf("expected name 'test-policy', got %v", got["name"])
	}

	// List policies
	req = httptest.NewRequest("GET", "/ism/policies", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", w.Code)
	}
	var listResp map[string]any
	json.NewDecoder(w.Body).Decode(&listResp)
	policies := listResp["policies"].([]any)
	if len(policies) != 1 {
		t.Errorf("expected 1 policy, got %d", len(policies))
	}

	// Delete policy
	req = httptest.NewRequest("DELETE", "/ism/policies/test-policy", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify gone
	req = httptest.NewRequest("GET", "/ism/policies/test-policy", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", w.Code)
	}
}

func TestISMPolicyInvalid(t *testing.T) {
	srv := setupServerWithInit(t)

	// Missing default_state
	policy := `{"name": "bad", "states": [{"name": "s1"}]}`
	req := httptest.NewRequest("PUT", "/ism/policies/bad", bytes.NewBufferString(policy))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestISMAttachDetach(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create policy
	policy := `{
		"default_state": "hot",
		"states": [{"name": "hot"}]
	}`
	req := httptest.NewRequest("PUT", "/ism/policies/attach-pol", bytes.NewBufferString(policy))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create policy: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Create index
	req = httptest.NewRequest("PUT", "/indices/ism-test-idx", bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create index: expected 201, got %d", w.Code)
	}

	// Attach
	req = httptest.NewRequest("POST", "/ism/attach/ism-test-idx", bytes.NewBufferString(`{"policy":"attach-pol"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("attach: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check status
	req = httptest.NewRequest("GET", "/ism/status/ism-test-idx", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var status map[string]any
	json.NewDecoder(w.Body).Decode(&status)
	if status["policy_name"] != "attach-pol" {
		t.Errorf("expected policy_name 'attach-pol', got %v", status["policy_name"])
	}

	// Detach
	req = httptest.NewRequest("POST", "/ism/detach/ism-test-idx", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("detach: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Status should be gone
	req = httptest.NewRequest("GET", "/ism/status/ism-test-idx", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 after detach, got %d", w.Code)
	}
}

func TestISMStatusAll(t *testing.T) {
	srv := setupServerWithInit(t)

	// No managed indices yet
	req := httptest.NewRequest("GET", "/ism/status", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	statuses := resp["statuses"].([]any)
	if len(statuses) != 0 {
		t.Errorf("expected 0 statuses, got %d", len(statuses))
	}
}

func TestISMRetryNotFound(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("POST", "/ism/retry/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestISMDeleteNotFound(t *testing.T) {
	srv := setupServerWithInit(t)

	req := httptest.NewRequest("DELETE", "/ism/policies/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestISMYAMLCreate(t *testing.T) {
	srv := setupServerWithInit(t)

	yamlPolicy := `
name: yaml-policy
default_state: active
states:
  - name: active
    transitions:
      - state_name: done
  - name: done
`
	req := httptest.NewRequest("PUT", "/ism/policies/yaml-policy", bytes.NewBufferString(yamlPolicy))
	req.Header.Set("Content-Type", "application/yaml")
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("yaml create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it was created
	req = httptest.NewRequest("GET", "/ism/policies/yaml-policy", nil)
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}
}
