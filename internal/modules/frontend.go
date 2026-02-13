package modules

import (
	"context"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/frontend"
	"github.com/chronicblondiee/duckdb-cluster/internal/module"
)

// QueryFrontendModule wraps the query frontend component
type QueryFrontendModule struct {
	cfg      *config.Config
	frontend *frontend.QueryFrontend
}

// NewQueryFrontendModule creates a new query frontend module
func NewQueryFrontendModule(cfg *config.Config, fe *frontend.QueryFrontend) *QueryFrontendModule {
	return &QueryFrontendModule{
		cfg:      cfg,
		frontend: fe,
	}
}

func (q *QueryFrontendModule) Name() string {
	return "query-frontend"
}

func (q *QueryFrontendModule) Dependencies() []string {
	// Frontend depends on querier
	return []string{"querier"}
}

func (q *QueryFrontendModule) Init(ctx context.Context) error {
	slog.Info("initializing query-frontend module")
	return nil
}

func (q *QueryFrontendModule) Start(ctx context.Context) error {
	slog.Info("starting query-frontend module")
	return nil
}

func (q *QueryFrontendModule) Stop() error {
	slog.Info("stopping query-frontend module")
	return nil
}

// Verify interface implementation
var _ module.Module = (*QueryFrontendModule)(nil)
