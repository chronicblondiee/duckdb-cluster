package frontend

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/brown/duckdb-cluster/internal/config"
	"github.com/brown/duckdb-cluster/internal/querier"
)

// QuerierClient is the interface for executing queries on queriers
type QuerierClient interface {
	Query(ctx context.Context, req *querier.QueryRequest) (*querier.QueryResponse, error)
}

// QueryFrontend handles query coordination and optimization
type QueryFrontend struct {
	cfg     *config.Config
	querier QuerierClient
}

// NewQueryFrontend creates a new query frontend
func NewQueryFrontend(cfg *config.Config, querierClient QuerierClient) *QueryFrontend {
	return &QueryFrontend{
		cfg:     cfg,
		querier: querierClient,
	}
}

// Query handles a query request with retries and timeout
func (f *QueryFrontend) Query(ctx context.Context, sql string) (*querier.QueryResponse, error) {
	slog.Debug("query frontend processing query", "sql", sql)
	
	// Apply query timeout
	ctx, cancel := context.WithTimeout(ctx, f.cfg.QueryFrontend.QueryTimeout)
	defer cancel()
	
	// Execute query with retries
	var lastErr error
	for attempt := 0; attempt <= f.cfg.QueryFrontend.MaxRetries; attempt++ {
		if attempt > 0 {
			slog.Debug("retrying query", "attempt", attempt, "sql", sql)
			// Small backoff between retries
			select {
			case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		
		req := &querier.QueryRequest{
			SQL:      sql,
			ShardIDs: []int{}, // Query all shards
		}
		
		resp, err := f.querier.Query(ctx, req)
		if err != nil {
			lastErr = err
			continue
		}
		
		// Check if query itself returned an error
		if resp.Error != "" {
			lastErr = fmt.Errorf("query error: %s", resp.Error)
			continue
		}
		
		// Success
		return resp, nil
	}
	
	return nil, fmt.Errorf("query failed after %d attempts: %w", 
		f.cfg.QueryFrontend.MaxRetries+1, lastErr)
}

// TODO: Future enhancements
// - Query splitting for large queries
// - Result caching
// - Query queuing and rate limiting
