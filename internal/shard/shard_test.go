package shard

import (
	"context"
	"os"
	"testing"
)

func TestShardLifecycle(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	s, err := NewShard(0, dir)
	if err != nil {
		t.Fatalf("NewShard: %v", err)
	}
	defer s.Close()

	// Verify file was created
	if _, err := os.Stat(s.Path); err != nil {
		t.Fatalf("shard file not created: %v", err)
	}

	// Execute DDL
	_, err = s.Execute(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)")
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	// Insert
	_, err = s.Execute(ctx, "INSERT INTO test VALUES (1, 'alice')")
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	_, err = s.Execute(ctx, "INSERT INTO test VALUES (2, 'bob')")
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}

	// Query
	rows, err := s.Query(ctx, "SELECT * FROM test ORDER BY id")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Close and reopen
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Open(); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	rows, err = s.Query(ctx, "SELECT * FROM test")
	if err != nil {
		t.Fatalf("SELECT after reopen: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows after reopen, got %d", len(rows))
	}
}
