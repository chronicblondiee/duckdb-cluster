package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleVersion returns the current binary version and migration status.
// GET /admin/version
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if s.migrationManager == nil {
		http.Error(w, "migration manager not initialized", http.StatusServiceUnavailable)
		return
	}
	status, err := s.migrationManager.GetStatus()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to get version status: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handleMigrate triggers pending migrations.
// POST /admin/migrate
func (s *Server) handleMigrate(w http.ResponseWriter, r *http.Request) {
	if s.migrationManager == nil {
		http.Error(w, "migration manager not initialized", http.StatusServiceUnavailable)
		return
	}
	result, err := s.migrationManager.RunPending(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(result)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
