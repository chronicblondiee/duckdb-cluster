package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brown/duckdb-cluster/internal/config"
	"github.com/brown/duckdb-cluster/internal/distributor"
	"github.com/brown/duckdb-cluster/internal/ingester"
	"github.com/brown/duckdb-cluster/internal/querier"
	"github.com/brown/duckdb-cluster/internal/ring"
)

// TestWriteReplication tests write replication across multiple ingesters
func TestWriteReplication(t *testing.T) {
	ctx := context.Background()
	
	// Create temporary test directory
	testDir := t.TempDir()
	
	// Create 3 mock ingesters with shards
	ingesters := make([]*ingester.Ingester, 3)
	for i := 0; i < 3; i++ {
		cfg := config.Default()
		cfg.Common.DataDir = filepath.Join(testDir, fmt.Sprintf("node%d", i))
		cfg.Common.NumShards = 2
		
		if err := os.MkdirAll(cfg.Common.DataDir, 0755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		
		ing := ingester.NewIngester(cfg)
		if err := ing.Init(ctx); err != nil {
			t.Fatalf("init ingester %d: %v", i, err)
		}
		
		// Initialize shards by running a query on the manager
		manager := ing.GetManager()
		if manager.ShardCount() == 0 {
			// Create shards via the manager
			for j := 0; j < cfg.Common.NumShards; j++ {
				if _, err := manager.AddShard(); err != nil {
					t.Fatalf("add shard to ingester %d: %v", i, err)
				}
			}
		}
		
		ingesters[i] = ing
	}
	
	// Clean up
	defer func() {
		for _, ing := range ingesters {
			ing.Shutdown()
		}
	}()
	
	// Create ring with nodes
	cfg := config.Default()
	cfg.Distributor.ReplicationFactor = 3
	r := ring.NewRing(cfg)
	
	for i := 0; i < 3; i++ {
		r.AddNode(&ring.Node{
			ID:      fmt.Sprintf("node-%d", i),
			Addr:    fmt.Sprintf("localhost:909%d", i),
			IsLocal: i == 0,
		})
	}
	
	// Create replication coordinator
	replicator := distributor.NewReplicationCoordinator(r, 3)
	
	// Create push function that routes to the correct ingester
	pushFunc := func(ctx context.Context, nodeID string, req *distributor.PushRequest) (*distributor.PushResponse, error) {
		// Extract node index from ID
		var idx int
		fmt.Sscanf(nodeID, "node-%d", &idx)
		
		if idx < 0 || idx >= len(ingesters) {
			return nil, fmt.Errorf("invalid node index: %d", idx)
		}
		
		return ingesters[idx].Push(ctx, req)
	}
	
	// Create DDL on all ingesters
	for i, ing := range ingesters {
		_, err := ing.Query(ctx, "CREATE TABLE test (id INTEGER, name TEXT)")
		if err != nil {
			t.Fatalf("create table on ingester %d: %v", i, err)
		}
	}
	
	// Execute replicated write
	req := &distributor.PushRequest{
		SQL:          "INSERT INTO test VALUES (1, 'Alice')",
		PartitionKey: "user-1",
	}
	
	resp, err := replicator.ReplicateWrite(ctx, req, pushFunc)
	if err != nil {
		t.Fatalf("replicate write: %v", err)
	}
	
	if resp.Error != "" {
		t.Fatalf("write returned error: %s", resp.Error)
	}
	
	// Verify data was written to all ingesters
	for i, ing := range ingesters {
		result, err := ing.Query(ctx, "SELECT * FROM test")
		if err != nil {
			t.Fatalf("query ingester %d: %v", i, err)
		}
		
		if len(result.Rows) != 1 {
			t.Errorf("ingester %d: expected 1 row, got %d", i, len(result.Rows))
		}
	}
	
	t.Logf("✓ Write replicated to all 3 ingesters")
}

// TestReadConsistency tests read consistency levels
func TestReadConsistency(t *testing.T) {
	ctx := context.Background()
	
	// Create temporary test directory
	testDir := t.TempDir()
	
	// Create 3 mock queriers
	queriers := make([]*querier.Querier, 3)
	for i := 0; i < 3; i++ {
		cfg := config.Default()
		cfg.Common.DataDir = filepath.Join(testDir, fmt.Sprintf("node%d", i))
		cfg.Common.NumShards = 2
		
		if err := os.MkdirAll(cfg.Common.DataDir, 0755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		
		// Create ingester first to initialize data
		ing := ingester.NewIngester(cfg)
		if err := ing.Init(ctx); err != nil {
			t.Fatalf("init ingester %d: %v", i, err)
		}
		
		// Initialize shards
		manager := ing.GetManager()
		if manager.ShardCount() == 0 {
			for j := 0; j < cfg.Common.NumShards; j++ {
				if _, err := manager.AddShard(); err != nil {
					t.Fatalf("add shard to ingester %d: %v", i, err)
				}
			}
		}
		
		// Create table and insert data
		_, err := ing.Query(ctx, "CREATE TABLE test (id INTEGER, name TEXT)")
		if err != nil {
			t.Fatalf("create table on node %d: %v", i, err)
		}
		
		_, err = ing.Push(ctx, &distributor.PushRequest{
			SQL:          "INSERT INTO test VALUES (1, 'Alice')",
			PartitionKey: "user-1",
		})
		if err != nil {
			t.Fatalf("insert on node %d: %v", i, err)
		}
		
		ing.Shutdown()
		
		// Create querier
		q := querier.NewQuerier(cfg)
		if err := q.Init(ctx); err != nil {
			t.Fatalf("init querier %d: %v", i, err)
		}
		queriers[i] = q
	}
	
	// Clean up
	defer func() {
		for _, q := range queriers {
			q.Shutdown()
		}
	}()
	
	// Create ring with nodes
	cfg := config.Default()
	r := ring.NewRing(cfg)
	
	for i := 0; i < 3; i++ {
		r.AddNode(&ring.Node{
			ID:      fmt.Sprintf("node-%d", i),
			Addr:    fmt.Sprintf("localhost:909%d", i),
			IsLocal: i == 0,
		})
	}
	
	// Test different consistency levels
	testCases := []struct {
		name              string
		level             querier.ConsistencyLevel
		replicationFactor int
	}{
		{"ConsistencyOne", querier.ConsistencyOne, 3},
		{"ConsistencyQuorum", querier.ConsistencyQuorum, 3},
		{"ConsistencyAll", querier.ConsistencyAll, 3},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			coordinator := querier.NewConsistencyCoordinator(r, tc.level, tc.replicationFactor)
			
			// Query function that routes to the correct querier
			queryFunc := func(ctx context.Context, nodeID string, req *querier.QueryRequest) (*querier.QueryResponse, error) {
				var idx int
				fmt.Sscanf(nodeID, "node-%d", &idx)
				
				if idx < 0 || idx >= len(queriers) {
					return nil, fmt.Errorf("invalid node index: %d", idx)
				}
				
				return queriers[idx].Query(ctx, req)
			}
			
			req := &querier.QueryRequest{
				SQL: "SELECT * FROM test",
			}
			
			resp, err := coordinator.ConsistentQuery(ctx, req, queryFunc)
			if err != nil {
				t.Fatalf("consistent query: %v", err)
			}
			
			if resp.Error != "" {
				t.Fatalf("query returned error: %s", resp.Error)
			}
			
			if len(resp.Rows) != 1 {
				t.Errorf("expected 1 row, got %d", len(resp.Rows))
			}
			
			t.Logf("✓ %s succeeded", tc.name)
		})
	}
}

// TestHealthTracking tests node health tracking and circuit breaker
func TestHealthTracking(t *testing.T) {
	tracker := ring.NewHealthTracker()
	
	// Add some nodes
	tracker.AddNode("node-1", "localhost:9091")
	tracker.AddNode("node-2", "localhost:9092")
	tracker.AddNode("node-3", "localhost:9093")
	
	// Verify all nodes are initially healthy
	if !tracker.IsHealthy("node-1") {
		t.Errorf("node-1 should be healthy")
	}
	
	// Record failures for node-1
	for i := 0; i < 3; i++ {
		tracker.RecordFailure("node-1")
	}
	
	// Node should be unhealthy after threshold failures
	health, exists := tracker.GetHealth("node-1")
	if !exists {
		t.Fatal("node-1 not found")
	}
	
	if health.State != ring.StateUnhealthy {
		t.Errorf("node-1 should be unhealthy, got %s", health.State)
	}
	
	// Record more failures
	for i := 0; i < 3; i++ {
		tracker.RecordFailure("node-1")
	}
	
	// Node should be down after more failures
	health, _ = tracker.GetHealth("node-1")
	if health.State != ring.StateDown {
		t.Errorf("node-1 should be down, got %s", health.State)
	}
	
	// Record success to recover
	tracker.RecordSuccess("node-1")
	
	// Node should be healthy again
	if !tracker.IsHealthy("node-1") {
		t.Errorf("node-1 should be healthy after recovery")
	}
	
	t.Logf("✓ Health tracking works correctly")
}

// TestReplicationPartialFailure tests replication with some nodes failing
func TestReplicationPartialFailure(t *testing.T) {
	ctx := context.Background()
	
	// Create temporary test directory
	testDir := t.TempDir()
	
	// Create 2 working ingesters
	ingesters := make([]*ingester.Ingester, 2)
	for i := 0; i < 2; i++ {
		cfg := config.Default()
		cfg.Common.DataDir = filepath.Join(testDir, fmt.Sprintf("node%d", i))
		cfg.Common.NumShards = 2
		
		if err := os.MkdirAll(cfg.Common.DataDir, 0755); err != nil {
			t.Fatalf("create data dir: %v", err)
		}
		
		ing := ingester.NewIngester(cfg)
		if err := ing.Init(ctx); err != nil {
			t.Fatalf("init ingester %d: %v", i, err)
		}
		
		// Initialize shards
		manager := ing.GetManager()
		if manager.ShardCount() == 0 {
			for j := 0; j < cfg.Common.NumShards; j++ {
				if _, err := manager.AddShard(); err != nil {
					t.Fatalf("add shard to ingester %d: %v", i, err)
				}
			}
		}
		
		ingesters[i] = ing
	}
	
	// Clean up
	defer func() {
		for _, ing := range ingesters {
			ing.Shutdown()
		}
	}()
	
	// Create ring with 3 nodes (but only 2 are working)
	cfg := config.Default()
	cfg.Distributor.ReplicationFactor = 3
	r := ring.NewRing(cfg)
	
	for i := 0; i < 3; i++ {
		r.AddNode(&ring.Node{
			ID:      fmt.Sprintf("node-%d", i),
			Addr:    fmt.Sprintf("localhost:909%d", i),
			IsLocal: i == 0,
		})
	}
	
	// Create replication coordinator
	replicator := distributor.NewReplicationCoordinator(r, 3)
	
	// Create push function where node-2 always fails
	pushFunc := func(ctx context.Context, nodeID string, req *distributor.PushRequest) (*distributor.PushResponse, error) {
		var idx int
		fmt.Sscanf(nodeID, "node-%d", &idx)
		
		// Simulate node-2 failure
		if idx == 2 {
			return nil, fmt.Errorf("node-2 is down")
		}
		
		if idx < 0 || idx >= len(ingesters) {
			return nil, fmt.Errorf("invalid node index: %d", idx)
		}
		
		return ingesters[idx].Push(ctx, req)
	}
	
	// Create DDL on working ingesters
	for i, ing := range ingesters {
		_, err := ing.Query(ctx, "CREATE TABLE test (id INTEGER, name TEXT)")
		if err != nil {
			t.Fatalf("create table on ingester %d: %v", i, err)
		}
	}
	
	// Execute replicated write (should succeed with quorum 2/3)
	req := &distributor.PushRequest{
		SQL:          "INSERT INTO test VALUES (1, 'Alice')",
		PartitionKey: "user-1",
	}
	
	resp, err := replicator.ReplicateWrite(ctx, req, pushFunc)
	if err != nil {
		t.Fatalf("replicate write: %v", err)
	}
	
	if resp.Error != "" {
		t.Fatalf("write returned error: %s", resp.Error)
	}
	
	// Verify data was written to working ingesters
	for i, ing := range ingesters {
		result, err := ing.Query(ctx, "SELECT * FROM test")
		if err != nil {
			t.Fatalf("query ingester %d: %v", i, err)
		}
		
		if len(result.Rows) != 1 {
			t.Errorf("ingester %d: expected 1 row, got %d", i, len(result.Rows))
		}
	}
	
	t.Logf("✓ Replication succeeded with partial failure (2/3 nodes)")
}

// TestRecoveryCheck tests the health tracker's recovery check mechanism
func TestRecoveryCheck(t *testing.T) {
	tracker := ring.NewHealthTracker()
	
	// Add nodes
	tracker.AddNode("node-1", "localhost:9091")
	tracker.AddNode("node-2", "localhost:9092")
	
	// Mark node-1 as unhealthy
	for i := 0; i < 5; i++ {
		tracker.RecordFailure("node-1")
	}
	
	health, _ := tracker.GetHealth("node-1")
	if health.State == ring.StateHealthy {
		t.Fatal("node-1 should be unhealthy")
	}
	
	// Create a check function that succeeds
	checkFunc := func(nodeID, addr string) error {
		return nil // Simulate successful check
	}
	
	// Create context with short timeout for testing
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	
	// Start recovery check in background
	go tracker.StartRecoveryCheck(ctx, checkFunc)
	
	// Wait a bit for recovery check to run
	time.Sleep(50 * time.Millisecond)
	
	// Node should still be unhealthy (recovery check interval is longer)
	// But we can manually trigger it
	allHealth := tracker.GetAllHealth()
	if len(allHealth) != 2 {
		t.Errorf("expected 2 nodes in health tracker, got %d", len(allHealth))
	}
	
	t.Logf("✓ Recovery check mechanism initialized")
}
