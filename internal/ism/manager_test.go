package ism

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
)

func setupTestManager(t *testing.T) (*Manager, *index.Registry) {
	t.Helper()
	dir := t.TempDir()

	reg := index.NewRegistry(dir)
	_, err := reg.Create("test-index", index.Settings{ShardCount: 1})
	if err != nil {
		t.Fatalf("create test index: %v", err)
	}
	_, err = reg.Create("logs-2025-01", index.Settings{ShardCount: 1})
	if err != nil {
		t.Fatalf("create logs index: %v", err)
	}

	mgr := NewManager(dir, reg)
	return mgr, reg
}

func testPolicy() *Policy {
	return &Policy{
		Name:         "test-policy",
		Description:  "test",
		DefaultState: "hot",
		States: []PolicyState{
			{
				Name: "hot",
				Transitions: []Transition{
					{StateName: "delete", Conditions: &TransitionConditions{
						MinIndexAge: &Duration{Duration: 7 * 24 * time.Hour},
					}},
				},
			},
			{
				Name:    "delete",
				Actions: []Action{{Type: ActionDelete}},
			},
		},
		ISMTemplate: []ISMTemplate{
			{Pattern: "logs-*", Priority: 100},
		},
	}
}

func TestManagerPutGet(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := testPolicy()
	if err := mgr.PutPolicy(p); err != nil {
		t.Fatalf("PutPolicy: %v", err)
	}

	got, err := mgr.GetPolicy("test-policy")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if got.Version != 1 {
		t.Errorf("expected version 1, got %d", got.Version)
	}
	if got.LastUpdated.IsZero() {
		t.Error("expected LastUpdated to be set")
	}
}

func TestManagerPutVersionIncrement(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p := testPolicy()
	mgr.PutPolicy(p)

	p2 := testPolicy()
	p2.Description = "updated"
	mgr.PutPolicy(p2)

	got, _ := mgr.GetPolicy("test-policy")
	if got.Version != 2 {
		t.Errorf("expected version 2 after update, got %d", got.Version)
	}
}

func TestManagerDelete(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())

	if err := mgr.DeletePolicy("test-policy"); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}

	_, err := mgr.GetPolicy("test-policy")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestManagerDeleteNotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)
	if err := mgr.DeletePolicy("nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent policy")
	}
}

func TestManagerList(t *testing.T) {
	mgr, _ := setupTestManager(t)

	p1 := testPolicy()
	p1.Name = "b-policy"
	mgr.PutPolicy(p1)

	p2 := testPolicy()
	p2.Name = "a-policy"
	mgr.PutPolicy(p2)

	list := mgr.ListPolicies()
	if len(list) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(list))
	}
	if list[0].Name != "a-policy" {
		t.Errorf("expected sorted by name, first is %q", list[0].Name)
	}
}

func TestManagerAttachDetach(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())

	if err := mgr.AttachPolicy("test-index", "test-policy"); err != nil {
		t.Fatalf("AttachPolicy: %v", err)
	}

	status, err := mgr.GetStatus("test-index")
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.PolicyName != "test-policy" {
		t.Errorf("expected policy name test-policy, got %q", status.PolicyName)
	}
	if status.CurrentState != "hot" {
		t.Errorf("expected current state hot, got %q", status.CurrentState)
	}

	if err := mgr.DetachPolicy("test-index"); err != nil {
		t.Fatalf("DetachPolicy: %v", err)
	}

	_, err = mgr.GetStatus("test-index")
	if err == nil {
		t.Fatal("expected error after detach")
	}
}

func TestManagerAttachPolicyNotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)
	if err := mgr.AttachPolicy("test-index", "nonexistent"); err == nil {
		t.Fatal("expected error for nonexistent policy")
	}
}

func TestManagerAttachIndexNotFound(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())
	if err := mgr.AttachPolicy("nonexistent-index", "test-policy"); err == nil {
		t.Fatal("expected error for nonexistent index")
	}
}

func TestManagerAutoAttach(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy()) // has ISMTemplate pattern "logs-*"

	mgr.AutoAttach("logs-2025-01")

	status, err := mgr.GetStatus("logs-2025-01")
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.PolicyName != "test-policy" {
		t.Errorf("expected auto-attached test-policy, got %q", status.PolicyName)
	}
}

func TestManagerAutoAttachNoMatch(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())

	mgr.AutoAttach("test-index") // doesn't match "logs-*"

	_, err := mgr.GetStatus("test-index")
	if err == nil {
		t.Fatal("expected no auto-attach for non-matching index")
	}
}

func TestManagerAutoAttachAlreadyAttached(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())
	mgr.AttachPolicy("logs-2025-01", "test-policy")

	// Create second policy with higher priority
	p2 := testPolicy()
	p2.Name = "higher-priority"
	p2.ISMTemplate = []ISMTemplate{{Pattern: "logs-*", Priority: 200}}
	mgr.PutPolicy(p2)

	// AutoAttach should NOT override existing attachment
	mgr.AutoAttach("logs-2025-01")

	status, _ := mgr.GetStatus("logs-2025-01")
	if status.PolicyName != "test-policy" {
		t.Errorf("expected existing policy to remain, got %q", status.PolicyName)
	}
}

func TestManagerGetAllStatuses(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())
	mgr.AttachPolicy("test-index", "test-policy")
	mgr.AttachPolicy("logs-2025-01", "test-policy")

	statuses := mgr.GetAllStatuses()
	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}
	// Should be sorted by index name
	if statuses[0].Index != "logs-2025-01" {
		t.Errorf("expected sorted, first is %q", statuses[0].Index)
	}
}

func TestManagerRetryFailed(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())
	mgr.AttachPolicy("test-index", "test-policy")

	// Manually set failed state
	mgr.mu.Lock()
	mgr.states["test-index"].Failed = true
	mgr.states["test-index"].RetryCount = 3
	mgr.states["test-index"].LastError = "some error"
	mgr.mu.Unlock()

	if err := mgr.RetryFailed("test-index"); err != nil {
		t.Fatalf("RetryFailed: %v", err)
	}

	status, _ := mgr.GetStatus("test-index")
	if status.Failed {
		t.Error("expected Failed to be reset")
	}
	if status.RetryCount != 0 {
		t.Errorf("expected RetryCount 0, got %d", status.RetryCount)
	}
}

func TestManagerRetryNotFailed(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())
	mgr.AttachPolicy("test-index", "test-policy")

	if err := mgr.RetryFailed("test-index"); err == nil {
		t.Fatal("expected error when retrying non-failed index")
	}
}

func TestManagerPersistence(t *testing.T) {
	dir := t.TempDir()
	reg := index.NewRegistry(dir)
	reg.Create("test-index", index.Settings{ShardCount: 1})

	// Create manager, add policy, attach
	mgr1 := NewManager(dir, reg)
	mgr1.PutPolicy(testPolicy())
	mgr1.AttachPolicy("test-index", "test-policy")

	// Create new manager from same dir, load
	mgr2 := NewManager(dir, reg)
	if err := mgr2.LoadAll(); err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	// Verify policy loaded
	p, err := mgr2.GetPolicy("test-policy")
	if err != nil {
		t.Fatalf("GetPolicy after load: %v", err)
	}
	if p.Version != 1 {
		t.Errorf("expected version 1, got %d", p.Version)
	}

	// Verify state loaded
	status, err := mgr2.GetStatus("test-index")
	if err != nil {
		t.Fatalf("GetStatus after load: %v", err)
	}
	if status.PolicyName != "test-policy" {
		t.Errorf("expected policy name test-policy, got %q", status.PolicyName)
	}
}

func TestManagerLoadPolicyDir(t *testing.T) {
	dir := t.TempDir()
	reg := index.NewRegistry(dir)

	policyDir := filepath.Join(dir, "policies")
	os.MkdirAll(policyDir, 0o755)

	// Write a YAML policy file
	yamlContent := `name: yaml-policy
default_state: hot
states:
  - name: hot
    transitions:
      - state_name: cold
        conditions:
          min_index_age: 168h
  - name: cold
    actions:
      - type: close
`
	os.WriteFile(filepath.Join(policyDir, "test.yaml"), []byte(yamlContent), 0o644)

	// Write a non-YAML file (should be ignored)
	os.WriteFile(filepath.Join(policyDir, "readme.txt"), []byte("ignore me"), 0o644)

	mgr := NewManager(dir, reg)
	if err := mgr.LoadPolicyDir(policyDir); err != nil {
		t.Fatalf("LoadPolicyDir: %v", err)
	}

	p, err := mgr.GetPolicy("yaml-policy")
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if len(p.States) != 2 {
		t.Errorf("expected 2 states, got %d", len(p.States))
	}
}

func TestManagerManagedIndices(t *testing.T) {
	mgr, _ := setupTestManager(t)
	mgr.PutPolicy(testPolicy())
	mgr.AttachPolicy("test-index", "test-policy")
	mgr.AttachPolicy("logs-2025-01", "test-policy")

	indices := mgr.ManagedIndices()
	if len(indices) != 2 {
		t.Fatalf("expected 2 managed indices, got %d", len(indices))
	}
	if indices[0] != "logs-2025-01" {
		t.Errorf("expected sorted, first is %q", indices[0])
	}
}
