package modules

import (
	"context"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/module"
	"github.com/chronicblondiee/duckdb-cluster/internal/querier"
)

// QuerierModule wraps the querier component
type QuerierModule struct {
	cfg     *config.Config
	querier *querier.Querier
}

// NewQuerierModule creates a new querier module
func NewQuerierModule(cfg *config.Config, q *querier.Querier) *QuerierModule {
	return &QuerierModule{
		cfg:     cfg,
		querier: q,
	}
}

func (q *QuerierModule) Name() string {
	return "querier"
}

func (q *QuerierModule) Dependencies() []string {
	return []string{} // No dependencies
}

func (q *QuerierModule) Init(ctx context.Context) error {
	slog.Info("initializing querier module")
	return q.querier.Init(ctx)
}

func (q *QuerierModule) Start(ctx context.Context) error {
	slog.Info("starting querier module")
	return nil
}

func (q *QuerierModule) Stop() error {
	slog.Info("stopping querier module")
	return q.querier.Shutdown()
}

// Verify interface implementation
var _ module.Module = (*QuerierModule)(nil)
