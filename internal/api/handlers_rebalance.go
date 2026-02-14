package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/chronicblondiee/duckdb-cluster/internal/rebalance"
)

func (s *Server) handleRebalancePlan(w http.ResponseWriter, r *http.Request) {
	var cfg rebalance.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return
	}

	plan, err := s.rebalancer.Plan(r.Context(), cfg)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) handleRebalance(w http.ResponseWriter, r *http.Request) {
	var cfg rebalance.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return
	}

	// Launch rebalance in background with detached context
	go func() {
		s.rebalancer.Execute(context.Background(), cfg)
	}()

	// Return current status immediately
	writeJSON(w, http.StatusAccepted, s.rebalancer.GetStatus())
}

func (s *Server) handleRebalanceStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.rebalancer.GetStatus())
}
