package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/backup"
	"github.com/chronicblondiee/duckdb-cluster/internal/cluster"
	"github.com/chronicblondiee/duckdb-cluster/internal/config"
	"github.com/chronicblondiee/duckdb-cluster/internal/index"
	"github.com/chronicblondiee/duckdb-cluster/internal/migration"
	"github.com/chronicblondiee/duckdb-cluster/internal/rebalance"
	"github.com/chronicblondiee/duckdb-cluster/internal/security"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	Cluster          *cluster.Cluster
	mux              *http.ServeMux
	authenticator    *security.Authenticator
	authorizer       *security.Authorizer
	rateLimiter      *security.RateLimiter
	backupManager    *backup.BackupManager
	migrationManager *migration.Manager
	rebalancer       *rebalance.Rebalancer
	registry         *index.Registry
	schemaRegistry   *index.SchemaRegistry
}

func NewServer(c *cluster.Cluster, cfg *config.Config) (*Server, error) {
	// Initialize security components
	authenticator, err := security.NewAuthenticator(security.AuthConfig{
		Enabled:         cfg.Security.Authentication.Enabled,
		JWTSecret:       cfg.Security.Authentication.JWTSecret,
		TokenExpiration: cfg.Security.Authentication.TokenExpiration,
		AllowAnonymous:  cfg.Security.Authentication.AllowAnonymous,
	})
	if err != nil {
		return nil, err
	}

	authorizer := security.NewAuthorizer()

	rateLimiter := security.NewRateLimiter(security.RateLimitConfig{
		Enabled:           cfg.Security.RateLimit.Enabled,
		RequestsPerSecond: cfg.Security.RateLimit.RequestsPerSecond,
		Burst:             cfg.Security.RateLimit.Burst,
		PerTenant:         cfg.Security.RateLimit.PerTenant,
		PerAPIKey:         cfg.Security.RateLimit.PerAPIKey,
	})

	// Initialize backup manager
	backupManager, err := backup.NewBackupManager(cfg)
	if err != nil {
		return nil, err
	}

	// Initialize index registry and schema registry
	var reg *index.Registry
	var schemaReg *index.SchemaRegistry
	if c.Registry != nil {
		reg = c.Registry
	} else {
		reg = index.NewRegistry(cfg.Common.DataDir)
	}
	schemaReg = index.NewSchemaRegistry(cfg.Common.DataDir)
	schemaReg.LoadAll()

	s := &Server{
		Cluster:        c,
		mux:            http.NewServeMux(),
		authenticator:  authenticator,
		authorizer:     authorizer,
		rateLimiter:    rateLimiter,
		backupManager:  backupManager,
		rebalancer:     rebalance.NewRebalancer(c.Manager, slog.Default()),
		registry:       reg,
		schemaRegistry: schemaReg,
	}

	// Query endpoints
	s.mux.HandleFunc("POST /query", s.handleQuery)
	s.mux.HandleFunc("POST /bulk", s.handleBulk)
	s.mux.HandleFunc("POST /multi-query", s.handleMultiQuery)

	// Admin endpoints (backward compat — operate on _default)
	s.mux.HandleFunc("GET /admin/shards", s.handleListShards)
	s.mux.HandleFunc("POST /admin/shards", s.handleAddShard)
	s.mux.HandleFunc("DELETE /admin/shards/{id}", s.handleRemoveShard)
	s.mux.HandleFunc("GET /admin/tables", s.handleListTables)
	s.mux.HandleFunc("GET /admin/tables/{name}", s.handleTableSchema)
	s.mux.HandleFunc("GET /admin/stats", s.handleStats)

	// Security endpoints
	s.mux.HandleFunc("POST /admin/auth/token", s.handleGenerateToken)
	s.mux.HandleFunc("POST /admin/auth/apikey", s.handleRegisterAPIKey)
	s.mux.HandleFunc("DELETE /admin/auth/apikey", s.handleRevokeAPIKey)

	// Backup endpoints
	s.mux.HandleFunc("POST /admin/backups", s.handleCreateBackup)
	s.mux.HandleFunc("GET /admin/backups", s.handleListBackups)
	s.mux.HandleFunc("POST /admin/backups/", s.handleRestoreBackup)
	s.mux.HandleFunc("DELETE /admin/backups/", s.handleDeleteBackup)

	// Migration endpoints
	s.mux.HandleFunc("GET /admin/version", s.handleVersion)
	s.mux.HandleFunc("POST /admin/migrate", s.handleMigrate)

	// Rebalance endpoints
	s.mux.HandleFunc("POST /admin/rebalance", s.handleRebalance)
	s.mux.HandleFunc("GET /admin/rebalance/status", s.handleRebalanceStatus)
	s.mux.HandleFunc("POST /admin/rebalance/plan", s.handleRebalancePlan)
	s.mux.HandleFunc("GET /admin/rebalance/stream", s.handleRebalanceStream)

	// Index CRUD endpoints
	s.mux.HandleFunc("PUT /indices/{name}", s.handleCreateIndex)
	s.mux.HandleFunc("GET /indices", s.handleListIndices)
	s.mux.HandleFunc("GET /indices/{name}", s.handleGetIndex)
	s.mux.HandleFunc("DELETE /indices/{name}", s.handleDeleteIndex)
	s.mux.HandleFunc("POST /indices/{name}/_close", s.handleCloseIndex)
	s.mux.HandleFunc("POST /indices/{name}/_open", s.handleOpenIndex)

	// Mapping endpoints
	s.mux.HandleFunc("PUT /indices/{name}/_mapping", s.handlePutMapping)
	s.mux.HandleFunc("GET /indices/{name}/_mapping", s.handleGetMapping)

	// Document ingestion endpoints
	s.mux.HandleFunc("POST /indices/{name}/_doc", s.handleIndexDoc)
	s.mux.HandleFunc("POST /indices/{name}/_bulk", s.handleBulkDocs)

	// Schema registry endpoints
	s.mux.HandleFunc("PUT /indices/{name}/_schema", s.handlePutSchema)
	s.mux.HandleFunc("GET /indices/{name}/_schema", s.handleGetSchema)
	s.mux.HandleFunc("DELETE /indices/{name}/_schema", s.handleDeleteSchema)

	// Health endpoint
	s.mux.HandleFunc("GET /health", s.handleHealth)

	// Metrics endpoint (Prometheus)
	s.mux.Handle("GET /metrics", promhttp.Handler())

	return s, nil
}

// Handler returns the HTTP handler with security middleware
func (s *Server) Handler() http.Handler {
	handler := security.ChainHTTPMiddleware(
		security.HTTPRateLimitMiddleware(s.rateLimiter),
		security.HTTPAuthMiddleware(s.authenticator),
		security.HTTPAuthzMiddleware(s.authorizer),
	)(s.mux)

	return handler
}

// SetMigrationManager sets the migration manager for version/migrate endpoints.
func (s *Server) SetMigrationManager(mm *migration.Manager) {
	s.migrationManager = mm
}

func (s *Server) Start(addr string) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("HTTP server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("received shutdown signal")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return err
	}

	return s.Cluster.Shutdown()
}
