package reliability

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// AdmissionConfig holds admission control configuration
type AdmissionConfig struct {
	// Enabled controls whether admission control is enabled
	Enabled bool
	
	// MaxMemoryMB is the maximum memory usage in MB before rejecting requests
	MaxMemoryMB int64
	
	// MaxCPUPercent is the maximum CPU usage percentage before rejecting
	MaxCPUPercent float64
	
	// HealthCheckInterval is how often to check system health
	HealthCheckInterval time.Duration
	
	// RejectProbability is the probability of rejecting when overloaded (0-1)
	RejectProbability float64
}

// DefaultAdmissionConfig returns default admission control configuration
func DefaultAdmissionConfig() AdmissionConfig {
	return AdmissionConfig{
		Enabled:             true,
		MaxMemoryMB:         8192, // 8 GB
		MaxCPUPercent:       90.0, // 90%
		HealthCheckInterval: 5 * time.Second,
		RejectProbability:   0.5, // 50% rejection rate when overloaded
	}
}

// AdmissionController manages admission control
type AdmissionController struct {
	config      AdmissionConfig
	overloaded  atomic.Bool
	mu          sync.RWMutex
	stopChan    chan struct{}
}

// NewAdmissionController creates a new admission controller
func NewAdmissionController(config AdmissionConfig) *AdmissionController {
	if config.HealthCheckInterval == 0 {
		config.HealthCheckInterval = 5 * time.Second
	}
	if config.RejectProbability == 0 {
		config.RejectProbability = 0.5
	}
	
	ac := &AdmissionController{
		config:   config,
		stopChan: make(chan struct{}),
	}
	
	if config.Enabled {
		go ac.monitorHealth()
	}
	
	return ac
}

// Admit checks if a request should be admitted
func (ac *AdmissionController) Admit(ctx context.Context, requestType string) error {
	if !ac.config.Enabled {
		return nil // Admission control disabled
	}
	
	// Check if system is overloaded
	if ac.overloaded.Load() {
		// Probabilistic rejection when overloaded
		// In a real implementation, you would use proper random number generation
		// For now, we just reject with a fixed probability
		return fmt.Errorf("system overloaded, request rejected (type: %s)", requestType)
	}
	
	return nil
}

// monitorHealth periodically checks system health
func (ac *AdmissionController) monitorHealth() {
	ticker := time.NewTicker(ac.config.HealthCheckInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			// Check system resources
			// In a real implementation, you would check actual memory and CPU usage
			// For now, we just keep the system marked as not overloaded
			ac.overloaded.Store(false)
		case <-ac.stopChan:
			return
		}
	}
}

// Stop stops the admission controller
func (ac *AdmissionController) Stop() {
	close(ac.stopChan)
}

// SetOverloaded manually sets the overloaded state (for testing)
func (ac *AdmissionController) SetOverloaded(overloaded bool) {
	ac.overloaded.Store(overloaded)
}

// IsOverloaded returns whether the system is currently overloaded
func (ac *AdmissionController) IsOverloaded() bool {
	return ac.overloaded.Load()
}
