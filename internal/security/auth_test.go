package security

import (
	"testing"
	"time"
)

func TestNewAuthenticator(t *testing.T) {
	tests := []struct {
		name    string
		config  AuthConfig
		wantErr bool
	}{
		{
			name: "valid config with secret",
			config: AuthConfig{
				Enabled:         true,
				JWTSecret:       "test-secret",
				TokenExpiration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "valid config without secret (auto-generate)",
			config: AuthConfig{
				Enabled:         true,
				TokenExpiration: 1 * time.Hour,
			},
			wantErr: false,
		},
		{
			name: "disabled auth",
			config: AuthConfig{
				Enabled: false,
			},
			wantErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := NewAuthenticator(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAuthenticator() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && auth == nil {
				t.Error("NewAuthenticator() returned nil")
			}
		})
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	auth, err := NewAuthenticator(AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret",
		TokenExpiration: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	
	user := &User{
		ID:       "user-123",
		Username: "testuser",
		Roles:    []string{"admin", "writer"},
		TenantID: "tenant-1",
	}
	
	// Generate token
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	
	if token == "" {
		t.Error("GenerateToken() returned empty token")
	}
	
	// Validate token
	validatedUser, err := auth.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken() error = %v", err)
	}
	
	if validatedUser.ID != user.ID {
		t.Errorf("ValidateToken() ID = %v, want %v", validatedUser.ID, user.ID)
	}
	if validatedUser.Username != user.Username {
		t.Errorf("ValidateToken() Username = %v, want %v", validatedUser.Username, user.Username)
	}
	if validatedUser.TenantID != user.TenantID {
		t.Errorf("ValidateToken() TenantID = %v, want %v", validatedUser.TenantID, user.TenantID)
	}
	if len(validatedUser.Roles) != len(user.Roles) {
		t.Errorf("ValidateToken() Roles = %v, want %v", validatedUser.Roles, user.Roles)
	}
}

func TestValidateTokenInvalid(t *testing.T) {
	auth, err := NewAuthenticator(AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret",
		TokenExpiration: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
		},
		{
			name:    "invalid token",
			token:   "invalid.token.here",
			wantErr: true,
		},
		{
			name:    "malformed token",
			token:   "not-a-jwt",
			wantErr: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := auth.ValidateToken(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateToken() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAPIKeyManagement(t *testing.T) {
	auth, err := NewAuthenticator(AuthConfig{
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	
	user := &User{
		ID:       "user-456",
		Username: "apiuser",
		Roles:    []string{"reader"},
		TenantID: "tenant-2",
	}
	
	apiKey := "test-api-key-12345"
	
	// Register API key
	err = auth.RegisterAPIKey(apiKey, user)
	if err != nil {
		t.Fatalf("RegisterAPIKey() error = %v", err)
	}
	
	// Validate API key
	validatedUser, err := auth.ValidateAPIKey(apiKey)
	if err != nil {
		t.Fatalf("ValidateAPIKey() error = %v", err)
	}
	
	if validatedUser.ID != user.ID {
		t.Errorf("ValidateAPIKey() ID = %v, want %v", validatedUser.ID, user.ID)
	}
	
	// Revoke API key
	err = auth.RevokeAPIKey(apiKey)
	if err != nil {
		t.Fatalf("RevokeAPIKey() error = %v", err)
	}
	
	// Validate revoked API key should fail
	_, err = auth.ValidateAPIKey(apiKey)
	if err == nil {
		t.Error("ValidateAPIKey() should fail for revoked key")
	}
}

func TestAuthenticateRequest(t *testing.T) {
	auth, err := NewAuthenticator(AuthConfig{
		Enabled:         true,
		JWTSecret:       "test-secret",
		TokenExpiration: 1 * time.Hour,
		AllowAnonymous:  false,
	})
	if err != nil {
		t.Fatalf("NewAuthenticator() error = %v", err)
	}
	
	user := &User{
		ID:       "user-789",
		Username: "requser",
		Roles:    []string{"admin"},
		TenantID: "tenant-3",
	}
	
	// Generate JWT token
	token, err := auth.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	
	// Register API key
	apiKey := "test-api-key-99999"
	auth.RegisterAPIKey(apiKey, user)
	
	tests := []struct {
		name       string
		authHeader string
		wantErr    bool
		wantUserID string
	}{
		{
			name:       "valid bearer token",
			authHeader: "Bearer " + token,
			wantErr:    false,
			wantUserID: user.ID,
		},
		{
			name:       "valid api key",
			authHeader: "ApiKey " + apiKey,
			wantErr:    false,
			wantUserID: user.ID,
		},
		{
			name:       "missing auth header",
			authHeader: "",
			wantErr:    true,
		},
		{
			name:       "invalid auth type",
			authHeader: "Basic dGVzdDp0ZXN0",
			wantErr:    true,
		},
		{
			name:       "invalid token format",
			authHeader: "Bearer",
			wantErr:    true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validatedUser, err := auth.AuthenticateRequest(tt.authHeader)
			if (err != nil) != tt.wantErr {
				t.Errorf("AuthenticateRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && validatedUser.ID != tt.wantUserID {
				t.Errorf("AuthenticateRequest() UserID = %v, want %v", validatedUser.ID, tt.wantUserID)
			}
		})
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key1, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}
	
	if key1 == "" {
		t.Error("GenerateAPIKey() returned empty key")
	}
	
	// Generate another key and ensure it's different
	key2, err := GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}
	
	if key1 == key2 {
		t.Error("GenerateAPIKey() returned duplicate keys")
	}
}
