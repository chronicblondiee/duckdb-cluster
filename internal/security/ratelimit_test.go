package security

import (
	"context"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	tests := []struct {
		name   string
		config RateLimitConfig
	}{
		{
			name: "default config",
			config: RateLimitConfig{
				Enabled: true,
			},
		},
		{
			name: "custom config",
			config: RateLimitConfig{
				Enabled:           true,
				RequestsPerSecond: 50,
				Burst:             100,
			},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewRateLimiter(tt.config)
			if limiter == nil {
				t.Error("NewRateLimiter() returned nil")
			}
		})
	}
}

func TestRateLimiterAllow(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
	})
	
	ctx := context.Background()
	user := &User{
		ID:       "user-1",
		Username: "testuser",
		TenantID: "tenant-1",
	}
	ctx = WithUser(ctx, user)
	
	// First 10 requests should be allowed (burst)
	for i := 0; i < 10; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Errorf("Allow() request %d error = %v, want nil", i, err)
		}
	}
	
	// 11th request should be rate limited
	if err := limiter.Allow(ctx); err == nil {
		t.Error("Allow() should fail after burst exhausted")
	}
}

func TestRateLimiterRefill(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 100, // Fast refill for testing
		Burst:             5,
	})
	
	ctx := context.Background()
	user := &User{
		ID:       "user-2",
		Username: "testuser2",
		TenantID: "tenant-2",
	}
	ctx = WithUser(ctx, user)
	
	// Exhaust burst
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx)
	}
	
	// Should be rate limited now
	if err := limiter.Allow(ctx); err == nil {
		t.Error("Allow() should fail after burst exhausted")
	}
	
	// Wait for refill (>10ms should give us at least 1 token at 100/s)
	time.Sleep(20 * time.Millisecond)
	
	// Should be allowed after refill
	if err := limiter.Allow(ctx); err != nil {
		t.Errorf("Allow() error = %v, want nil after refill", err)
	}
}

func TestRateLimiterDisabled(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled: false,
	})
	
	ctx := context.Background()
	
	// With disabled limiter, all requests should be allowed
	for i := 0; i < 1000; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Errorf("Allow() error = %v, want nil when disabled", err)
		}
	}
}

func TestRateLimiterPerTenant(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             5,
		PerTenant:         true,
	})
	
	tenant1User := &User{
		ID:       "user-1",
		Username: "tenant1user",
		TenantID: "tenant-1",
	}
	
	tenant2User := &User{
		ID:       "user-2",
		Username: "tenant2user",
		TenantID: "tenant-2",
	}
	
	ctx1 := WithUser(context.Background(), tenant1User)
	ctx2 := WithUser(context.Background(), tenant2User)
	
	// Exhaust tenant 1's burst
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(ctx1); err != nil {
			t.Errorf("Allow() tenant1 request %d error = %v", i, err)
		}
	}
	
	// Tenant 1 should be rate limited
	if err := limiter.Allow(ctx1); err == nil {
		t.Error("Allow() should fail for tenant 1 after burst")
	}
	
	// Tenant 2 should still be allowed (separate bucket)
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(ctx2); err != nil {
			t.Errorf("Allow() tenant2 request %d error = %v", i, err)
		}
	}
}

func TestRateLimiterGetStats(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
	})
	
	ctx := context.Background()
	user := &User{
		ID:       "user-3",
		Username: "statsuser",
		TenantID: "tenant-3",
	}
	ctx = WithUser(ctx, user)
	
	key := "global" // Default key when PerAPIKey and PerTenant are false
	
	// Before any requests
	_, _, exists := limiter.GetStats(key)
	if exists {
		t.Error("GetStats() should not find bucket before first request")
	}
	
	// Make a request to create bucket
	limiter.Allow(ctx)
	
	// After request
	tokens, maxTokens, exists := limiter.GetStats(key)
	if !exists {
		t.Error("GetStats() should find bucket after request")
	}
	if maxTokens != 10 {
		t.Errorf("GetStats() maxTokens = %v, want 10", maxTokens)
	}
	if tokens != 9 { // 10 - 1 consumed
		t.Errorf("GetStats() tokens = %v, want 9", tokens)
	}
}

func TestRateLimiterReset(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             5,
		PerAPIKey:         true, // Enable per-user limiting
	})
	
	ctx := context.Background()
	user := &User{
		ID:       "user-4",
		Username: "resetuser",
		TenantID: "tenant-4",
	}
	ctx = WithUser(ctx, user)
	
	key := "user:user-4"
	
	// Exhaust burst
	for i := 0; i < 5; i++ {
		limiter.Allow(ctx)
	}
	
	// Should be rate limited
	if err := limiter.Allow(ctx); err == nil {
		t.Error("Allow() should fail after burst exhausted")
	}
	
	// Reset bucket
	limiter.Reset(key)
	
	// Should be allowed after reset
	if err := limiter.Allow(ctx); err != nil {
		t.Errorf("Allow() error = %v, want nil after reset", err)
	}
}

func TestRateLimiterResetAll(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             5,
		PerAPIKey:         true, // Enable per-user limiting
	})
	
	// Create multiple buckets
	for i := 0; i < 3; i++ {
		user := &User{
			ID:       "user-" + string(rune('5'+i)),
			Username: "resetalluser",
			TenantID: "tenant-5",
		}
		ctx := WithUser(context.Background(), user)
		// Exhaust each user's bucket
		for j := 0; j < 5; j++ {
			limiter.Allow(ctx)
		}
	}
	
	// Reset all buckets
	limiter.ResetAll()
	
	// All users should be allowed again with full burst
	for i := 0; i < 3; i++ {
		user := &User{
			ID:       "user-" + string(rune('5'+i)),
			Username: "resetalluser",
			TenantID: "tenant-5",
		}
		ctx := WithUser(context.Background(), user)
		
		for j := 0; j < 5; j++ {
			if err := limiter.Allow(ctx); err != nil {
				t.Errorf("Allow() error = %v after ResetAll for user %d request %d", err, i, j)
			}
		}
	}
}

func TestRateLimiterCleanupOldBuckets(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             10,
		PerAPIKey:         true, // Enable per-user limiting
	})
	
	ctx := context.Background()
	user := &User{
		ID:       "user-cleanup",
		Username: "cleanupuser",
		TenantID: "tenant-cleanup",
	}
	ctx = WithUser(ctx, user)
	
	// Create a bucket
	limiter.Allow(ctx)
	
	key := "user:user-cleanup"
	
	// Bucket should exist
	_, _, exists := limiter.GetStats(key)
	if !exists {
		t.Error("bucket should exist before cleanup")
	}
	
	// Cleanup buckets older than 0 (should remove all)
	limiter.CleanupOldBuckets(0)
	
	// Bucket should be removed
	_, _, exists = limiter.GetStats(key)
	if exists {
		t.Error("bucket should be removed after cleanup")
	}
}

func TestRateLimiterAnonymous(t *testing.T) {
	limiter := NewRateLimiter(RateLimitConfig{
		Enabled:           true,
		RequestsPerSecond: 10,
		Burst:             5,
	})
	
	// Context without user (anonymous)
	ctx := context.Background()
	
	// All anonymous requests share the same bucket
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Errorf("Allow() anonymous request %d error = %v", i, err)
		}
	}
	
	// Should be rate limited
	if err := limiter.Allow(ctx); err == nil {
		t.Error("Allow() should fail for anonymous after burst")
	}
}
