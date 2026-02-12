package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brown/duckdb-cluster/internal/cluster"
)

type Server struct {
	Cluster *cluster.Cluster
	mux     *http.ServeMux
}

func NewServer(c *cluster.Cluster) *Server {
	s := &Server{
		Cluster: c,
		mux:     http.NewServeMux(),
	}
	s.mux.HandleFunc("POST /query", s.handleQuery)
	s.mux.HandleFunc("GET /admin/shards", s.handleListShards)
	s.mux.HandleFunc("POST /admin/shards", s.handleAddShard)
	s.mux.HandleFunc("DELETE /admin/shards/{id}", s.handleRemoveShard)
	s.mux.HandleFunc("GET /health", s.handleHealth)
	return s
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
