package reliability

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// BackpressureConfig holds backpressure configuration
type BackpressureConfig struct {
	// Enabled controls whether backpressure is enabled
	Enabled bool
	
	// MaxConcurrentWrites is the maximum concurrent writes allowed
	MaxConcurrentWrites int
	
	// MaxConcurrentReads is the maximum concurrent reads allowed
	MaxConcurrentReads int
	
	// MaxQueueSize is the maximum size of the request queue
	MaxQueueSize int
	
	// QueueTimeout is how long to wait in queue before rejecting
	QueueTimeout time.Duration
}

// DefaultBackpressureConfig returns default backpressure configuration
func DefaultBackpressureConfig() BackpressureConfig {
	return BackpressureConfig{
		Enabled:             true,
		MaxConcurrentWrites: 100,
		MaxConcurrentReads:  1000,
		MaxQueueSize:        1000,
		QueueTimeout:        10 * time.Second,
	}
}

// BackpressureManager manages backpressure for writes and reads
type BackpressureManager struct {
	config           BackpressureConfig
	writeSemaphore   chan struct{}
	readSemaphore    chan struct{}
	writeQueueSize   int
	readQueueSize    int
	mu               sync.RWMutex
}

// NewBackpressureManager creates a new backpressure manager
func NewBackpressureManager(config BackpressureConfig) *BackpressureManager {
	if config.MaxConcurrentWrites == 0 {
		config.MaxConcurrentWrites = 100
	}
	if config.MaxConcurrentReads == 0 {
		config.MaxConcurrentReads = 1000
	}
	if config.MaxQueueSize == 0 {
		config.MaxQueueSize = 1000
	}
	if config.QueueTimeout == 0 {
		config.QueueTimeout = 10 * time.Second
	}
	
	return &BackpressureManager{
		config:         config,
		writeSemaphore: make(chan struct{}, config.MaxConcurrentWrites),
		readSemaphore:  make(chan struct{}, config.MaxConcurrentReads),
	}
}

// AcquireWrite acquires permission to perform a write
func (bpm *BackpressureManager) AcquireWrite(ctx context.Context) (func(), error) {
	if !bpm.config.Enabled {
		return func() {}, nil
	}
	
	// Check queue size
	bpm.mu.Lock()
	if bpm.writeQueueSize >= bpm.config.MaxQueueSize {
		bpm.mu.Unlock()
		return nil, fmt.Errorf("write queue full (%d requests)", bpm.config.MaxQueueSize)
	}
	bpm.writeQueueSize++
	bpm.mu.Unlock()
	
	// Try to acquire semaphore with timeout
	ctx, cancel := context.WithTimeout(ctx, bpm.config.QueueTimeout)
	defer cancel()
	
	select {
	case bpm.writeSemaphore <- struct{}{}:
		// Successfully acquired
		bpm.mu.Lock()
		bpm.writeQueueSize--
		bpm.mu.Unlock()
		
		// Return release function
		return func() {
			<-bpm.writeSemaphore
		}, nil
	case <-ctx.Done():
		// Timeout or cancellation
		bpm.mu.Lock()
		bpm.writeQueueSize--
		bpm.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for write slot: %w", ctx.Err())
	}
}

// AcquireRead acquires permission to perform a read
func (bpm *BackpressureManager) AcquireRead(ctx context.Context) (func(), error) {
	if !bpm.config.Enabled {
		return func() {}, nil
	}
	
	// Check queue size
	bpm.mu.Lock()
	if bpm.readQueueSize >= bpm.config.MaxQueueSize {
		bpm.mu.Unlock()
		return nil, fmt.Errorf("read queue full (%d requests)", bpm.config.MaxQueueSize)
	}
	bpm.readQueueSize++
	bpm.mu.Unlock()
	
	// Try to acquire semaphore with timeout
	ctx, cancel := context.WithTimeout(ctx, bpm.config.QueueTimeout)
	defer cancel()
	
	select {
	case bpm.readSemaphore <- struct{}{}:
		// Successfully acquired
		bpm.mu.Lock()
		bpm.readQueueSize--
		bpm.mu.Unlock()
		
		// Return release function
		return func() {
			<-bpm.readSemaphore
		}, nil
	case <-ctx.Done():
		// Timeout or cancellation
		bpm.mu.Lock()
		bpm.readQueueSize--
		bpm.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for read slot: %w", ctx.Err())
	}
}

// GetStats returns current backpressure statistics
func (bpm *BackpressureManager) GetStats() BackpressureStats {
	bpm.mu.RLock()
	defer bpm.mu.RUnlock()
	
	return BackpressureStats{
		ActiveWrites:   len(bpm.writeSemaphore),
		ActiveReads:    len(bpm.readSemaphore),
		QueuedWrites:   bpm.writeQueueSize,
		QueuedReads:    bpm.readQueueSize,
		MaxWrites:      bpm.config.MaxConcurrentWrites,
		MaxReads:       bpm.config.MaxConcurrentReads,
	}
}

// BackpressureStats holds backpressure statistics
type BackpressureStats struct {
	ActiveWrites int `json:"active_writes"`
	ActiveReads  int `json:"active_reads"`
	QueuedWrites int `json:"queued_writes"`
	QueuedReads  int `json:"queued_reads"`
	MaxWrites    int `json:"max_writes"`
	MaxReads     int `json:"max_reads"`
}
