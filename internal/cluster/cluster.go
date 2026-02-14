package cluster

import (
	"fmt"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
	"github.com/chronicblondiee/duckdb-cluster/internal/router"
	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

type ClusterStatus struct {
	Status     string `json:"status"`
	ShardCount int    `json:"shard_count"`
}

type Cluster struct {
	Config   *Config
	Manager  *shard.Manager
	Router   *router.Router
	Registry *index.Registry
}

func NewCluster(cfg *Config) (*Cluster, error) {
	return &Cluster{Config: cfg}, nil
}

func (c *Cluster) Init() error {
	slog.Info("initializing cluster", "shards", c.Config.NumShards, "data_dir", c.Config.DataDir)

	// Create registry and _default index
	reg := index.NewRegistry(c.Config.DataDir)
	settings := index.Settings{
		ShardCount:        c.Config.NumShards,
		PartitionKeyField: "_id",
	}
	idx, err := reg.Create("_default", settings)
	if err != nil {
		return fmt.Errorf("create _default index: %w", err)
	}
	c.Registry = reg
	c.Manager = idx.Manager
	c.Router = idx.Router

	if err := c.Config.Save("cluster.json"); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Close shards after init — they'll be reopened on start
	if err := reg.CloseAll(); err != nil {
		return fmt.Errorf("close shards after init: %w", err)
	}

	slog.Info("cluster initialized", "shards", c.Config.NumShards)
	return nil
}

func (c *Cluster) Start() error {
	slog.Info("starting cluster", "data_dir", c.Config.DataDir)

	reg := index.NewRegistry(c.Config.DataDir)
	if err := reg.LoadAll(); err != nil {
		return fmt.Errorf("load indices: %w", err)
	}
	c.Registry = reg

	// Set Manager/Router from _default index for backward compat
	defaultIdx, err := reg.Get("_default")
	if err != nil {
		slog.Warn("no _default index found, falling back to empty manager")
		c.Manager = &shard.Manager{DataDir: c.Config.DataDir}
		c.Router = router.NewRouter(c.Manager)
	} else {
		c.Manager = defaultIdx.Manager
		c.Router = defaultIdx.Router
		slog.Info("cluster started", "shards", defaultIdx.Manager.ShardCount())
	}

	return nil
}

func (c *Cluster) Status() (*ClusterStatus, error) {
	count := 0
	if c.Manager != nil {
		count = c.Manager.ShardCount()
	}
	return &ClusterStatus{
		Status:     "healthy",
		ShardCount: count,
	}, nil
}

func (c *Cluster) Shutdown() error {
	slog.Info("shutting down cluster")
	if c.Registry != nil {
		return c.Registry.CloseAll()
	}
	if c.Manager != nil {
		return c.Manager.CloseAll()
	}
	return nil
}
