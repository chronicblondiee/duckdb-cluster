package api

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chronicblondiee/duckdb-cluster/internal/backup"
)

// handleCreateBackup creates a new backup
// POST /admin/backups
func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.backupManager == nil {
		http.Error(w, "backup manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Parse request body
	var req struct {
		Type string `json:"type"` // "full" or "incremental"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Default to full backup
	backupType := backup.BackupTypeFull
	if req.Type != "" {
		backupType = backup.BackupType(req.Type)
		if backupType != backup.BackupTypeFull && backupType != backup.BackupTypeIncremental {
			http.Error(w, "invalid backup type: must be 'full' or 'incremental'", http.StatusBadRequest)
			return
		}
	}

	// Create backup
	metadata, err := s.backupManager.CreateBackup(r.Context(), backupType)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to create backup: %v", err), http.StatusInternalServerError)
		return
	}

	// Return metadata
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(metadata)
}

// handleListBackups lists all available backups
// GET /admin/backups
func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.backupManager == nil {
		http.Error(w, "backup manager not initialized", http.StatusServiceUnavailable)
		return
	}

	backups, err := s.backupManager.ListBackups()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list backups: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"backups": backups,
		"count":   len(backups),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleRestoreBackup restores from a backup
// POST /admin/backups/{id}/restore
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.backupManager == nil {
		http.Error(w, "backup manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Extract backup ID from URL path
	// URL format: /admin/backups/{id}/restore
	// Parse the path to get the backup ID
	path := r.URL.Path
	backupID := ""
	
	// Simple path parsing (in production, use a router library)
	if len(path) > len("/admin/backups/") {
		parts := path[len("/admin/backups/"):]
		if idx := len(parts) - len("/restore"); idx > 0 && parts[idx:] == "/restore" {
			backupID = parts[:idx]
		}
	}

	if backupID == "" {
		http.Error(w, "backup ID is required", http.StatusBadRequest)
		return
	}

	// Parse optional target path from request body
	var req struct {
		TargetPath string `json:"target_path,omitempty"`
	}
	
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}

	// Restore backup
	if err := s.backupManager.Restore(r.Context(), backupID, req.TargetPath); err != nil {
		http.Error(w, fmt.Sprintf("failed to restore backup: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success":   true,
		"backup_id": backupID,
		"message":   "backup restored successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleDeleteBackup deletes a backup
// DELETE /admin/backups/{id}
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.backupManager == nil {
		http.Error(w, "backup manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Extract backup ID from URL path
	// URL format: /admin/backups/{id}
	path := r.URL.Path
	backupID := ""
	
	// Simple path parsing
	if len(path) > len("/admin/backups/") {
		backupID = path[len("/admin/backups/"):]
	}

	if backupID == "" {
		http.Error(w, "backup ID is required", http.StatusBadRequest)
		return
	}

	// Delete backup
	if err := s.backupManager.DeleteBackup(backupID); err != nil {
		http.Error(w, fmt.Sprintf("failed to delete backup: %v", err), http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"success":   true,
		"backup_id": backupID,
		"message":   "backup deleted successfully",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
