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
	cfg      *config.Config
	ingester IngesterClient
}

// NewDistributor creates a new distributor
func NewDistributor(cfg *config.Config, ingester IngesterClient) *Distributor {
	return &Distributor{
		cfg:      cfg,
		ingester: ingester,
	}
}

// Push validates and forwards a write request to the appropriate ingester
func (d *Distributor) Push(ctx context.Context, req *PushRequest) (*PushResponse, error) {
	// Validate request
	if err := d.validateRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	
	slog.Debug("distributor pushing write", "sql", req.SQL, "partition_key", req.PartitionKey)
	
	// Forward to ingester (local or remote)
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
