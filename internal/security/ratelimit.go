package security

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	// Enabled controls whether rate limiting is enforced
	Enabled bool
	
	// RequestsPerSecond is the maximum requests per second per tenant
	RequestsPerSecond int
	
	// Burst is the maximum burst size (allows short bursts above the rate)
	Burst int
	
	// PerTenant enables per-tenant rate limiting
	PerTenant bool
	
	// PerAPIKey enables per-API-key rate limiting
	PerAPIKey bool
}

// RateLimiter manages rate limiting using token bucket algorithm
type RateLimiter struct {
	config  RateLimitConfig
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
}

// tokenBucket implements the token bucket algorithm
type tokenBucket struct {
	tokens         int
	maxTokens      int
	refillRate     int // tokens per second
	lastRefillTime time.Time
	mu             sync.Mutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.RequestsPerSecond == 0 {
		config.RequestsPerSecond = 100 // Default: 100 req/s
	}
	if config.Burst == 0 {
		config.Burst = config.RequestsPerSecond * 2 // Default: 2x burst
	}
	
	return &RateLimiter{
		config:  config,
		buckets: make(map[string]*tokenBucket),
	}
}

// Allow checks if a request should be allowed
func (rl *RateLimiter) Allow(ctx context.Context) error {
	if !rl.config.Enabled {
		return nil // Rate limiting disabled
	}
	
	// Determine the key for rate limiting
	key := rl.getKeyFromContext(ctx)
	
	// Get or create bucket for this key
	bucket := rl.getOrCreateBucket(key)
	
	// Try to consume a token
	if !bucket.consume() {
		return fmt.Errorf("rate limit exceeded for '%s'", key)
	}
	
	return nil
}

// getKeyFromContext extracts the rate limit key from context
func (rl *RateLimiter) getKeyFromContext(ctx context.Context) string {
	user, ok := UserFromContext(ctx)
	if !ok {
		return "anonymous"
	}
	
	// Use different keys based on configuration priority
	// PerAPIKey takes precedence over PerTenant
	if rl.config.PerAPIKey && user.ID != "" {
		return "user:" + user.ID
	}
	
	if rl.config.PerTenant && user.TenantID != "" {
		return "tenant:" + user.TenantID
	}
	
	return "global"
}

// getOrCreateBucket gets or creates a token bucket for a key
func (rl *RateLimiter) getOrCreateBucket(key string) *tokenBucket {
	rl.mu.RLock()
	bucket, exists := rl.buckets[key]
	rl.mu.RUnlock()
	
	if exists {
		return bucket
	}
	
	// Create new bucket
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	// Double-check after acquiring write lock
	if bucket, exists := rl.buckets[key]; exists {
		return bucket
	}
	
	bucket = &tokenBucket{
		tokens:         rl.config.Burst,
		maxTokens:      rl.config.Burst,
		refillRate:     rl.config.RequestsPerSecond,
		lastRefillTime: time.Now(),
	}
	
	rl.buckets[key] = bucket
	return bucket
}

// consume tries to consume one token from the bucket
func (tb *tokenBucket) consume() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	
	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefillTime)
	tokensToAdd := int(elapsed.Seconds() * float64(tb.refillRate))
	
	if tokensToAdd > 0 {
		tb.tokens += tokensToAdd
		if tb.tokens > tb.maxTokens {
			tb.tokens = tb.maxTokens
		}
		tb.lastRefillTime = now
	}
	
	// Try to consume a token
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}
	
	return false
}

// GetStats returns rate limiting statistics for a key
func (rl *RateLimiter) GetStats(key string) (tokens int, maxTokens int, exists bool) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	bucket, exists := rl.buckets[key]
	if !exists {
		return 0, 0, false
	}
	
	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	
	return bucket.tokens, bucket.maxTokens, true
}

// Reset resets the rate limiter for a specific key
func (rl *RateLimiter) Reset(key string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	delete(rl.buckets, key)
}

// ResetAll resets all rate limiters
func (rl *RateLimiter) ResetAll() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	rl.buckets = make(map[string]*tokenBucket)
}

// CleanupOldBuckets removes unused buckets (called periodically)
func (rl *RateLimiter) CleanupOldBuckets(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	for key, bucket := range rl.buckets {
		bucket.mu.Lock()
		age := now.Sub(bucket.lastRefillTime)
		bucket.mu.Unlock()
		
		if age > maxAge {
			delete(rl.buckets, key)
		}
	}
}

// StartCleanupWorker starts a background worker to clean up old buckets
func (rl *RateLimiter) StartCleanupWorker(ctx context.Context, cleanupInterval, maxAge time.Duration) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.CleanupOldBuckets(maxAge)
		}
	}
}
