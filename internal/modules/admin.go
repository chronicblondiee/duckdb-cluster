package modules

import (
	"context"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/module"
)

// AdminModule handles administrative operations
type AdminModule struct {
	cfg *config.Config
}

// NewAdminModule creates a new admin module
func NewAdminModule(cfg *config.Config) *AdminModule {
	return &AdminModule{cfg: cfg}
}

func (a *AdminModule) Name() string {
	return "admin"
}

func (a *AdminModule) Dependencies() []string {
	return []string{}
}

func (a *AdminModule) Init(ctx context.Context) error {
	slog.Info("initializing admin module")
	return nil
}

func (a *AdminModule) Start(ctx context.Context) error {
	slog.Info("starting admin module")
	return nil
}

func (a *AdminModule) Stop() error {
	slog.Info("stopping admin module")
	return nil
}

// Verify interface implementation
var _ module.Module = (*AdminModule)(nil)

// CompactorModule handles shard compaction
type CompactorModule struct {
	cfg *config.Config
}

// NewCompactorModule creates a new compactor module
func NewCompactorModule(cfg *config.Config) *CompactorModule {
	return &CompactorModule{cfg: cfg}
}

func (c *CompactorModule) Name() string {
	return "compactor"
}

func (c *CompactorModule) Dependencies() []string {
	return []string{}
}

func (c *CompactorModule) Init(ctx context.Context) error {
	slog.Info("initializing compactor module")
	return nil
}

func (c *CompactorModule) Start(ctx context.Context) error {
	slog.Info("starting compactor module")
	return nil
}

func (c *CompactorModule) Stop() error {
	slog.Info("stopping compactor module")
	return nil
}

// Verify interface implementation
var _ module.Module = (*CompactorModule)(nil)
