package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/backup"
	"github.com/chronicblondiee/duckdb-cluster/internal/cluster"
	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/distributor"
	"github.com/chronicblondiee/duckdb-cluster/internal/frontend"
	"github.com/chronicblondiee/duckdb-cluster/internal/ingester"
	"github.com/chronicblondiee/duckdb-cluster/internal/migration"
	"github.com/chronicblondiee/duckdb-cluster/internal/module"
	"github.com/chronicblondiee/duckdb-cluster/internal/modules"
	"github.com/chronicblondiee/duckdb-cluster/internal/observability"
	"github.com/chronicblondiee/duckdb-cluster/internal/querier"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "start":
		cmdStart(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  init    Initialize a new cluster")
	fmt.Fprintln(os.Stderr, "  start   Start the cluster server")
	fmt.Fprintln(os.Stderr, "  status  Check cluster status")
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	shards := fs.Int("shards", 3, "number of shards")
	dataDir := fs.String("data-dir", "./data", "data directory")
	configPath := fs.String("config", "config.yaml", "config file to create")
	fs.Parse(args)

	// Create config
	cfg := config.Default()
	cfg.Common.DataDir = *dataDir
	cfg.Common.NumShards = *shards

	// Initialize cluster using old method for backward compatibility
	legacyCfg := &cluster.Config{
		DataDir:    *dataDir,
		NumShards:  *shards,
		ListenAddr: cfg.Server.HTTPListenAddr,
	}

	c, err := cluster.NewCluster(legacyCfg)
	if err != nil {
		slog.Error("failed to create cluster", "error", err)
		os.Exit(1)
	}

	if err := c.Init(); err != nil {
		slog.Error("failed to init cluster", "error", err)
		os.Exit(1)
	}

	// Save new YAML config
	if err := cfg.Save(*configPath); err != nil {
		slog.Error("failed to save config", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Cluster initialized with %d shards in %s\n", *shards, *dataDir)
	fmt.Printf("Configuration saved to %s\n", *configPath)
}

func cmdStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	configPath := fs.String("config", "config.yaml", "path to config file")
	target := fs.String("target", "", "target to run (all, write, read, backend)")
	addr := fs.String("addr", "", "listen address (overrides config)")
	dataDir := fs.String("data-dir", "", "data directory (overrides config)")
	fs.Parse(args)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Apply overrides
	if *target != "" {
		cfg.Target = *target
	}
	if *addr != "" {
		cfg.Server.HTTPListenAddr = *addr
	}
	if *dataDir != "" {
		cfg.Common.DataDir = *dataDir
	}

	// Start the cluster using module system
	if err := startCluster(cfg); err != nil {
		slog.Error("failed to start cluster", "error", err)
		os.Exit(1)
	}
}

func startCluster(cfg *config.Config) error {
	// Initialize observability
	logger := observability.NewLogger(
		observability.ParseLevel(cfg.Observability.Logging.Level),
		cfg.Observability.Logging.Format,
	)
	
	logger.Info("starting duckdb-cluster", "target", cfg.Target, "data_dir", cfg.Common.DataDir)
	
	// Initialize metrics
	metrics := observability.NewMetrics(cfg.Observability.Metrics.Namespace)
	metrics.ShardCount.Set(float64(cfg.Common.NumShards))
	metrics.ReplicationFactor.Set(float64(cfg.Distributor.ReplicationFactor))
	
	// Initialize tracing if enabled
	if cfg.Observability.Tracing.Enabled {
		tp, err := observability.NewTracerProvider(observability.TracingConfig{
			Enabled:      cfg.Observability.Tracing.Enabled,
			OTLPEndpoint: cfg.Observability.Tracing.OTLPEndpoint,
			ServiceName:  cfg.Observability.Tracing.ServiceName,
			Environment:  cfg.Observability.Tracing.Environment,
			InstanceID:   cfg.Ring.InstanceID,
			SampleRate:   cfg.Observability.Tracing.SampleRate,
		}, logger.Logger)
		if err != nil {
			logger.Error("failed to initialize tracing", "error", err)
		} else {
			// Schedule tracer shutdown (will be called when process exits)
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := tp.Shutdown(shutdownCtx); err != nil {
					logger.Error("failed to shutdown tracer", "error", err)
				}
			}()
			logger.Info("tracing initialized", "endpoint", cfg.Observability.Tracing.OTLPEndpoint)
		}
	}

	// Create module manager
	mgr := module.NewManager()

	// Create components
	ing := ingester.NewIngester(cfg)
	quer := querier.NewQuerier(cfg)
	dist := distributor.NewDistributor(cfg, ing, metrics, logger)
	fe := frontend.NewQueryFrontend(cfg, quer)

	// Create cluster facade for API compatibility
	clust := &cluster.Cluster{
		Config: &cluster.Config{
			DataDir:    cfg.Common.DataDir,
			NumShards:  cfg.Common.NumShards,
			ListenAddr: cfg.Server.HTTPListenAddr,
		},
	}

	// In monolithic mode, we need to initialize the ingester first
	// to populate the cluster's Manager and Router
	ctx := context.Background()
	if err := ing.Init(ctx); err != nil {
		return fmt.Errorf("failed to init ingester: %w", err)
	}
	clust.Manager = ing.GetManager()
	clust.Router = ing.GetRouter()

	// Create backup manager (shared between server and migration manager)
	backupManager, err := backup.NewBackupManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create backup manager: %w", err)
	}

	// Create migration manager and run pending migrations on startup
	migrationMgr := migration.NewManager(cfg.Common.DataDir, ing.GetManager(), backupManager)
	registerMigrations(migrationMgr)

	migrationResult, err := migrationMgr.RunPending(ctx)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	if len(migrationResult.Applied) > 0 {
		logger.Info("migrations applied on startup",
			"count", len(migrationResult.Applied),
			"backup_id", migrationResult.BackupID)
	}

	// Register modules
	mgr.Register(modules.NewIngesterModule(cfg, ing))
	mgr.Register(modules.NewQuerierModule(cfg, quer))
	mgr.Register(modules.NewDistributorModule(cfg, dist))
	mgr.Register(modules.NewQueryFrontendModule(cfg, fe))
	mgr.Register(modules.NewAdminModule(cfg))
	mgr.Register(modules.NewCompactorModule(cfg))
	serverMod := modules.NewServerModule(cfg, clust)
	serverMod.SetMigrationManager(migrationMgr)
	mgr.Register(serverMod)

	// Start modules based on target
	if err := mgr.Start(ctx, cfg.Target); err != nil {
		return fmt.Errorf("failed to start modules: %w", err)
	}

	slog.Info("cluster started successfully", "target", cfg.Target)

	// Block forever (modules handle their own shutdown)
	select {}
}

// registerMigrations registers all known schema migrations.
// Add new migrations here as the schema evolves.
func registerMigrations(mgr *migration.Manager) {
	// Example:
	// mgr.Register(migration.MigrationDef{
	//     ID:          "001_add_created_at",
	//     Description: "Add created_at column to events table",
	//     Up: func(ctx context.Context, s *shard.Shard) error {
	//         _, err := s.Execute(ctx, "ALTER TABLE events ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now()")
	//         return err
	//     },
	// })
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/health", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status cluster.ClusterStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Status: %s\nShards: %d\n", status.Status, status.ShardCount)
}
