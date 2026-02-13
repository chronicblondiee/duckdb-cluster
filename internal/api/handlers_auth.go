package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/chronicblondiee/duckdb-cluster/internal/security"
)

// generateTokenRequest represents a request to generate a JWT token
type generateTokenRequest struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	TenantID string   `json:"tenant_id"`
}

// generateTokenResponse represents the response with a JWT token
type generateTokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"` // seconds
}

// handleGenerateToken generates a JWT token for a user
func (s *Server) handleGenerateToken(w http.ResponseWriter, r *http.Request) {
	var req generateTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	
	// Validate request
	if req.UserID == "" || req.Username == "" {
		http.Error(w, "user_id and username are required", http.StatusBadRequest)
		return
	}
	
	if len(req.Roles) == 0 {
		req.Roles = []string{"reader"} // Default role
	}
	
	if req.TenantID == "" {
		req.TenantID = "default"
	}
	
	// Create user
	user := &security.User{
		ID:       req.UserID,
		Username: req.Username,
		Roles:    req.Roles,
		TenantID: req.TenantID,
	}
	
	// Generate token
	token, err := s.authenticator.GenerateToken(user)
	if err != nil {
		slog.Error("failed to generate token", "error", err)
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}
	
	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(generateTokenResponse{
		Token:     token,
		ExpiresIn: int64(s.authenticator.TokenExpiration().Seconds()),
	})
}

// registerAPIKeyRequest represents a request to register an API key
type registerAPIKeyRequest struct {
	APIKey   string   `json:"api_key"` // Optional: if empty, generates one
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	TenantID string   `json:"tenant_id"`
}

// registerAPIKeyResponse represents the response with API key details
type registerAPIKeyResponse struct {
	APIKey   string `json:"api_key"`
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Roles    []string `json:"roles"`
	TenantID string `json:"tenant_id"`
}

// handleRegisterAPIKey registers a new API key
func (s *Server) handleRegisterAPIKey(w http.ResponseWriter, r *http.Request) {
	var req registerAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	
	// Validate request
	if req.UserID == "" || req.Username == "" {
		http.Error(w, "user_id and username are required", http.StatusBadRequest)
		return
	}
	
	if len(req.Roles) == 0 {
		req.Roles = []string{"reader"} // Default role
	}
	
	if req.TenantID == "" {
		req.TenantID = "default"
	}
	
	// Generate API key if not provided
	if req.APIKey == "" {
		apiKey, err := security.GenerateAPIKey()
		if err != nil {
			slog.Error("failed to generate API key", "error", err)
			http.Error(w, "Failed to generate API key", http.StatusInternalServerError)
			return
		}
		req.APIKey = apiKey
	}
	
	// Create user
	user := &security.User{
		ID:       req.UserID,
		Username: req.Username,
		Roles:    req.Roles,
		TenantID: req.TenantID,
	}
	
	// Register API key
	if err := s.authenticator.RegisterAPIKey(req.APIKey, user); err != nil {
		slog.Error("failed to register API key", "error", err)
		http.Error(w, "Failed to register API key", http.StatusInternalServerError)
		return
	}
	
	slog.Info("API key registered", "user_id", req.UserID, "username", req.Username)
	
	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registerAPIKeyResponse{
		APIKey:   req.APIKey,
		UserID:   user.ID,
		Username: user.Username,
		Roles:    user.Roles,
		TenantID: user.TenantID,
	})
}

// revokeAPIKeyRequest represents a request to revoke an API key
type revokeAPIKeyRequest struct {
	APIKey string `json:"api_key"`
}

// handleRevokeAPIKey revokes an API key
func (s *Server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	var req revokeAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	
	// Validate request
	if req.APIKey == "" {
		http.Error(w, "api_key is required", http.StatusBadRequest)
		return
	}
	
	// Revoke API key
	if err := s.authenticator.RevokeAPIKey(req.APIKey); err != nil {
		slog.Error("failed to revoke API key", "error", err)
		http.Error(w, "Failed to revoke API key: "+err.Error(), http.StatusBadRequest)
		return
	}
	
	slog.Info("API key revoked", "api_key", req.APIKey)
	
	// Return success
	w.WriteHeader(http.StatusNoContent)
}
