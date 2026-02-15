package security

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// HTTPAuthMiddleware creates HTTP middleware for authentication
func HTTPAuthMiddleware(auth *Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for health endpoint
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}
			
			// Extract authorization header
			authHeader := r.Header.Get("Authorization")
			
			// Authenticate request
			user, err := auth.AuthenticateRequest(authHeader)
			if err != nil {
				slog.Warn("authentication failed", "error", err, "path", r.URL.Path)
				http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}
			
			// Add user to context
			ctx := WithUser(r.Context(), user)
			
			// Continue to next handler
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HTTPAuthzMiddleware creates HTTP middleware for authorization
func HTTPAuthzMiddleware(authz *Authorizer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip authz for public endpoints (same as auth skip list)
			if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			// Get user from context
			user, ok := UserFromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized: no user in context", http.StatusUnauthorized)
				return
			}
			
			// Determine required permission based on path and method
			permission := getRequiredPermission(r.Method, r.URL.Path)
			if permission == "" {
				// No permission required for this endpoint
				next.ServeHTTP(w, r)
				return
			}
			
			// Check authorization
			if err := authz.Authorize(user, permission); err != nil {
				slog.Warn("authorization failed", 
					"user", user.Username,
					"permission", permission,
					"path", r.URL.Path,
					"error", err)
				http.Error(w, "Forbidden: "+err.Error(), http.StatusForbidden)
				return
			}
			
			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// HTTPRateLimitMiddleware creates HTTP middleware for rate limiting
func HTTPRateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check rate limit
			if err := limiter.Allow(r.Context()); err != nil {
				slog.Warn("rate limit exceeded", "error", err)
				http.Error(w, "Too Many Requests: "+err.Error(), http.StatusTooManyRequests)
				return
			}
			
			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// getRequiredPermission maps HTTP endpoint to required permission
func getRequiredPermission(method, path string) Permission {
	// Query endpoints
	if path == "/query" || path == "/bulk" || path == "/multi-query" {
		switch method {
		case "GET":
			return PermissionQueryRead
		case "POST":
			// For POST /query, we need to inspect the SQL to determine if it's read or write
			// For now, assume write permission is required (safest default)
			return PermissionQueryWrite
		case "DELETE":
			return PermissionQueryDelete
		}
	}
	
	// Admin endpoints
	if strings.HasPrefix(path, "/admin/shards") {
		return PermissionAdminShards
	}
	if strings.HasPrefix(path, "/admin/tables") {
		return PermissionAdminTables
	}
	if path == "/admin/stats" {
		return PermissionAdminStats
	}
	
	// No permission required (e.g., /health, /metrics)
	return ""
}

// GRPCAuthInterceptor creates gRPC unary interceptor for authentication
func GRPCAuthInterceptor(auth *Authenticator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Extract metadata from context
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			if !auth.config.AllowAnonymous {
				return nil, status.Error(codes.Unauthenticated, "missing metadata")
			}
		}
		
		// Extract authorization header
		var authHeader string
		if values := md.Get("authorization"); len(values) > 0 {
			authHeader = values[0]
		}
		
		// Authenticate request
		user, err := auth.AuthenticateRequest(authHeader)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}
		
		// Add user to context
		ctx = WithUser(ctx, user)
		
		// Continue to handler
		return handler(ctx, req)
	}
}

// GRPCAuthzInterceptor creates gRPC unary interceptor for authorization
func GRPCAuthzInterceptor(authz *Authorizer) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Get user from context
		user, ok := UserFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "no user in context")
		}
		
		// Determine required permission based on method
		permission := getRequiredGRPCPermission(info.FullMethod)
		if permission == "" {
			// No permission required
			return handler(ctx, req)
		}
		
		// Check authorization
		if err := authz.Authorize(user, permission); err != nil {
			slog.Warn("gRPC authorization failed",
				"user", user.Username,
				"permission", permission,
				"method", info.FullMethod,
				"error", err)
			return nil, status.Error(codes.PermissionDenied, err.Error())
		}
		
		// Continue to handler
		return handler(ctx, req)
	}
}

// GRPCRateLimitInterceptor creates gRPC unary interceptor for rate limiting
func GRPCRateLimitInterceptor(limiter *RateLimiter) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Check rate limit
		if err := limiter.Allow(ctx); err != nil {
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		}
		
		// Continue to handler
		return handler(ctx, req)
	}
}

// getRequiredGRPCPermission maps gRPC method to required permission
func getRequiredGRPCPermission(method string) Permission {
	// Ingester methods
	if strings.Contains(method, "IngesterService/Push") {
		return PermissionQueryWrite
	}
	
	// Querier methods
	if strings.Contains(method, "QuerierService/Query") {
		return PermissionQueryRead
	}
	
	// Health checks don't require permissions
	if strings.Contains(method, "/Health") {
		return ""
	}
	
	// Default: no permission required
	return ""
}

// ChainHTTPMiddleware chains multiple HTTP middleware functions
func ChainHTTPMiddleware(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(final http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			final = middlewares[i](final)
		}
		return final
	}
}
