package ingester

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/distributor"
	"github.com/chronicblondiee/duckdb-cluster/internal/router"
	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

// Ingester owns shards and handles write requests
type Ingester struct {
	cfg     *config.Config
	manager *shard.Manager
	router  *router.Router
}

// NewIngester creates a new ingester
func NewIngester(cfg *config.Config) *Ingester {
	return &Ingester{
		cfg: cfg,
	}
}

// Init initializes the ingester (opens shard files)
func (i *Ingester) Init(ctx context.Context) error {
	slog.Info("initializing ingester", "data_dir", i.cfg.Common.DataDir)
	
	// Initialize shard manager
	manager := &shard.Manager{DataDir: i.cfg.Common.DataDir}
	if err := manager.OpenAll(); err != nil {
		return fmt.Errorf("open shards: %w", err)
	}
	
	i.manager = manager
	i.router = router.NewRouter(manager)
	
	slog.Info("ingester initialized", "shard_count", manager.ShardCount())
	return nil
}

// Push handles a write request
func (i *Ingester) Push(ctx context.Context, req *distributor.PushRequest) (*distributor.PushResponse, error) {
	if i.manager == nil {
		return nil, fmt.Errorf("ingester not initialized")
	}
	
	slog.Debug("ingester executing write", "sql", req.SQL, "partition_key", req.PartitionKey)
	
	// Route the write through the router
	result, err := i.router.Route(ctx, req.SQL, req.PartitionKey)
	if err != nil {
		return &distributor.PushResponse{
			Error: err.Error(),
		}, nil
	}
	
	return &distributor.PushResponse{
		RowsAffected: result.RowsAffected,
		ShardID:      result.ShardID,
	}, nil
}

// Query handles a read request (for monolithic mode or local queries)
func (i *Ingester) Query(ctx context.Context, sql string) (*router.QueryResult, error) {
	if i.manager == nil {
		return nil, fmt.Errorf("ingester not initialized")
	}
	
	return i.router.Route(ctx, sql, "")
}

// Shutdown closes all shards
func (i *Ingester) Shutdown() error {
	slog.Info("shutting down ingester")
	if i.manager != nil {
		return i.manager.CloseAll()
	}
	return nil
}

// GetManager returns the shard manager (for direct access in monolithic mode)
func (i *Ingester) GetManager() *shard.Manager {
	return i.manager
}

// GetRouter returns the router (for direct access in monolithic mode)
func (i *Ingester) GetRouter() *router.Router {
	return i.router
}
