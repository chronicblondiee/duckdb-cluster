package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
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
	case "version":
		cmdVersion(os.Args[2:])
	case "migrate":
		cmdMigrate(os.Args[2:])
	case "backup":
		cmdBackup(os.Args[2:])
	case "rebalance":
		cmdRebalance(os.Args[2:])
	case "index":
		cmdIndex(os.Args[2:])
	case "alias":
		cmdAlias(os.Args[2:])
	case "template":
		cmdTemplate(os.Args[2:])
	case "ism":
		cmdISM(os.Args[2:])
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
	fmt.Fprintln(os.Stderr, "  init      Initialize a new cluster")
	fmt.Fprintln(os.Stderr, "  start     Start the cluster server")
	fmt.Fprintln(os.Stderr, "  status    Check cluster status")
	fmt.Fprintln(os.Stderr, "  version   Show version and migration status")
	fmt.Fprintln(os.Stderr, "  migrate   Manage schema migrations (status, run)")
	fmt.Fprintln(os.Stderr, "  backup    Manage backups (list, create, restore, delete)")
	fmt.Fprintln(os.Stderr, "  rebalance Rebalance data across shards (plan, run, status)")
	fmt.Fprintln(os.Stderr, "  index     Manage indices (list, create, delete, get, close, open)")
	fmt.Fprintln(os.Stderr, "  alias     Manage index aliases (list, create, delete, get)")
	fmt.Fprintln(os.Stderr, "  template  Manage index templates (list, create, delete, get)")
	fmt.Fprintln(os.Stderr, "  ism       Manage ISM policies (list, create, delete, get, status, attach, detach, retry)")
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

func cmdVersion(args []string) {
	fs := flag.NewFlagSet("version", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/admin/version", *addr)
	resp, err := http.Get(url)
	if err != nil {
		// Server not running — print build-time version only
		fmt.Printf("duckdb-cluster %s (server not reachable)\n", migration.Version)
		return
	}
	defer resp.Body.Close()

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Binary version:  %v\n", status["binary_version"])
	fmt.Printf("State version:   %v\n", status["state_version"])
	fmt.Printf("Applied:         %v\n", formatFloat(status["applied_count"]))
	fmt.Printf("Pending:         %v\n", formatFloat(status["pending_count"]))
}

func cmdMigrate(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster migrate <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  status  Show migration status")
		fmt.Fprintln(os.Stderr, "  run     Run pending migrations")
		os.Exit(1)
	}

	switch args[0] {
	case "status":
		cmdMigrateStatus(args[1:])
	case "run":
		cmdMigrateRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown migrate subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdMigrateStatus(args []string) {
	fs := flag.NewFlagSet("migrate status", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/admin/version", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Binary version:  %v\n", status["binary_version"])
	fmt.Printf("State version:   %v\n", status["state_version"])
	fmt.Printf("Applied:         %v\n", formatFloat(status["applied_count"]))
	fmt.Printf("Pending:         %v\n", formatFloat(status["pending_count"]))

	if pending, ok := status["pending_migrations"].([]any); ok && len(pending) > 0 {
		fmt.Println("\nPending migrations:")
		for _, m := range pending {
			fmt.Printf("  - %v\n", m)
		}
	}

	if lastAt, ok := status["last_migration_at"].(string); ok && lastAt != "" && lastAt != "0001-01-01T00:00:00Z" {
		fmt.Printf("\nLast migration:  %s\n", lastAt)
	}
}

func cmdMigrateRun(args []string) {
	fs := flag.NewFlagSet("migrate run", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/admin/migrate", *addr)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result migration.MigrationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "Migration failed: %s\n", result.Error)
		if result.BackupID != "" {
			fmt.Fprintf(os.Stderr, "Backup available for restore: %s\n", result.BackupID)
		}
		os.Exit(1)
	}

	if len(result.Applied) == 0 {
		fmt.Println("No pending migrations.")
		return
	}

	fmt.Printf("Applied %d migration(s):\n", len(result.Applied))
	for _, id := range result.Applied {
		fmt.Printf("  - %s\n", id)
	}
	if result.BackupID != "" {
		fmt.Printf("\nPre-migration backup: %s\n", result.BackupID)
	}
	fmt.Printf("Version: %s -> %s\n", result.PrevVersion, result.NewVersion)
}

func cmdBackup(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster backup <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  list     List all backups")
		fmt.Fprintln(os.Stderr, "  create   Create a new backup")
		fmt.Fprintln(os.Stderr, "  restore  Restore from a backup")
		fmt.Fprintln(os.Stderr, "  delete   Delete a backup")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		cmdBackupList(args[1:])
	case "create":
		cmdBackupCreate(args[1:])
	case "restore":
		cmdBackupRestore(args[1:])
	case "delete":
		cmdBackupDelete(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown backup subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdBackupList(args []string) {
	fs := flag.NewFlagSet("backup list", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/admin/backups", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Backups []backup.BackupMetadata `json:"backups"`
		Count   int                     `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	if result.Count == 0 {
		fmt.Println("No backups found.")
		return
	}

	fmt.Printf("%-36s  %-12s  %-8s  %-6s  %s\n", "ID", "TYPE", "STATUS", "SHARDS", "TIMESTAMP")
	for _, b := range result.Backups {
		fmt.Printf("%-36s  %-12s  %-8s  %-6d  %s\n",
			b.ID, b.Type, b.Status, b.ShardCount,
			b.Timestamp.Format(time.RFC3339))
	}
}

func cmdBackupCreate(args []string) {
	fs := flag.NewFlagSet("backup create", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	backupType := fs.String("type", "full", "backup type (full, incremental)")
	fs.Parse(args)

	body := fmt.Sprintf(`{"type":%q}`, *backupType)
	url := fmt.Sprintf("http://localhost%s/admin/backups", *addr)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errResp map[string]string
		json.NewDecoder(resp.Body).Decode(&errResp)
		fmt.Fprintf(os.Stderr, "Error: %s\n", errResp["error"])
		os.Exit(1)
	}

	var meta backup.BackupMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Backup created: %s\n", meta.ID)
	fmt.Printf("Type:           %s\n", meta.Type)
	fmt.Printf("Status:         %s\n", meta.Status)
	fmt.Printf("Shards:         %d\n", meta.ShardCount)
	fmt.Printf("Duration:       %s\n", meta.Duration)
}

func cmdBackupRestore(args []string) {
	fs := flag.NewFlagSet("backup restore", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	id := fs.String("id", "", "backup ID to restore (required)")
	fs.Parse(args)

	if *id == "" {
		fmt.Fprintln(os.Stderr, "Error: --id is required")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://localhost%s/admin/backups/%s/restore", *addr, *id)
	resp, err := http.Post(url, "application/json", strings.NewReader("{}"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Restore completed: %s\n", result["message"])
}

func cmdBackupDelete(args []string) {
	fs := flag.NewFlagSet("backup delete", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	id := fs.String("id", "", "backup ID to delete (required)")
	fs.Parse(args)

	if *id == "" {
		fmt.Fprintln(os.Stderr, "Error: --id is required")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://localhost%s/admin/backups/%s", *addr, *id)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Backup deleted: %s\n", *id)
}

func cmdRebalance(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster rebalance <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  plan    Show migration plan (dry run)")
		fmt.Fprintln(os.Stderr, "  run     Execute rebalance")
		fmt.Fprintln(os.Stderr, "  status  Show rebalance progress")
		os.Exit(1)
	}

	switch args[0] {
	case "plan":
		cmdRebalancePlan(args[1:])
	case "run":
		cmdRebalanceRun(args[1:])
	case "status":
		cmdRebalanceStatus(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown rebalance subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdRebalancePlan(args []string) {
	fs := flag.NewFlagSet("rebalance plan", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	pkCol := fs.String("partition-key", "", "partition key column (required)")
	tables := fs.String("tables", "", "comma-separated table names (empty = all)")
	targetShards := fs.Int("target-shards", 0, "target shard count (0 = current)")
	fs.Parse(args)

	if *pkCol == "" {
		fmt.Fprintln(os.Stderr, "Error: --partition-key is required")
		os.Exit(1)
	}

	body := buildRebalanceBody(*pkCol, *tables, 0, *targetShards)
	url := fmt.Sprintf("http://localhost%s/admin/rebalance/plan", *addr)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var plan map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&plan); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	if errMsg, ok := plan["error"].(string); ok {
		fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
		os.Exit(1)
	}

	fmt.Printf("Shard count:   %v\n", formatFloat(plan["shard_count"]))
	fmt.Printf("Total rows:    %v\n", formatFloat(plan["total_rows"]))
	fmt.Printf("Rows to move:  %v\n", formatFloat(plan["rows_to_move"]))

	if summary, ok := plan["summary"].(map[string]any); ok {
		fmt.Println("\nPer-table summary:")
		for table, v := range summary {
			if ts, ok := v.(map[string]any); ok {
				fmt.Printf("  %s: %v total, %v to move\n", table, formatFloat(ts["total_rows"]), formatFloat(ts["rows_to_move"]))
			}
		}
	}
}

func cmdRebalanceRun(args []string) {
	fs := flag.NewFlagSet("rebalance run", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	pkCol := fs.String("partition-key", "", "partition key column (required)")
	tables := fs.String("tables", "", "comma-separated table names (empty = all)")
	batchSize := fs.Int("batch-size", 0, "rows per batch (0 = default 1000)")
	targetShards := fs.Int("target-shards", 0, "target shard count (0 = current)")
	fs.Parse(args)

	if *pkCol == "" {
		fmt.Fprintln(os.Stderr, "Error: --partition-key is required")
		os.Exit(1)
	}

	body := buildRebalanceBody(*pkCol, *tables, *batchSize, *targetShards)
	url := fmt.Sprintf("http://localhost%s/admin/rebalance", *addr)
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	if errMsg, ok := status["error"].(string); ok {
		fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
		os.Exit(1)
	}

	fmt.Printf("Rebalance started (state: %v)\n", status["state"])
	fmt.Println("Use 'duckdb-cluster rebalance status' to monitor progress.")
}

func cmdRebalanceStatus(args []string) {
	fs := flag.NewFlagSet("rebalance status", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/admin/rebalance/status", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("State:         %v\n", status["state"])
	fmt.Printf("Tables:        %v / %v\n", formatFloat(status["tables_done"]), formatFloat(status["tables_total"]))
	fmt.Printf("Rows scanned:  %v\n", formatFloat(status["rows_scanned"]))
	fmt.Printf("Rows moved:    %v\n", formatFloat(status["rows_moved"]))

	if errs, ok := status["errors"].([]any); ok && len(errs) > 0 {
		fmt.Println("\nErrors:")
		for _, e := range errs {
			fmt.Printf("  - %v\n", e)
		}
	}
}

func buildRebalanceBody(pkCol, tables string, batchSize, targetShards int) string {
	cfg := map[string]any{
		"partition_key_column": pkCol,
	}
	if tables != "" {
		cfg["tables"] = strings.Split(tables, ",")
	}
	if batchSize > 0 {
		cfg["batch_size"] = batchSize
	}
	if targetShards > 0 {
		cfg["target_shard_count"] = targetShards
	}
	b, _ := json.Marshal(cfg)
	return string(b)
}

// formatFloat formats a JSON number (float64) as an integer string for display.
func formatFloat(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%d", int(f))
	}
	return fmt.Sprintf("%v", v)
}
