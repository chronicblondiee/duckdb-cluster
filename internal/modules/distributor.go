package modules

import (
	"context"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/distributor"
	"github.com/chronicblondiee/duckdb-cluster/internal/module"
)

// DistributorModule wraps the distributor component
type DistributorModule struct {
	cfg         *config.Config
	distributor *distributor.Distributor
}

// NewDistributorModule creates a new distributor module
func NewDistributorModule(cfg *config.Config, dist *distributor.Distributor) *DistributorModule {
	return &DistributorModule{
		cfg:         cfg,
		distributor: dist,
	}
}

func (d *DistributorModule) Name() string {
	return "distributor"
}

func (d *DistributorModule) Dependencies() []string {
	// Distributor depends on ingester being available
	return []string{"ingester"}
}

func (d *DistributorModule) Init(ctx context.Context) error {
	slog.Info("initializing distributor module")
	return nil
}

func (d *DistributorModule) Start(ctx context.Context) error {
	slog.Info("starting distributor module")
	return nil
}

func (d *DistributorModule) Stop() error {
	slog.Info("stopping distributor module")
	return nil
}

// Verify interface implementation
var _ module.Module = (*DistributorModule)(nil)
