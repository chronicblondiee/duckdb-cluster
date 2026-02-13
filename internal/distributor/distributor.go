package distributor

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/brown/duckdb-cluster/internal/config"
)

// IngesterClient is the interface for pushing writes to ingesters
type IngesterClient interface {
	Push(ctx context.Context, req *PushRequest) (*PushResponse, error)
}

// PushRequest represents a write request
type PushRequest struct {
	SQL          string
	PartitionKey string
	TenantID     string
}

// PushResponse represents the result of a write
type PushResponse struct {
	RowsAffected int64
	ShardID      int
	Error        string
}

// Distributor validates writes and routes them to the appropriate ingester
type Distributor struct {
	cfg         *config.Config
	ingester    IngesterClient
	replicator  *ReplicationCoordinator
	pushToNode  func(ctx context.Context, nodeID string, req *PushRequest) (*PushResponse, error)
}

// NewDistributor creates a new distributor
func NewDistributor(cfg *config.Config, ingester IngesterClient) *Distributor {
	d := &Distributor{
		cfg:      cfg,
		ingester: ingester,
	}
	
	// Set default push function (local ingester)
	d.pushToNode = func(ctx context.Context, nodeID string, req *PushRequest) (*PushResponse, error) {
		return ingester.Push(ctx, req)
	}
	
	return d
}

// SetReplicator sets the replication coordinator for distributed writes
func (d *Distributor) SetReplicator(replicator *ReplicationCoordinator) {
	d.replicator = replicator
}

// SetPushFunc sets the function used to push to specific nodes (for distributed mode)
func (d *Distributor) SetPushFunc(pushFunc func(ctx context.Context, nodeID string, req *PushRequest) (*PushResponse, error)) {
	d.pushToNode = pushFunc
}

// Push validates and forwards a write request to the appropriate ingester(s)
func (d *Distributor) Push(ctx context.Context, req *PushRequest) (*PushResponse, error) {
	// Validate request
	if err := d.validateRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	
	slog.Debug("distributor pushing write", "sql", req.SQL, "partition_key", req.PartitionKey)
	
	// If replication is enabled, use replication coordinator
	if d.replicator != nil && d.cfg.Distributor.ReplicationFactor > 1 {
		return d.replicator.ReplicateWrite(ctx, req, d.pushToNode)
	}
	
	// Otherwise, forward to local ingester (single-node or no replication)
	resp, err := d.ingester.Push(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ingester push failed: %w", err)
	}
	
	return resp, nil
}

// validateRequest checks that the write request is valid
func (d *Distributor) validateRequest(req *PushRequest) error {
	if req.SQL == "" {
		return fmt.Errorf("SQL cannot be empty")
	}
	
	if len(req.SQL) > d.cfg.Distributor.MaxQueryLength {
		return fmt.Errorf("SQL too long: %d bytes (max %d)", 
			len(req.SQL), d.cfg.Distributor.MaxQueryLength)
	}
	
	if req.PartitionKey == "" {
		return fmt.Errorf("partition_key is required for writes")
	}
	
	return nil
}
