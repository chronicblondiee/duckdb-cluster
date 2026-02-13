package reliability

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// DegradationMode represents the current degradation mode
type DegradationMode int

const (
	// ModeNormal indicates normal operation
	ModeNormal DegradationMode = iota
	
	// ModeDegraded indicates degraded operation (reduced functionality)
	ModeDegraded
	
	// ModeReadOnly indicates read-only mode (no writes allowed)
	ModeReadOnly
	
	// ModeMaintenance indicates maintenance mode (minimal functionality)
	ModeMaintenance
)

func (m DegradationMode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeDegraded:
		return "degraded"
	case ModeReadOnly:
		return "read_only"
	case ModeMaintenance:
		return "maintenance"
	default:
		return "unknown"
	}
}

// DegradationConfig holds graceful degradation configuration
type DegradationConfig struct {
	// Enabled controls whether graceful degradation is enabled
	Enabled bool
	
	// AutoDegradeOnError enables automatic degradation on repeated errors
	AutoDegradeOnError bool
	
	// ErrorThreshold is the number of errors before degrading
	ErrorThreshold int
	
	// RecoveryCheckInterval is how often to check for recovery
	RecoveryCheckInterval time.Duration
}

// DefaultDegradationConfig returns default degradation configuration
func DefaultDegradationConfig() DegradationConfig {
	return DegradationConfig{
		Enabled:               true,
		AutoDegradeOnError:    true,
		ErrorThreshold:        10,
		RecoveryCheckInterval: 30 * time.Second,
	}
}

// DegradationManager manages graceful degradation
type DegradationManager struct {
	config       DegradationConfig
	mode         DegradationMode
	errorCount   int
	lastError    time.Time
	mu           sync.RWMutex
	stopChan     chan struct{}
}

// NewDegradationManager creates a new degradation manager
func NewDegradationManager(config DegradationConfig) *DegradationManager {
	if config.RecoveryCheckInterval == 0 {
		config.RecoveryCheckInterval = 30 * time.Second
	}
	if config.ErrorThreshold == 0 {
		config.ErrorThreshold = 10
	}
	
	dm := &DegradationManager{
		config:   config,
		mode:     ModeNormal,
		stopChan: make(chan struct{}),
	}
	
	if config.Enabled && config.AutoDegradeOnError {
		go dm.monitorRecovery()
	}
	
	return dm
}

// GetMode returns the current degradation mode
func (dm *DegradationManager) GetMode() DegradationMode {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.mode
}

// SetMode sets the degradation mode
func (dm *DegradationManager) SetMode(mode DegradationMode) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.mode = mode
	dm.errorCount = 0 // Reset error count on manual mode change
}

// CheckWrite checks if writes are allowed in current mode
func (dm *DegradationManager) CheckWrite(ctx context.Context) error {
	if !dm.config.Enabled {
		return nil
	}
	
	mode := dm.GetMode()
	switch mode {
	case ModeNormal, ModeDegraded:
		return nil // Writes allowed
	case ModeReadOnly:
		return fmt.Errorf("system in read-only mode, writes not allowed")
	case ModeMaintenance:
		return fmt.Errorf("system in maintenance mode, writes not allowed")
	default:
		return fmt.Errorf("unknown degradation mode: %v", mode)
	}
}

// CheckRead checks if reads are allowed in current mode
func (dm *DegradationManager) CheckRead(ctx context.Context) error {
	if !dm.config.Enabled {
		return nil
	}
	
	mode := dm.GetMode()
	switch mode {
	case ModeNormal, ModeDegraded, ModeReadOnly:
		return nil // Reads allowed
	case ModeMaintenance:
		return fmt.Errorf("system in maintenance mode, reads not allowed")
	default:
		return fmt.Errorf("unknown degradation mode: %v", mode)
	}
}

// RecordError records an error and potentially triggers degradation
func (dm *DegradationManager) RecordError(err error) {
	if !dm.config.Enabled || !dm.config.AutoDegradeOnError {
		return
	}
	
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	dm.errorCount++
	dm.lastError = time.Now()
	
	// Check if we should degrade
	if dm.errorCount >= dm.config.ErrorThreshold {
		if dm.mode == ModeNormal {
			dm.mode = ModeDegraded
		} else if dm.mode == ModeDegraded {
			dm.mode = ModeReadOnly
		}
	}
}

// monitorRecovery periodically checks if the system can recover
func (dm *DegradationManager) monitorRecovery() {
	ticker := time.NewTicker(dm.config.RecoveryCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			dm.checkRecovery()
		case <-dm.stopChan:
			return
		}
	}
}

// checkRecovery checks if the system can recover from degradation
func (dm *DegradationManager) checkRecovery() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	
	// If no errors for a while, try to recover
	if time.Since(dm.lastError) > dm.config.RecoveryCheckInterval*2 {
		if dm.mode == ModeReadOnly {
			dm.mode = ModeDegraded
			dm.errorCount = 0
		} else if dm.mode == ModeDegraded {
			dm.mode = ModeNormal
			dm.errorCount = 0
		}
	}
}

// Stop stops the degradation manager
func (dm *DegradationManager) Stop() {
	close(dm.stopChan)
}

// GetStats returns degradation statistics
func (dm *DegradationManager) GetStats() DegradationStats {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	
	return DegradationStats{
		Mode:            dm.mode.String(),
		ErrorCount:      dm.errorCount,
		LastError:       dm.lastError,
		WritesAllowed:   dm.mode == ModeNormal || dm.mode == ModeDegraded,
		ReadsAllowed:    dm.mode != ModeMaintenance,
	}
}

// DegradationStats holds degradation statistics
type DegradationStats struct {
	Mode          string    `json:"mode"`
	ErrorCount    int       `json:"error_count"`
	LastError     time.Time `json:"last_error"`
	WritesAllowed bool      `json:"writes_allowed"`
	ReadsAllowed  bool      `json:"reads_allowed"`
}
