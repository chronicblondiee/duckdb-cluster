package modules

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/brown/duckdb-cluster/internal/api"
	"github.com/brown/duckdb-cluster/internal/cluster"
	"github.com/brown/duckdb-cluster/internal/config"
	"github.com/brown/duckdb-cluster/internal/module"
)

// ServerModule wraps the HTTP/gRPC server
type ServerModule struct {
	cfg     *config.Config
	cluster *cluster.Cluster
	server  *api.Server
	done    chan os.Signal
}

// NewServerModule creates a new server module
func NewServerModule(cfg *config.Config, clust *cluster.Cluster) *ServerModule {
	return &ServerModule{
		cfg:     cfg,
		cluster: clust,
		done:    make(chan os.Signal, 1),
	}
}

func (s *ServerModule) Name() string {
	return "server"
}

func (s *ServerModule) Dependencies() []string {
	// Server typically depends on ingester being ready (in monolithic mode)
	// In distributed mode, no dependencies
	if s.cfg.IsMonolithic() {
		return []string{"ingester"}
	}
	return []string{}
}

func (s *ServerModule) Init(ctx context.Context) error {
	slog.Info("initializing server module")
	s.server = api.NewServer(s.cluster)
	return nil
}

func (s *ServerModule) Start(ctx context.Context) error {
	slog.Info("starting server module", "http_addr", s.cfg.Server.HTTPListenAddr)
	
	srv := &http.Server{
		Addr:         s.cfg.Server.HTTPListenAddr,
		Handler:      s.server.Handler(),
		ReadTimeout:  s.cfg.Server.ReadTimeout,
		WriteTimeout: s.cfg.Server.WriteTimeout,
	}
	
	signal.Notify(s.done, os.Interrupt, syscall.SIGTERM)
	
	// Start server in background
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()
	
	// Wait for shutdown signal in background
	go func() {
		<-s.done
		slog.Info("received shutdown signal")
		
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
		defer cancel()
		
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	}()
	
	return nil
}

func (s *ServerModule) Stop() error {
	slog.Info("stopping server module")
	// Trigger shutdown
	s.done <- syscall.SIGTERM
	return nil
}

// Verify interface implementation
var _ module.Module = (*ServerModule)(nil)
