package reliability

import (
	"context"
	"fmt"
	"time"
)

// TimeoutConfig holds timeout configuration
type TimeoutConfig struct {
	// QueryTimeout is the maximum time a query can run
	QueryTimeout time.Duration
	
	// WriteTimeout is the maximum time a write can take
	WriteTimeout time.Duration
	
	// ReplicationTimeout is the maximum time for replication
	ReplicationTimeout time.Duration
	
	// HealthCheckTimeout is the timeout for health checks
	HealthCheckTimeout time.Duration
}

// DefaultTimeoutConfig returns default timeout configuration
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		QueryTimeout:       60 * time.Second,
		WriteTimeout:       30 * time.Second,
		ReplicationTimeout: 10 * time.Second,
		HealthCheckTimeout: 5 * time.Second,
	}
}

// WithQueryTimeout creates a context with query timeout
func WithQueryTimeout(ctx context.Context, config TimeoutConfig) (context.Context, context.CancelFunc) {
	if config.QueryTimeout == 0 {
		config = DefaultTimeoutConfig()
	}
	return context.WithTimeout(ctx, config.QueryTimeout)
}

// WithWriteTimeout creates a context with write timeout
func WithWriteTimeout(ctx context.Context, config TimeoutConfig) (context.Context, context.CancelFunc) {
	if config.WriteTimeout == 0 {
		config = DefaultTimeoutConfig()
	}
	return context.WithTimeout(ctx, config.WriteTimeout)
}

// WithReplicationTimeout creates a context with replication timeout
func WithReplicationTimeout(ctx context.Context, config TimeoutConfig) (context.Context, context.CancelFunc) {
	if config.ReplicationTimeout == 0 {
		config = DefaultTimeoutConfig()
	}
	return context.WithTimeout(ctx, config.ReplicationTimeout)
}

// WithHealthCheckTimeout creates a context with health check timeout
func WithHealthCheckTimeout(ctx context.Context, config TimeoutConfig) (context.Context, context.CancelFunc) {
	if config.HealthCheckTimeout == 0 {
		config = DefaultTimeoutConfig()
	}
	return context.WithTimeout(ctx, config.HealthCheckTimeout)
}

// IsTimeoutError checks if an error is a timeout error
func IsTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	return err == context.DeadlineExceeded || err == context.Canceled
}

// WrapTimeoutError wraps a timeout error with additional context
func WrapTimeoutError(err error, operation string, timeout time.Duration) error {
	if !IsTimeoutError(err) {
		return err
	}
	return fmt.Errorf("%s timed out after %v: %w", operation, timeout, err)
}
