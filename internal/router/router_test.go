package router

import (
	"context"
	"testing"

	"github.com/brown/duckdb-cluster/internal/shard"
)

func setupRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	m, err := shard.NewManager(dir, 3)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.CloseAll() })
	return NewRouter(m)
}

func TestDDLBroadcast(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	result, err := r.Route(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("DDL route: %v", err)
	}
	if result.ShardID != -1 {
		t.Errorf("DDL shard_id should be -1, got %d", result.ShardID)
	}

	// Verify table exists on all shards by querying each
	for i := 0; i < r.Manager.ShardCount(); i++ {
		s := r.Manager.GetShard(i)
		_, err := s.Query(ctx, "SELECT * FROM test")
		if err != nil {
			t.Errorf("shard %d: table not created: %v", i, err)
		}
	}
}

func TestWriteRoutesToOneShard(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	// Create table first
	_, err := r.Route(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("DDL: %v", err)
	}

	// Insert with partition key
	result, err := r.Route(ctx, "INSERT INTO test VALUES (1, 'alice')", "1")
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	if result.ShardID < 0 || result.ShardID >= r.Manager.ShardCount() {
		t.Errorf("unexpected shard_id: %d", result.ShardID)
	}
}

func TestWriteRequiresPartitionKey(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "INSERT INTO test VALUES (1, 'alice')", "")
	if err == nil {
		t.Fatal("expected error for INSERT without partition_key")
	}
}

func TestReadFanOut(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("DDL: %v", err)
	}

	// Insert to different shards
	for i := 0; i < 10; i++ {
		key := string(rune('0' + i))
		sql := "INSERT INTO test VALUES (" + key + ", 'user" + key + "')"
		_, err := r.Route(ctx, sql, key)
		if err != nil {
			t.Fatalf("INSERT %d: %v", i, err)
		}
	}

	// Select should fan out and merge
	result, err := r.Route(ctx, "SELECT * FROM test", "")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if result.ShardID != -1 {
		t.Errorf("SELECT shard_id should be -1, got %d", result.ShardID)
	}
	if len(result.Rows) != 10 {
		t.Errorf("expected 10 merged rows, got %d", len(result.Rows))
	}
}
