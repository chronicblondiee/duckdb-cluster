package querier

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/brown/duckdb-cluster/internal/config"
	"github.com/brown/duckdb-cluster/internal/router"
	"github.com/brown/duckdb-cluster/internal/shard"
)

// QueryRequest represents a query to execute
type QueryRequest struct {
	SQL      string
	ShardIDs []int // If empty, query all shards
}

// QueryResponse represents query results
type QueryResponse struct {
	Columns []string
	Rows    []map[string]any
	Error   string
}

// Querier executes read queries against shards
type Querier struct {
	cfg         *config.Config
	manager     *shard.Manager
	router      *router.Router
	consistency *ConsistencyCoordinator
	queryNode   func(ctx context.Context, nodeID string, req *QueryRequest) (*QueryResponse, error)
}

// NewQuerier creates a new querier
func NewQuerier(cfg *config.Config) *Querier {
	q := &Querier{
		cfg: cfg,
	}
	
	// Set default query function (local query)
	q.queryNode = func(ctx context.Context, nodeID string, req *QueryRequest) (*QueryResponse, error) {
		return q.queryLocal(ctx, req)
	}
	
	return q
}

// SetConsistency sets the consistency coordinator for distributed reads
func (q *Querier) SetConsistency(consistency *ConsistencyCoordinator) {
	q.consistency = consistency
}

// SetQueryFunc sets the function used to query specific nodes (for distributed mode)
func (q *Querier) SetQueryFunc(queryFunc func(ctx context.Context, nodeID string, req *QueryRequest) (*QueryResponse, error)) {
	q.queryNode = queryFunc
}

// Init initializes the querier (opens shard files in read mode)
func (q *Querier) Init(ctx context.Context) error {
	slog.Info("initializing querier", "data_dir", q.cfg.Common.DataDir)
	
	// Initialize shard manager
	manager := &shard.Manager{DataDir: q.cfg.Common.DataDir}
	if err := manager.OpenAll(); err != nil {
		return fmt.Errorf("open shards: %w", err)
	}
	
	q.manager = manager
	q.router = router.NewRouter(manager)
	
	slog.Info("querier initialized", "shard_count", manager.ShardCount())
	return nil
}

// Query executes a read query
func (q *Querier) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	if q.manager == nil {
		return nil, fmt.Errorf("querier not initialized")
	}
	
	slog.Debug("querier executing query", "sql", req.SQL, "shard_ids", req.ShardIDs)
	
	// If consistency coordinator is set, use consistent reads
	if q.consistency != nil {
		return q.consistency.ConsistentQuery(ctx, req, q.queryNode)
	}
	
	// Otherwise, query locally
	return q.queryLocal(ctx, req)
}

// queryLocal executes a query on the local querier
func (q *Querier) queryLocal(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	// If specific shards are requested, query only those
	// Otherwise, let the router decide (which will fan-out to all for reads)
	result, err := q.router.Route(ctx, req.SQL, "")
	if err != nil {
		return &QueryResponse{
			Error: err.Error(),
		}, nil
	}
	
	return &QueryResponse{
		Columns: result.Columns,
		Rows:    result.Rows,
	}, nil
}

// Shutdown closes all shards
func (q *Querier) Shutdown() error {
	slog.Info("shutting down querier")
	if q.manager != nil {
		return q.manager.CloseAll()
	}
	return nil
}

// GetRouter returns the router (for direct access in monolithic mode)
func (q *Querier) GetRouter() *router.Router {
	return q.router
}
