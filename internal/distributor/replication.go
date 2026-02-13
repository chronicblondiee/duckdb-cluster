package distributor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/observability"
	"github.com/chronicblondiee/duckdb-cluster/internal/ring"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ReplicationCoordinator handles replication of writes to multiple ingesters
type ReplicationCoordinator struct {
	ring              *ring.Ring
	replicationFactor int
	metrics           *observability.Metrics
}

// NewReplicationCoordinator creates a new replication coordinator
func NewReplicationCoordinator(r *ring.Ring, replicationFactor int, metrics *observability.Metrics) *ReplicationCoordinator {
	return &ReplicationCoordinator{
		ring:              r,
		replicationFactor: replicationFactor,
		metrics:           metrics,
	}
}

// ReplicateWrite sends a write to N ingesters based on replication factor
func (rc *ReplicationCoordinator) ReplicateWrite(
	ctx context.Context,
	req *PushRequest,
	pushFunc func(ctx context.Context, nodeID string, req *PushRequest) (*PushResponse, error),
) (*PushResponse, error) {
	// Start tracing span
	ctx, span := observability.StartSpan(ctx, "distributor", "ReplicateWrite",
		trace.WithAttributes(
			attribute.String("partition_key", req.PartitionKey),
			attribute.Int("replication_factor", rc.replicationFactor),
		))
	defer span.End()
	
	// Get target nodes for replication
	nodes, err := rc.getReplicationTargets(req.PartitionKey)
	if err != nil {
		observability.RecordError(ctx, err)
		return nil, fmt.Errorf("get replication targets: %w", err)
	}
	
	observability.AddSpanAttributes(ctx, attribute.Int("target_nodes", len(nodes)))
	
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
		duration time.Duration
	}
	
	resultCh := make(chan result, len(nodes))
	var wg sync.WaitGroup
	
	for _, node := range nodes {
		wg.Add(1)
		go func(n *ring.Node) {
			defer wg.Done()
			
			start := time.Now()
			resp, err := pushFunc(ctx, n.ID, req)
			duration := time.Since(start)
			
			resultCh <- result{
				nodeID:   n.ID,
				response: resp,
				err:      err,
				duration: duration,
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
		// Record replication latency
		if rc.metrics != nil {
			rc.metrics.ReplicationLatency.WithLabelValues(res.nodeID).Observe(res.duration.Seconds())
		}
		
		if res.err != nil {
			slog.Warn("replication failed",
				"node_id", res.nodeID,
				"error", res.err,
			)
			if rc.metrics != nil {
				rc.metrics.ReplicationFailure.WithLabelValues(res.nodeID, "error").Inc()
			}
			lastError = res.err
			errors = append(errors, res.err)
			observability.AddSpanEvent(ctx, "replication_failed", 
				attribute.String("node_id", res.nodeID),
				attribute.String("error", res.err.Error()))
			continue
		}
		
		if res.response.Error != "" {
			err := fmt.Errorf("node %s returned error: %s", res.nodeID, res.response.Error)
			slog.Warn("replication returned error",
				"node_id", res.nodeID,
				"error", res.response.Error,
			)
			if rc.metrics != nil {
				rc.metrics.ReplicationFailure.WithLabelValues(res.nodeID, "response_error").Inc()
			}
			lastError = err
			errors = append(errors, err)
			observability.AddSpanEvent(ctx, "replication_error", 
				attribute.String("node_id", res.nodeID),
				attribute.String("error", res.response.Error))
			continue
		}
		
		successCount++
		if firstSuccess == nil {
			firstSuccess = res.response
		}
		
		if rc.metrics != nil {
			rc.metrics.ReplicationSuccess.WithLabelValues(res.nodeID).Inc()
		}
		
		slog.Debug("replication succeeded",
			"node_id", res.nodeID,
			"rows_affected", res.response.RowsAffected,
		)
		observability.AddSpanEvent(ctx, "replication_succeeded", 
			attribute.String("node_id", res.nodeID),
			attribute.Int64("rows_affected", res.response.RowsAffected))
	}
	
	// Determine if replication was successful
	// We need at least a quorum (majority) of replicas to succeed
	quorum := (rc.replicationFactor / 2) + 1
	
	observability.AddSpanAttributes(ctx,
		attribute.Int("success_count", successCount),
		attribute.Int("required_quorum", quorum),
		attribute.Int("error_count", len(errors)))
	
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
	
	replicationErr := fmt.Errorf("replication failed (%d/%d succeeded)", successCount, len(nodes))
	if lastError != nil {
		replicationErr = fmt.Errorf("replication failed (%d/%d succeeded): %w",
			successCount, len(nodes), lastError)
	}
	
	observability.RecordError(ctx, replicationErr)
	return nil, replicationErr
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
