package distributor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	metrics     *observability.Metrics
	logger      *observability.Logger
}

// NewDistributor creates a new distributor
func NewDistributor(cfg *config.Config, ingester IngesterClient, metrics *observability.Metrics, logger *observability.Logger) *Distributor {
	d := &Distributor{
		cfg:      cfg,
		ingester: ingester,
		metrics:  metrics,
		logger:   logger,
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
	// Start tracing span
	ctx, span := observability.StartSpan(ctx, "distributor", "Distributor.Push",
		trace.WithAttributes(
			attribute.String("partition_key", req.PartitionKey),
			attribute.Int("sql_length", len(req.SQL)),
		))
	defer span.End()
	
	// Start timing for metrics
	startTime := time.Now()
	
	// Validate request
	if err := d.validateRequest(req); err != nil {
		d.metrics.WriteErrors.WithLabelValues("push", "validation").Inc()
		d.metrics.WriteTotal.WithLabelValues("push", "error").Inc()
		observability.RecordError(ctx, err)
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	
	if d.logger != nil {
		d.logger.DebugContext(ctx, "distributor pushing write", 
			"sql_length", len(req.SQL), 
			"partition_key", req.PartitionKey)
	} else {
		slog.Debug("distributor pushing write", "sql", req.SQL, "partition_key", req.PartitionKey)
	}
	
	var resp *PushResponse
	var err error
	
	// If replication is enabled, use replication coordinator
	if d.replicator != nil && d.cfg.Distributor.ReplicationFactor > 1 {
		observability.AddSpanAttributes(ctx, attribute.Bool("replicated", true))
		resp, err = d.replicator.ReplicateWrite(ctx, req, d.pushToNode)
	} else {
		// Otherwise, forward to local ingester (single-node or no replication)
		observability.AddSpanAttributes(ctx, attribute.Bool("replicated", false))
		resp, err = d.ingester.Push(ctx, req)
		if err != nil {
			err = fmt.Errorf("ingester push failed: %w", err)
		}
	}
	
	// Record metrics
	duration := time.Since(startTime).Seconds()
	d.metrics.WriteLatency.WithLabelValues("push", fmt.Sprintf("%d", resp.ShardID)).Observe(duration)
	
	if err != nil {
		d.metrics.WriteErrors.WithLabelValues("push", "ingester").Inc()
		d.metrics.WriteTotal.WithLabelValues("push", "error").Inc()
		observability.RecordError(ctx, err)
		return nil, err
	}
	
	d.metrics.WriteTotal.WithLabelValues("push", "success").Inc()
	observability.AddSpanAttributes(ctx, 
		attribute.Int64("rows_affected", resp.RowsAffected),
		attribute.Int("shard_id", resp.ShardID))
	
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
