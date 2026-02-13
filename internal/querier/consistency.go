package querier

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/brown/duckdb-cluster/internal/ring"
)

// ConsistencyLevel defines read consistency levels
type ConsistencyLevel string

const (
	// ConsistencyOne reads from one replica (fastest, least consistent)
	ConsistencyOne ConsistencyLevel = "one"
	
	// ConsistencyQuorum reads from a quorum (majority) of replicas
	ConsistencyQuorum ConsistencyLevel = "quorum"
	
	// ConsistencyAll reads from all replicas (slowest, most consistent)
	ConsistencyAll ConsistencyLevel = "all"
)

// ConsistencyCoordinator handles consistent reads across multiple queriers
type ConsistencyCoordinator struct {
	ring              *ring.Ring
	consistencyLevel  ConsistencyLevel
	replicationFactor int
}

// NewConsistencyCoordinator creates a new consistency coordinator
func NewConsistencyCoordinator(r *ring.Ring, level ConsistencyLevel, replicationFactor int) *ConsistencyCoordinator {
	return &ConsistencyCoordinator{
		ring:              r,
		consistencyLevel:  level,
		replicationFactor: replicationFactor,
	}
}

// ConsistentQuery executes a query with the configured consistency level
func (cc *ConsistencyCoordinator) ConsistentQuery(
	ctx context.Context,
	req *QueryRequest,
	queryFunc func(ctx context.Context, nodeID string, req *QueryRequest) (*QueryResponse, error),
) (*QueryResponse, error) {
	// Determine how many replicas we need to query
	requiredReplicas := cc.getRequiredReplicas()
	
	slog.Debug("executing consistent query",
		"consistency_level", cc.consistencyLevel,
		"required_replicas", requiredReplicas,
	)
	
	// Get target nodes
	nodes := cc.getQueryTargets()
	if len(nodes) == 0 {
		return &QueryResponse{
			Error: "no available nodes for query",
		}, nil
	}
	
	// Limit to required replicas
	if len(nodes) > requiredReplicas {
		nodes = nodes[:requiredReplicas]
	}
	
	// If only one node, use simple path
	if len(nodes) == 1 {
		return queryFunc(ctx, nodes[0].ID, req)
	}
	
	// Query multiple nodes in parallel
	type result struct {
		nodeID   string
		response *QueryResponse
		err      error
	}
	
	resultCh := make(chan result, len(nodes))
	var wg sync.WaitGroup
	
	for _, node := range nodes {
		wg.Add(1)
		go func(n *ring.Node) {
			defer wg.Done()
			
			resp, err := queryFunc(ctx, n.ID, req)
			resultCh <- result{
				nodeID:   n.ID,
				response: resp,
				err:      err,
			}
		}(node)
	}
	
	// Wait for all queries to complete
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	
	// Collect results
	var (
		successCount int
		firstSuccess *QueryResponse
		lastError    error
	)
	
	for res := range resultCh {
		if res.err != nil {
			slog.Warn("query failed on node",
				"node_id", res.nodeID,
				"error", res.err,
			)
			lastError = res.err
			continue
		}
		
		if res.response.Error != "" {
			slog.Warn("query returned error from node",
				"node_id", res.nodeID,
				"error", res.response.Error,
			)
			lastError = fmt.Errorf("node %s: %s", res.nodeID, res.response.Error)
			continue
		}
		
		successCount++
		if firstSuccess == nil {
			firstSuccess = res.response
		}
		
		slog.Debug("query succeeded on node",
			"node_id", res.nodeID,
			"row_count", len(res.response.Rows),
		)
	}
	
	// Check if we got enough successful responses
	if successCount >= requiredReplicas {
		slog.Debug("consistent query completed",
			"successful_replicas", successCount,
			"required", requiredReplicas,
		)
		
		// For now, return the first successful response
		// In the future, we could implement read repair or result comparison
		return firstSuccess, nil
	}
	
	// Not enough successful responses
	slog.Error("consistent query failed - insufficient successful replicas",
		"successful", successCount,
		"required", requiredReplicas,
		"total_queried", len(nodes),
	)
	
	if lastError != nil {
		return nil, fmt.Errorf("query failed (%d/%d succeeded): %w",
			successCount, len(nodes), lastError)
	}
	
	return nil, fmt.Errorf("query failed (%d/%d succeeded)",
		successCount, len(nodes))
}

// getRequiredReplicas returns how many replicas to query based on consistency level
func (cc *ConsistencyCoordinator) getRequiredReplicas() int {
	switch cc.consistencyLevel {
	case ConsistencyOne:
		return 1
	case ConsistencyQuorum:
		// Quorum is majority: (N/2) + 1
		return (cc.replicationFactor / 2) + 1
	case ConsistencyAll:
		return cc.replicationFactor
	default:
		slog.Warn("unknown consistency level, defaulting to ONE",
			"level", cc.consistencyLevel,
		)
		return 1
	}
}

// getQueryTargets returns healthy nodes to query
func (cc *ConsistencyCoordinator) getQueryTargets() []*ring.Node {
	// Get healthy nodes first
	nodes := cc.ring.GetHealthyNodes()
	
	// If no healthy nodes, fall back to all nodes
	if len(nodes) == 0 {
		slog.Warn("no healthy nodes available, using all nodes")
		nodes = cc.ring.GetAllNodes()
	}
	
	return nodes
}

// ParseConsistencyLevel parses a consistency level string
func ParseConsistencyLevel(level string) (ConsistencyLevel, error) {
	switch level {
	case "one":
		return ConsistencyOne, nil
	case "quorum":
		return ConsistencyQuorum, nil
	case "all":
		return ConsistencyAll, nil
	default:
		return "", fmt.Errorf("invalid consistency level: %s (must be: one, quorum, all)", level)
	}
}
