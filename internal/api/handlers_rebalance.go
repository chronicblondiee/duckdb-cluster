package api

import (
	"context"
	"encoding/json"
	"fmt"
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

func (s *Server) handleRebalanceStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := s.rebalancer.Subscribe()
	defer unsubscribe()

	// Send current status immediately
	status := s.rebalancer.GetStatus()
	data, _ := json.Marshal(status)
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case update, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(update)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if update.State == "completed" || update.State == "failed" {
				return
			}
		}
	}
}
