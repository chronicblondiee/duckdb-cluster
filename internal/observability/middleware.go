package observability

import (
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HTTPMetricsMiddleware wraps an HTTP handler to record metrics
func HTTPMetricsMiddleware(metrics *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			
			// Wrap response writer to capture status code
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}
			
			// Increment active connections
			if metrics != nil {
				metrics.ActiveConnections.Inc()
				defer metrics.ActiveConnections.Dec()
			}
			
			// Handle request
			next.ServeHTTP(wrapped, r)
			
			// Record duration
			duration := time.Since(start).Seconds()
			
			// Record metrics
			if metrics != nil {
				status := strconv.Itoa(wrapped.statusCode)
				path := r.URL.Path
				
				// Simplified path for metrics (avoid high cardinality)
				simplePath := simplifyPath(path)
				
				metrics.GRPCRequestDuration.WithLabelValues("http", simplePath, status).Observe(duration)
				metrics.GRPCRequestTotal.WithLabelValues("http", simplePath).Inc()
				
				if wrapped.statusCode >= 400 {
					metrics.GRPCRequestErrors.WithLabelValues("http", simplePath, status).Inc()
				}
			}
		})
	}
}

// HTTPTracingMiddleware wraps an HTTP handler to add tracing
func HTTPTracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := StartSpan(r.Context(), "http-server", r.URL.Path,
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.url", r.URL.String()),
				attribute.String("http.user_agent", r.UserAgent()),
				attribute.String("http.remote_addr", r.RemoteAddr),
			))
		defer span.End()
		
		// Wrap response writer to capture status code
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}
		
		// Handle request with traced context
		next.ServeHTTP(wrapped, r.WithContext(ctx))
		
		// Add status code to span
		span.SetAttributes(attribute.Int("http.status_code", wrapped.statusCode))
		
		if wrapped.statusCode >= 400 {
			span.SetAttributes(attribute.Bool("error", true))
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// simplifyPath reduces path cardinality for metrics
func simplifyPath(path string) string {
	// Map common paths to simplified versions
	switch {
	case path == "/health":
		return "/health"
	case path == "/metrics":
		return "/metrics"
	case path == "/query":
		return "/query"
	case path == "/bulk":
		return "/bulk"
	case path == "/admin/shards":
		return "/admin/shards"
	case path == "/admin/stats":
		return "/admin/stats"
	case path == "/admin/tables":
		return "/admin/tables"
	default:
		// For paths with IDs, simplify to template
		if len(path) > 14 && path[:14] == "/admin/shards/" {
			return "/admin/shards/:id"
		}
		if len(path) > 14 && path[:14] == "/admin/tables/" {
			return "/admin/tables/:name"
		}
		return path
	}
}
