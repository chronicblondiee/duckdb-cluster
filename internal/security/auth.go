package security

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// User represents an authenticated user
type User struct {
	ID       string   `json:"id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	TenantID string   `json:"tenant_id"`
}

// Claims represents JWT claims with custom fields
type Claims struct {
	jwt.RegisteredClaims
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	TenantID string   `json:"tenant_id"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	// Enabled controls whether authentication is enforced
	Enabled bool
	
	// JWTSecret is the secret key for signing JWT tokens
	JWTSecret string
	
	// TokenExpiration is how long tokens are valid
	TokenExpiration time.Duration
	
	// AllowAnonymous allows unauthenticated requests
	AllowAnonymous bool
}

// Authenticator manages authentication
type Authenticator struct {
	config    AuthConfig
	apiKeys   map[string]*User // API key -> User mapping
	apiKeysMu sync.RWMutex
}

// NewAuthenticator creates a new authenticator
func NewAuthenticator(config AuthConfig) (*Authenticator, error) {
	if config.Enabled && config.JWTSecret == "" {
		// Generate a random secret if not provided
		secret, err := generateSecret(32)
		if err != nil {
			return nil, fmt.Errorf("generate JWT secret: %w", err)
		}
		config.JWTSecret = secret
	}
	
	if config.TokenExpiration == 0 {
		config.TokenExpiration = 24 * time.Hour // Default: 24 hours
	}
	
	return &Authenticator{
		config:  config,
		apiKeys: make(map[string]*User),
	}, nil
}

// GenerateToken creates a JWT token for a user
func (a *Authenticator) GenerateToken(user *User) (string, error) {
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.config.TokenExpiration)),
			Issuer:    "duckdb-cluster",
		},
		UserID:   user.ID,
		Username: user.Username,
		Roles:    user.Roles,
		TenantID: user.TenantID,
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.JWTSecret))
}

// ValidateToken validates a JWT token and returns the user
func (a *Authenticator) ValidateToken(tokenString string) (*User, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(a.config.JWTSecret), nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}
	
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, fmt.Errorf("invalid claims type")
	}
	
	return &User{
		ID:       claims.UserID,
		Username: claims.Username,
		Roles:    claims.Roles,
		TenantID: claims.TenantID,
	}, nil
}

// AuthenticateRequest authenticates a request from Authorization header
// Supports both "Bearer <token>" and "ApiKey <key>" formats
func (a *Authenticator) AuthenticateRequest(authHeader string) (*User, error) {
	if !a.config.Enabled {
		// Return a default user when auth is disabled
		return &User{
			ID:       "default",
			Username: "default",
			Roles:    []string{"admin"},
			TenantID: "default",
		}, nil
	}
	
	if authHeader == "" {
		if a.config.AllowAnonymous {
			return &User{
				ID:       "anonymous",
				Username: "anonymous",
				Roles:    []string{"read"},
				TenantID: "default",
			}, nil
		}
		return nil, fmt.Errorf("missing authorization header")
	}
	
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid authorization header format")
	}
	
	authType := parts[0]
	credentials := parts[1]
	
	switch authType {
	case "Bearer":
		return a.ValidateToken(credentials)
	case "ApiKey":
		return a.ValidateAPIKey(credentials)
	default:
		return nil, fmt.Errorf("unsupported authorization type: %s", authType)
	}
}

// RegisterAPIKey registers an API key for a user
func (a *Authenticator) RegisterAPIKey(apiKey string, user *User) error {
	a.apiKeysMu.Lock()
	defer a.apiKeysMu.Unlock()
	
	if _, exists := a.apiKeys[apiKey]; exists {
		return fmt.Errorf("API key already exists")
	}
	
	a.apiKeys[apiKey] = user
	return nil
}

// ValidateAPIKey validates an API key and returns the associated user
func (a *Authenticator) ValidateAPIKey(apiKey string) (*User, error) {
	a.apiKeysMu.RLock()
	defer a.apiKeysMu.RUnlock()
	
	user, exists := a.apiKeys[apiKey]
	if !exists {
		return nil, fmt.Errorf("invalid API key")
	}
	
	return user, nil
}

// RevokeAPIKey removes an API key
func (a *Authenticator) RevokeAPIKey(apiKey string) error {
	a.apiKeysMu.Lock()
	defer a.apiKeysMu.Unlock()
	
	if _, exists := a.apiKeys[apiKey]; !exists {
		return fmt.Errorf("API key not found")
	}
	
	delete(a.apiKeys, apiKey)
	return nil
}

// GenerateAPIKey generates a new random API key
func GenerateAPIKey() (string, error) {
	return generateSecret(32)
}

// generateSecret generates a random base64-encoded secret
func generateSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// Context keys for storing user in context
type contextKey string

const userContextKey contextKey = "user"

// WithUser adds a user to the context
func WithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext retrieves the user from context
func UserFromContext(ctx context.Context) (*User, bool) {
	user, ok := ctx.Value(userContextKey).(*User)
	return user, ok
}

// MustUserFromContext retrieves the user from context, panics if not found
func MustUserFromContext(ctx context.Context) *User {
	user, ok := UserFromContext(ctx)
	if !ok {
		panic("user not found in context")
	}
	return user
}

// TokenExpiration returns the configured token expiration duration
func (a *Authenticator) TokenExpiration() time.Duration {
	return a.config.TokenExpiration
}
