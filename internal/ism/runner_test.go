package ism

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
)

func setupRunnerTest(t *testing.T) (*Runner, *Manager, *index.Registry) {
	t.Helper()
	dir := t.TempDir()

	reg := index.NewRegistry(dir)
	mgr := NewManager(dir, reg)
	runner := NewRunner(mgr, reg, time.Hour) // long interval — we call EvaluateAll manually
	return runner, mgr, reg
}

func TestRunnerCloseAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	reg.Create("test-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "close-policy",
		DefaultState: "active",
		States: []PolicyState{
			{
				Name:    "active",
				Actions: []Action{{Type: ActionClose}},
			},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("test-idx", "close-policy")

	runner.EvaluateAll(context.Background())

	// Index should now be closed
	_, err := reg.Get("test-idx")
	if err == nil {
		t.Fatal("expected error getting closed index")
	}

	// Action index should have advanced
	status, _ := mgr.GetStatus("test-idx")
	if status.ActionIndex != 1 {
		t.Errorf("expected action_index 1, got %d", status.ActionIndex)
	}
}

func TestRunnerDeleteAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	reg.Create("del-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "delete-policy",
		DefaultState: "doomed",
		States: []PolicyState{
			{
				Name:    "doomed",
				Actions: []Action{{Type: ActionDelete}},
			},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("del-idx", "delete-policy")

	runner.EvaluateAll(context.Background())

	// Index should be gone
	_, err := reg.GetAny("del-idx")
	if err == nil {
		t.Fatal("expected index to be deleted")
	}

	// ISM state should be cleaned up
	_, err = mgr.GetStatus("del-idx")
	if err == nil {
		t.Fatal("expected ISM state to be removed after delete")
	}
}

func TestRunnerTransitionMinIndexAge(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	// Create index with a CreatedAt in the past
	reg.Create("old-idx", index.Settings{ShardCount: 1})

	// Manually set CreatedAt to 10 days ago
	idx, _ := reg.GetAny("old-idx")
	idx.Meta.CreatedAt = time.Now().Add(-10 * 24 * time.Hour)
	reg.SaveCatalog()

	sevenDays := Duration{Duration: 7 * 24 * time.Hour}
	p := &Policy{
		Name:         "age-policy",
		DefaultState: "hot",
		States: []PolicyState{
			{
				Name: "hot",
				Transitions: []Transition{
					{
						StateName:  "warm",
						Conditions: &TransitionConditions{MinIndexAge: &sevenDays},
					},
				},
			},
			{Name: "warm"},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("old-idx", "age-policy")

	runner.EvaluateAll(context.Background())

	status, _ := mgr.GetStatus("old-idx")
	if status.CurrentState != "warm" {
		t.Errorf("expected state 'warm', got %q", status.CurrentState)
	}
}

func TestRunnerTransitionNotYet(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	reg.Create("new-idx", index.Settings{ShardCount: 1})

	sevenDays := Duration{Duration: 7 * 24 * time.Hour}
	p := &Policy{
		Name:         "age-policy",
		DefaultState: "hot",
		States: []PolicyState{
			{
				Name: "hot",
				Transitions: []Transition{
					{
						StateName:  "warm",
						Conditions: &TransitionConditions{MinIndexAge: &sevenDays},
					},
				},
			},
			{Name: "warm"},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("new-idx", "age-policy")

	runner.EvaluateAll(context.Background())

	// Should still be in hot state (index is brand new)
	status, _ := mgr.GetStatus("new-idx")
	if status.CurrentState != "hot" {
		t.Errorf("expected state 'hot', got %q", status.CurrentState)
	}
}

func TestRunnerActionFailureAndRetry(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	// Create index — then close it so force_merge will fail (can't force_merge closed index)
	reg.Create("fail-idx", index.Settings{ShardCount: 1})
	reg.CloseIndex("fail-idx")

	p := &Policy{
		Name:         "fail-policy",
		DefaultState: "active",
		States: []PolicyState{
			{
				Name: "active",
				Actions: []Action{
					{
						Type:  ActionForceMerge,
						Retry: &RetryConfig{Count: 1, Backoff: Duration{Duration: time.Nanosecond}},
					},
				},
			},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("fail-idx", "fail-policy")

	// First evaluation — should fail, retry count = 1
	runner.EvaluateAll(context.Background())
	status, _ := mgr.GetStatus("fail-idx")
	if status.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", status.RetryCount)
	}
	if status.Failed {
		t.Error("should not be failed yet (1 retry allowed)")
	}

	// Second evaluation — should fail again and mark as failed
	runner.EvaluateAll(context.Background())
	status, _ = mgr.GetStatus("fail-idx")
	if !status.Failed {
		t.Error("expected Failed to be true after retries exhausted")
	}
}

func TestRunnerSkipsFailedIndex(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	reg.Create("stuck-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "close-policy",
		DefaultState: "active",
		States: []PolicyState{
			{Name: "active", Actions: []Action{{Type: ActionClose}}},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("stuck-idx", "close-policy")

	// Mark as failed
	mgr.mu.Lock()
	mgr.states["stuck-idx"].Failed = true
	mgr.mu.Unlock()

	// Should skip the failed index
	runner.EvaluateAll(context.Background())

	// Index should still be open (action not executed)
	idx, err := reg.Get("stuck-idx")
	if err != nil {
		t.Fatalf("expected index to still be open, got: %v", err)
	}
	_ = idx
}

func TestRunnerReadOnlyAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	reg.Create("ro-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "ro-policy",
		DefaultState: "active",
		States: []PolicyState{
			{Name: "active", Actions: []Action{{Type: ActionReadOnly}}},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("ro-idx", "ro-policy")

	runner.EvaluateAll(context.Background())

	idx, _ := reg.GetAny("ro-idx")
	if !idx.Meta.ReadOnly {
		t.Error("expected index to be read-only")
	}
}

func TestRunnerForceMergeAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	reg.Create("merge-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "merge-policy",
		DefaultState: "active",
		States: []PolicyState{
			{Name: "active", Actions: []Action{{Type: ActionForceMerge}}},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("merge-idx", "merge-policy")

	runner.EvaluateAll(context.Background())

	status, _ := mgr.GetStatus("merge-idx")
	if status.ActionIndex != 1 {
		t.Errorf("expected action_index 1, got %d", status.ActionIndex)
	}
}

func TestRunnerNotificationAction(t *testing.T) {
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		received = body["text"]
		w.WriteHeader(200)
	}))
	defer srv.Close()

	runner, mgr, reg := setupRunnerTest(t)
	reg.Create("notify-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "notify-policy",
		DefaultState: "active",
		States: []PolicyState{
			{
				Name: "active",
				Actions: []Action{
					{
						Type: ActionNotification,
						Config: map[string]any{
							"url":     srv.URL,
							"message": "Index {{.IndexName}} in {{.State}}",
						},
					},
				},
			},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("notify-idx", "notify-policy")

	runner.EvaluateAll(context.Background())

	if received != "Index notify-idx in active" {
		t.Errorf("expected notification message, got %q", received)
	}
}

func TestRunnerSnapshotAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)
	reg.Create("snap-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "snap-policy",
		DefaultState: "active",
		States: []PolicyState{
			{Name: "active", Actions: []Action{{Type: ActionSnapshot}}},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("snap-idx", "snap-policy")

	runner.EvaluateAll(context.Background())

	status, _ := mgr.GetStatus("snap-idx")
	if status.ActionIndex != 1 {
		t.Errorf("expected action_index 1, got %d", status.ActionIndex)
	}
	if status.LastError != "" {
		t.Errorf("unexpected error: %s", status.LastError)
	}
}

func TestRunnerRolloverAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)

	// Create source index with rollover-style name
	reg.Create("logs-000001", index.Settings{ShardCount: 1})

	// Set up a mock alias updater
	mockAlias := &mockAliasUpdater{indices: map[string][]string{
		"logs": {"logs-000001"},
	}}
	runner.SetAliasUpdater(mockAlias)

	p := &Policy{
		Name:         "rollover-policy",
		DefaultState: "active",
		States: []PolicyState{
			{
				Name: "active",
				Actions: []Action{
					{
						Type:   ActionRollover,
						Config: map[string]any{"alias": "logs"},
					},
				},
			},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("logs-000001", "rollover-policy")

	runner.EvaluateAll(context.Background())

	// New index should have been created
	_, err := reg.Get("logs-000002")
	if err != nil {
		t.Fatalf("expected new index logs-000002, got: %v", err)
	}

	// Alias should have been updated
	if indices, ok := mockAlias.indices["logs"]; ok {
		found := false
		for _, idx := range indices {
			if idx == "logs-000002" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected alias to include logs-000002, got %v", indices)
		}
	} else {
		t.Error("expected alias 'logs' to exist")
	}
}

func TestRunnerSequentialActions(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)
	reg.Create("seq-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "seq-policy",
		DefaultState: "active",
		States: []PolicyState{
			{
				Name: "active",
				Actions: []Action{
					{Type: ActionReadOnly},
					{Type: ActionClose},
				},
			},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("seq-idx", "seq-policy")

	// First evaluation — executes read_only (action 0)
	runner.EvaluateAll(context.Background())
	status, _ := mgr.GetStatus("seq-idx")
	if status.ActionIndex != 1 {
		t.Fatalf("expected action_index 1, got %d", status.ActionIndex)
	}

	// Second evaluation — executes close (action 1)
	runner.EvaluateAll(context.Background())
	status, _ = mgr.GetStatus("seq-idx")
	if status.ActionIndex != 2 {
		t.Fatalf("expected action_index 2, got %d", status.ActionIndex)
	}
}

func TestRunnerUnconditionalTransition(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)
	reg.Create("uc-idx", index.Settings{ShardCount: 1})

	p := &Policy{
		Name:         "uc-policy",
		DefaultState: "a",
		States: []PolicyState{
			{
				Name:        "a",
				Transitions: []Transition{{StateName: "b"}}, // no conditions
			},
			{Name: "b"},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("uc-idx", "uc-policy")

	runner.EvaluateAll(context.Background())

	status, _ := mgr.GetStatus("uc-idx")
	if status.CurrentState != "b" {
		t.Errorf("expected state 'b', got %q", status.CurrentState)
	}
}

func TestRunnerStartStop(t *testing.T) {
	runner, _, _ := setupRunnerTest(t)

	ctx := context.Background()
	runner.interval = 50 * time.Millisecond
	runner.Start(ctx)

	// Let it tick a couple times
	time.Sleep(150 * time.Millisecond)

	runner.Stop()
	// If Stop blocks forever, test will timeout
}

func TestRunnerOpenAction(t *testing.T) {
	runner, mgr, reg := setupRunnerTest(t)
	reg.Create("reopen-idx", index.Settings{ShardCount: 1})
	reg.CloseIndex("reopen-idx")

	p := &Policy{
		Name:         "open-policy",
		DefaultState: "closed",
		States: []PolicyState{
			{Name: "closed", Actions: []Action{{Type: ActionOpen}}},
		},
	}
	mgr.PutPolicy(p)
	mgr.AttachPolicy("reopen-idx", "open-policy")

	runner.EvaluateAll(context.Background())

	// Should be open now
	_, err := reg.Get("reopen-idx")
	if err != nil {
		t.Fatalf("expected index to be open, got: %v", err)
	}
}

// mockAliasUpdater implements AliasUpdater for testing.
type mockAliasUpdater struct {
	indices map[string][]string
}

func (m *mockAliasUpdater) Put(name string, indices []string) error {
	m.indices[name] = indices
	return nil
}

func (m *mockAliasUpdater) GetIndices(name string) ([]string, error) {
	indices, ok := m.indices[name]
	if !ok {
		return nil, fmt.Errorf("alias %q not found", name)
	}
	return indices, nil
}
