package modules

import (
	"context"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/ingester"
	"github.com/chronicblondiee/duckdb-cluster/internal/module"
)

// IngesterModule wraps the ingester component
type IngesterModule struct {
	cfg      *config.Config
	ingester *ingester.Ingester
}

// NewIngesterModule creates a new ingester module
func NewIngesterModule(cfg *config.Config, ing *ingester.Ingester) *IngesterModule {
	return &IngesterModule{
		cfg:      cfg,
		ingester: ing,
	}
}

func (i *IngesterModule) Name() string {
	return "ingester"
}

func (i *IngesterModule) Dependencies() []string {
	return []string{} // No dependencies
}

func (i *IngesterModule) Init(ctx context.Context) error {
	slog.Info("initializing ingester module")
	return i.ingester.Init(ctx)
}

func (i *IngesterModule) Start(ctx context.Context) error {
	slog.Info("starting ingester module")
	// Ingester is ready after Init
	return nil
}

func (i *IngesterModule) Stop() error {
	slog.Info("stopping ingester module")
	return i.ingester.Shutdown()
}

// Verify interface implementation
var _ module.Module = (*IngesterModule)(nil)
