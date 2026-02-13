package ring

import (
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
)

func TestRingSingleNode(t *testing.T) {
	cfg := config.Default()
	cfg.Ring.InstanceID = "node-1"
	cfg.Ring.InstanceAddr = "localhost:9095"
	
	ring := NewRing(cfg)
	
	if err := ring.Init(); err != nil {
		t.Fatalf("failed to init ring: %v", err)
	}
	
	// Check that the local node is in the ring
	localNode, err := ring.GetLocalNode()
	if err != nil {
		t.Fatalf("failed to get local node: %v", err)
	}
	
	if localNode.ID != "node-1" {
		t.Errorf("expected node ID 'node-1', got '%s'", localNode.ID)
	}
	
	if !localNode.IsLocal {
		t.Error("node should be marked as local")
	}
	
	// Check that we can get the node for any key
	node, err := ring.GetNode("test-key")
	if err != nil {
		t.Fatalf("failed to get node for key: %v", err)
	}
	
	if node.ID != "node-1" {
		t.Errorf("expected node ID 'node-1', got '%s'", node.ID)
	}
}

func TestRingConsistentHashing(t *testing.T) {
	cfg := config.Default()
	ring := NewRing(cfg)
	
	// Manually add multiple nodes to test consistent hashing
	ring.mu.Lock()
	ring.addNode(&Node{ID: "node-1", Addr: "localhost:9001", IsLocal: true})
	ring.addNode(&Node{ID: "node-2", Addr: "localhost:9002", IsLocal: false})
	ring.addNode(&Node{ID: "node-3", Addr: "localhost:9003", IsLocal: false})
	ring.mu.Unlock()
	
	// Test that the same key always maps to the same node
	keys := []string{"user-1", "user-2", "user-3", "order-100", "product-abc"}
	
	for _, key := range keys {
		node1, err := ring.GetNode(key)
		if err != nil {
			t.Fatalf("failed to get node for key %s: %v", key, err)
		}
		
		// Call again and verify we get the same node
		node2, err := ring.GetNode(key)
		if err != nil {
			t.Fatalf("failed to get node for key %s (2nd call): %v", key, err)
		}
		
		if node1.ID != node2.ID {
			t.Errorf("key %s mapped to different nodes: %s vs %s", key, node1.ID, node2.ID)
		}
	}
}

func TestRingDistribution(t *testing.T) {
	cfg := config.Default()
	ring := NewRing(cfg)
	
	// Manually add multiple nodes
	ring.mu.Lock()
	ring.addNode(&Node{ID: "node-1", Addr: "localhost:9001", IsLocal: true})
	ring.addNode(&Node{ID: "node-2", Addr: "localhost:9002", IsLocal: false})
	ring.addNode(&Node{ID: "node-3", Addr: "localhost:9003", IsLocal: false})
	ring.mu.Unlock()
	
	// Generate many keys and check distribution
	distribution := make(map[string]int)
	numKeys := 10000
	
	for i := 0; i < numKeys; i++ {
		key := string(rune('k')) + string(rune(i))
		node, err := ring.GetNode(key)
		if err != nil {
			t.Fatalf("failed to get node for key: %v", err)
		}
		distribution[node.ID]++
	}
	
	// With 3 nodes and good hashing, each should get roughly 1/3 of keys
	// Allow for some variance (between 20% and 45% of keys per node)
	for nodeID, count := range distribution {
		percentage := float64(count) / float64(numKeys) * 100
		t.Logf("Node %s: %d keys (%.1f%%)", nodeID, count, percentage)
		
		if percentage < 20 || percentage > 45 {
			t.Errorf("poor distribution for node %s: %.1f%% (expected ~33%%)", nodeID, percentage)
		}
	}
	
	// Verify all 3 nodes received some keys
	if len(distribution) != 3 {
		t.Errorf("expected 3 nodes to receive keys, got %d", len(distribution))
	}
}

func TestRingGetAllNodes(t *testing.T) {
	cfg := config.Default()
	ring := NewRing(cfg)
	
	if err := ring.Init(); err != nil {
		t.Fatalf("failed to init ring: %v", err)
	}
	
	nodes := ring.GetAllNodes()
	if len(nodes) != 1 {
		t.Errorf("expected 1 node, got %d", len(nodes))
	}
	
	if nodes[0].ID != cfg.Ring.InstanceID {
		t.Errorf("unexpected node ID: %s", nodes[0].ID)
	}
}

func TestRingEmptyRing(t *testing.T) {
	cfg := config.Default()
	ring := NewRing(cfg)
	
	// Don't initialize the ring
	
	_, err := ring.GetNode("test-key")
	if err == nil {
		t.Error("expected error when getting node from empty ring")
	}
	
	_, err = ring.GetLocalNode()
	if err == nil {
		t.Error("expected error when getting local node from empty ring")
	}
}

func TestRingIsSingleNode(t *testing.T) {
	cfg := config.Default()
	ring := NewRing(cfg)
	
	if err := ring.Init(); err != nil {
		t.Fatalf("failed to init ring: %v", err)
	}
	
	if !ring.IsSingleNode() {
		t.Error("expected IsSingleNode() to return true")
	}
	
	// Add another node manually
	ring.mu.Lock()
	ring.addNode(&Node{ID: "node-2", Addr: "localhost:9002", IsLocal: false})
	ring.mu.Unlock()
	
	if ring.IsSingleNode() {
		t.Error("expected IsSingleNode() to return false after adding second node")
	}
}
