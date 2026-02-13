package main

import (
	"context"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/cluster"
	"github.com/chronicblondiee/duckdb-cluster/internal/router"
	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

func TestIntegration(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Init cluster
	cfg := &cluster.Config{
		DataDir:   dir,
		NumShards: 3,
	}
	c, err := cluster.NewCluster(cfg)
	if err != nil {
		t.Fatalf("NewCluster: %v", err)
	}
	if err := c.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Start cluster
	m := &shard.Manager{DataDir: dir}
	if err := m.OpenAll(); err != nil {
		t.Fatalf("OpenAll: %v", err)
	}
	defer m.CloseAll()

	r := router.NewRouter(m)

	// Create table on all shards
	result, err := r.Route(ctx, "CREATE TABLE users (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}
	if result.ShardID != -1 {
		t.Errorf("DDL shard_id should be -1, got %d", result.ShardID)
	}

	// Insert data with different partition keys
	names := []string{"alice", "bob", "charlie", "dave", "eve", "frank", "grace", "heidi", "ivan", "judy"}
	shardCounts := make(map[int]int)
	for i, name := range names {
		key := string(rune('0' + i))
		sql := "INSERT INTO users VALUES (" + key + ", '" + name + "')"
		res, err := r.Route(ctx, sql, key)
		if err != nil {
			t.Fatalf("INSERT %s: %v", name, err)
		}
		shardCounts[res.ShardID]++
	}

	// Verify data distributed across multiple shards
	shardsUsed := len(shardCounts)
	if shardsUsed < 2 {
		t.Errorf("expected data on at least 2 shards, got %d", shardsUsed)
	}
	t.Logf("distribution: %v", shardCounts)

	// Select all — should merge from all shards
	result, err = r.Route(ctx, "SELECT * FROM users", "")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if len(result.Rows) != 10 {
		t.Errorf("expected 10 merged rows, got %d", len(result.Rows))
	}

	// Verify columns are present
	if len(result.Columns) == 0 {
		t.Error("expected columns in result")
	}
}
