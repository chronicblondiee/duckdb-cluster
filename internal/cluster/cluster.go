package cluster

import (
	"fmt"
	"log/slog"

	"github.com/brown/duckdb-cluster/internal/router"
	"github.com/brown/duckdb-cluster/internal/shard"
)

type ClusterStatus struct {
	Status     string `json:"status"`
	ShardCount int    `json:"shard_count"`
}

type Cluster struct {
	Config  *Config
	Manager *shard.Manager
	Router  *router.Router
}

func NewCluster(cfg *Config) (*Cluster, error) {
	return &Cluster{Config: cfg}, nil
}

func (c *Cluster) Init() error {
	slog.Info("initializing cluster", "shards", c.Config.NumShards, "data_dir", c.Config.DataDir)
	m, err := shard.NewManager(c.Config.DataDir, c.Config.NumShards)
	if err != nil {
		return fmt.Errorf("init shards: %w", err)
	}
	c.Manager = m
	c.Router = router.NewRouter(m)

	if err := c.Config.Save("cluster.json"); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Close shards after init — they'll be reopened on start
	if err := m.CloseAll(); err != nil {
		return fmt.Errorf("close shards after init: %w", err)
	}

	slog.Info("cluster initialized", "shards", c.Config.NumShards)
	return nil
}

func (c *Cluster) Start() error {
	slog.Info("starting cluster", "data_dir", c.Config.DataDir)
	m := &shard.Manager{DataDir: c.Config.DataDir}
	if err := m.OpenAll(); err != nil {
		return fmt.Errorf("open shards: %w", err)
	}
	c.Manager = m
	c.Router = router.NewRouter(m)
	slog.Info("cluster started", "shards", m.ShardCount())
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
	if c.Manager != nil {
		return c.Manager.CloseAll()
	}
	return nil
}
