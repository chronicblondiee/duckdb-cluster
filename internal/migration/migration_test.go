package migration

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		input   string
		want    SemVer
		wantErr bool
	}{
		{"1.2.3", SemVer{1, 2, 3}, false},
		{"v1.2.3", SemVer{1, 2, 3}, false},
		{"0.0.0-dev", SemVer{0, 0, 0}, false},
		{"1.10.3-rc1", SemVer{1, 10, 3}, false},
		{"1.2", SemVer{}, true},
		{"abc", SemVer{}, true},
		{"", SemVer{}, true},
		{"1.2.x", SemVer{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSemVer(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseSemVer(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("ParseSemVer(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSemVerLess(t *testing.T) {
	tests := []struct {
		a, b SemVer
		want bool
	}{
		{SemVer{0, 0, 1}, SemVer{0, 0, 2}, true},
		{SemVer{0, 1, 0}, SemVer{0, 2, 0}, true},
		{SemVer{1, 0, 0}, SemVer{2, 0, 0}, true},
		{SemVer{1, 0, 0}, SemVer{1, 0, 0}, false},
		{SemVer{2, 0, 0}, SemVer{1, 0, 0}, false},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s<%s", tt.a, tt.b)
		t.Run(name, func(t *testing.T) {
			if got := tt.a.Less(tt.b); got != tt.want {
				t.Fatalf("%s.Less(%s) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestSemVerEqual(t *testing.T) {
	a := SemVer{1, 2, 3}
	b := SemVer{1, 2, 3}
	c := SemVer{1, 2, 4}

	if !a.Equal(b) {
		t.Fatalf("%s should equal %s", a, b)
	}
	if a.Equal(c) {
		t.Fatalf("%s should not equal %s", a, c)
	}
}

func TestMigrationStateRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil, nil)

	state := &MigrationState{
		CurrentVersion: "1.0.0",
		AppliedMigrations: []AppliedMigration{
			{ID: "001_first"},
			{ID: "002_second"},
		},
	}

	if err := mgr.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := mgr.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.CurrentVersion != state.CurrentVersion {
		t.Fatalf("version = %q, want %q", loaded.CurrentVersion, state.CurrentVersion)
	}
	if len(loaded.AppliedMigrations) != 2 {
		t.Fatalf("applied count = %d, want 2", len(loaded.AppliedMigrations))
	}
	if loaded.AppliedMigrations[0].ID != "001_first" {
		t.Fatalf("first migration ID = %q, want %q", loaded.AppliedMigrations[0].ID, "001_first")
	}
}

func TestMigrationStateMissing(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil, nil)

	state, err := mgr.LoadState()
	if err != nil {
		t.Fatalf("LoadState on missing file: %v", err)
	}
	if state.CurrentVersion != "" {
		t.Fatalf("expected empty version, got %q", state.CurrentVersion)
	}
	if len(state.AppliedMigrations) != 0 {
		t.Fatalf("expected no applied migrations, got %d", len(state.AppliedMigrations))
	}
}

func TestAtomicStateWrite(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil, nil)

	state := &MigrationState{CurrentVersion: "1.0.0"}
	if err := mgr.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify tmp file was cleaned up
	tmpPath := mgr.stateFilePath() + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not exist after save, err = %v", err)
	}
}

func TestPendingMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil, nil)

	mgr.Register(MigrationDef{ID: "001_first", Description: "first"})
	mgr.Register(MigrationDef{ID: "002_second", Description: "second"})
	mgr.Register(MigrationDef{ID: "003_third", Description: "third"})

	// Mark the first as applied
	state := &MigrationState{
		AppliedMigrations: []AppliedMigration{
			{ID: "001_first"},
		},
	}
	if err := mgr.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	pending, err := mgr.PendingMigrations()
	if err != nil {
		t.Fatalf("PendingMigrations: %v", err)
	}

	if len(pending) != 2 {
		t.Fatalf("pending count = %d, want 2", len(pending))
	}
	if pending[0].ID != "002_second" {
		t.Fatalf("first pending = %q, want %q", pending[0].ID, "002_second")
	}
	if pending[1].ID != "003_third" {
		t.Fatalf("second pending = %q, want %q", pending[1].ID, "003_third")
	}
}

func TestRunPendingNoMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	sm, err := shard.NewManager(tmpDir, 2)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer sm.CloseAll()

	mgr := NewManager(tmpDir, sm, nil)
	result, err := mgr.RunPending(context.Background())
	if err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(result.Applied) != 0 {
		t.Fatalf("expected no applied migrations, got %d", len(result.Applied))
	}
}

func TestRunPendingAppliesAll(t *testing.T) {
	tmpDir := t.TempDir()
	sm, err := shard.NewManager(tmpDir, 2)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer sm.CloseAll()

	mgr := NewManager(tmpDir, sm, nil)
	mgr.Register(MigrationDef{
		ID:          "001_create_test_table",
		Description: "Create test table",
		Up: func(ctx context.Context, s *shard.Shard) error {
			_, err := s.Execute(ctx, "CREATE TABLE IF NOT EXISTS migration_test (id INTEGER, name VARCHAR)")
			return err
		},
	})

	ctx := context.Background()
	result, err := mgr.RunPending(ctx)
	if err != nil {
		t.Fatalf("RunPending: %v", err)
	}
	if len(result.Applied) != 1 {
		t.Fatalf("applied count = %d, want 1", len(result.Applied))
	}
	if result.Applied[0] != "001_create_test_table" {
		t.Fatalf("applied[0] = %q, want %q", result.Applied[0], "001_create_test_table")
	}

	// Verify table exists on both shards (query should not error)
	for i := 0; i < sm.ShardCount(); i++ {
		s := sm.GetShard(i)
		_, err := s.Query(ctx, "SELECT * FROM migration_test")
		if err != nil {
			t.Fatalf("shard %d: query migration_test: %v", i, err)
		}
	}

	// Verify state was persisted
	state, err := mgr.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.AppliedMigrations) != 1 {
		t.Fatalf("applied in state = %d, want 1", len(state.AppliedMigrations))
	}
}

func TestRunPendingIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	sm, err := shard.NewManager(tmpDir, 2)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer sm.CloseAll()

	mgr := NewManager(tmpDir, sm, nil)
	mgr.Register(MigrationDef{
		ID:          "001_create_table",
		Description: "Create table",
		Up: func(ctx context.Context, s *shard.Shard) error {
			_, err := s.Execute(ctx, "CREATE TABLE IF NOT EXISTS idempotent_test (id INTEGER)")
			return err
		},
	})

	ctx := context.Background()

	// First run
	result1, err := mgr.RunPending(ctx)
	if err != nil {
		t.Fatalf("first RunPending: %v", err)
	}
	if len(result1.Applied) != 1 {
		t.Fatalf("first run applied = %d, want 1", len(result1.Applied))
	}

	// Second run — should be no-op
	result2, err := mgr.RunPending(ctx)
	if err != nil {
		t.Fatalf("second RunPending: %v", err)
	}
	if len(result2.Applied) != 0 {
		t.Fatalf("second run applied = %d, want 0", len(result2.Applied))
	}
}

func TestRunPendingPartialFailure(t *testing.T) {
	tmpDir := t.TempDir()
	sm, err := shard.NewManager(tmpDir, 2)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer sm.CloseAll()

	mgr := NewManager(tmpDir, sm, nil)
	mgr.Register(MigrationDef{
		ID:          "001_good",
		Description: "Good migration",
		Up: func(ctx context.Context, s *shard.Shard) error {
			_, err := s.Execute(ctx, "CREATE TABLE IF NOT EXISTS good_table (id INTEGER)")
			return err
		},
	})
	mgr.Register(MigrationDef{
		ID:          "002_bad",
		Description: "Bad migration",
		Up: func(ctx context.Context, s *shard.Shard) error {
			return fmt.Errorf("intentional failure")
		},
	})

	ctx := context.Background()
	result, err := mgr.RunPending(ctx)
	if err == nil {
		t.Fatal("expected error from failing migration")
	}
	if result.Error == "" {
		t.Fatal("expected error message in result")
	}

	// First migration should have been recorded
	state, err := mgr.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.AppliedMigrations) != 1 {
		t.Fatalf("applied in state = %d, want 1 (only the successful one)", len(state.AppliedMigrations))
	}
	if state.AppliedMigrations[0].ID != "001_good" {
		t.Fatalf("applied ID = %q, want %q", state.AppliedMigrations[0].ID, "001_good")
	}
}

func TestGetStatus(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, nil, nil)

	mgr.Register(MigrationDef{ID: "001_first", Description: "first"})
	mgr.Register(MigrationDef{ID: "002_second", Description: "second"})

	// Mark one as applied
	state := &MigrationState{
		CurrentVersion:    "0.0.0-dev",
		AppliedMigrations: []AppliedMigration{{ID: "001_first"}},
	}
	if err := mgr.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	status, err := mgr.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}

	if status["applied_count"] != 1 {
		t.Fatalf("applied_count = %v, want 1", status["applied_count"])
	}
	if status["pending_count"] != 1 {
		t.Fatalf("pending_count = %v, want 1", status["pending_count"])
	}
	pendingList := status["pending_migrations"].([]string)
	if len(pendingList) != 1 || pendingList[0] != "002_second" {
		t.Fatalf("pending_migrations = %v, want [002_second]", pendingList)
	}
}
