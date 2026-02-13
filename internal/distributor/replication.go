package distributor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/brown/duckdb-cluster/internal/ring"
)

// ReplicationCoordinator handles replication of writes to multiple ingesters
type ReplicationCoordinator struct {
	ring              *ring.Ring
	replicationFactor int
}

// NewReplicationCoordinator creates a new replication coordinator
func NewReplicationCoordinator(r *ring.Ring, replicationFactor int) *ReplicationCoordinator {
	return &ReplicationCoordinator{
		ring:              r,
		replicationFactor: replicationFactor,
	}
}

// ReplicateWrite sends a write to N ingesters based on replication factor
func (rc *ReplicationCoordinator) ReplicateWrite(
	ctx context.Context,
	req *PushRequest,
	pushFunc func(ctx context.Context, nodeID string, req *PushRequest) (*PushResponse, error),
) (*PushResponse, error) {
	// Get target nodes for replication
	nodes, err := rc.getReplicationTargets(req.PartitionKey)
	if err != nil {
		return nil, fmt.Errorf("get replication targets: %w", err)
	}
	
	slog.Debug("replicating write",
		"partition_key", req.PartitionKey,
		"replication_factor", rc.replicationFactor,
		"target_nodes", len(nodes),
	)
	
	// If only one node, use simple path
	if len(nodes) == 1 {
		return pushFunc(ctx, nodes[0].ID, req)
	}
	
	// Replicate to all nodes in parallel
	type result struct {
		nodeID   string
		response *PushResponse
		err      error
	}
	
	resultCh := make(chan result, len(nodes))
	var wg sync.WaitGroup
	
	for _, node := range nodes {
		wg.Add(1)
		go func(n *ring.Node) {
			defer wg.Done()
			
			resp, err := pushFunc(ctx, n.ID, req)
			resultCh <- result{
				nodeID:   n.ID,
				response: resp,
				err:      err,
			}
		}(node)
	}
	
	// Wait for all replications to complete
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	
	// Collect results
	var (
		successCount int
		firstSuccess *PushResponse
		lastError    error
		errors       []error
	)
	
	for res := range resultCh {
		if res.err != nil {
			slog.Warn("replication failed",
				"node_id", res.nodeID,
				"error", res.err,
			)
			lastError = res.err
			errors = append(errors, res.err)
			continue
		}
		
		if res.response.Error != "" {
			err := fmt.Errorf("node %s returned error: %s", res.nodeID, res.response.Error)
			slog.Warn("replication returned error",
				"node_id", res.nodeID,
				"error", res.response.Error,
			)
			lastError = err
			errors = append(errors, err)
			continue
		}
		
		successCount++
		if firstSuccess == nil {
			firstSuccess = res.response
		}
		
		slog.Debug("replication succeeded",
			"node_id", res.nodeID,
			"rows_affected", res.response.RowsAffected,
		)
	}
	
	// Determine if replication was successful
	// We need at least a quorum (majority) of replicas to succeed
	quorum := (rc.replicationFactor / 2) + 1
	
	if successCount >= quorum {
		slog.Debug("replication completed",
			"successful_replicas", successCount,
			"required_quorum", quorum,
		)
		return firstSuccess, nil
	}
	
	// Replication failed - not enough successful replicas
	slog.Error("replication failed - insufficient successful replicas",
		"successful", successCount,
		"required_quorum", quorum,
		"total_targets", len(nodes),
		"errors", len(errors),
	)
	
	if lastError != nil {
		return nil, fmt.Errorf("replication failed (%d/%d succeeded): %w",
			successCount, len(nodes), lastError)
	}
	
	return nil, fmt.Errorf("replication failed (%d/%d succeeded)",
		successCount, len(nodes))
}

// getReplicationTargets returns N nodes for replication based on partition key
func (rc *ReplicationCoordinator) getReplicationTargets(partitionKey string) ([]*ring.Node, error) {
	// Get primary node for this partition key
	primaryNode, err := rc.ring.GetNode(partitionKey)
	if err != nil {
		return nil, fmt.Errorf("get primary node: %w", err)
	}
	
	// If replication factor is 1, return only primary
	if rc.replicationFactor == 1 {
		return []*ring.Node{primaryNode}, nil
	}
	
	// Get all healthy nodes for replication
	allNodes := rc.ring.GetHealthyNodes()
	if len(allNodes) == 0 {
		// Fall back to all nodes if no healthy nodes
		allNodes = rc.ring.GetAllNodes()
	}
	
	// If we don't have enough nodes, use what we have
	if len(allNodes) < rc.replicationFactor {
		slog.Warn("not enough nodes for replication",
			"available", len(allNodes),
			"required", rc.replicationFactor,
		)
		return allNodes, nil
	}
	
	// Find primary node in the list
	primaryIdx := -1
	for i, node := range allNodes {
		if node.ID == primaryNode.ID {
			primaryIdx = i
			break
		}
	}
	
	if primaryIdx == -1 {
		// Primary not found in healthy nodes, add it
		allNodes = append([]*ring.Node{primaryNode}, allNodes...)
		primaryIdx = 0
	}
	
	// Select N consecutive nodes starting from primary (consistent hashing ring walk)
	targets := make([]*ring.Node, 0, rc.replicationFactor)
	for i := 0; i < rc.replicationFactor; i++ {
		idx := (primaryIdx + i) % len(allNodes)
		targets = append(targets, allNodes[idx])
	}
	
	return targets, nil
}
