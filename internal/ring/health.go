package ring

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// NodeHealth tracks the health status of a node
type NodeHealth struct {
	NodeID         string
	Addr           string
	State          NodeState
	FailureCount   int
	LastFailure    time.Time
	LastSuccess    time.Time
	ConsecutiveFail int
}

// NodeState represents the health state of a node
type NodeState int

const (
	// StateHealthy indicates the node is healthy and available
	StateHealthy NodeState = iota
	// StateUnhealthy indicates the node is experiencing issues
	StateUnhealthy
	// StateDown indicates the node is down or unreachable
	StateDown
)

func (s NodeState) String() string {
	switch s {
	case StateHealthy:
		return "healthy"
	case StateUnhealthy:
		return "unhealthy"
	case StateDown:
		return "down"
	default:
		return "unknown"
	}
}

// HealthTracker tracks node health and implements circuit breaker pattern
type HealthTracker struct {
	health map[string]*NodeHealth
	mu     sync.RWMutex
	
	// Circuit breaker thresholds
	failureThreshold    int           // failures before marking unhealthy
	downThreshold       int           // consecutive failures before marking down
	recoveryCheckPeriod time.Duration // how often to check unhealthy nodes
}

// NewHealthTracker creates a new health tracker
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		health:              make(map[string]*NodeHealth),
		failureThreshold:    3,
		downThreshold:       5,
		recoveryCheckPeriod: 30 * time.Second,
	}
}

// RecordSuccess records a successful operation for a node
func (ht *HealthTracker) RecordSuccess(nodeID string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	
	h, exists := ht.health[nodeID]
	if !exists {
		return
	}
	
	h.LastSuccess = time.Now()
	h.ConsecutiveFail = 0
	
	// Transition back to healthy if it was unhealthy
	if h.State != StateHealthy {
		slog.Info("node recovered", "node_id", nodeID, "previous_state", h.State.String())
		h.State = StateHealthy
		h.FailureCount = 0
	}
}

// RecordFailure records a failed operation for a node
func (ht *HealthTracker) RecordFailure(nodeID string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	
	h, exists := ht.health[nodeID]
	if !exists {
		return
	}
	
	h.FailureCount++
	h.ConsecutiveFail++
	h.LastFailure = time.Now()
	
	// Update state based on failure count
	previousState := h.State
	
	if h.ConsecutiveFail >= ht.downThreshold {
		h.State = StateDown
	} else if h.ConsecutiveFail >= ht.failureThreshold {
		h.State = StateUnhealthy
	}
	
	if h.State != previousState {
		slog.Warn("node state changed",
			"node_id", nodeID,
			"old_state", previousState.String(),
			"new_state", h.State.String(),
			"consecutive_failures", h.ConsecutiveFail,
		)
	}
}

// AddNode adds a node to health tracking
func (ht *HealthTracker) AddNode(nodeID, addr string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	
	if _, exists := ht.health[nodeID]; exists {
		return
	}
	
	ht.health[nodeID] = &NodeHealth{
		NodeID:      nodeID,
		Addr:        addr,
		State:       StateHealthy,
		LastSuccess: time.Now(),
	}
	
	slog.Debug("added node to health tracker", "node_id", nodeID, "addr", addr)
}

// RemoveNode removes a node from health tracking
func (ht *HealthTracker) RemoveNode(nodeID string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()
	
	delete(ht.health, nodeID)
	slog.Debug("removed node from health tracker", "node_id", nodeID)
}

// GetHealth returns the health status of a node
func (ht *HealthTracker) GetHealth(nodeID string) (*NodeHealth, bool) {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	
	h, exists := ht.health[nodeID]
	if !exists {
		return nil, false
	}
	
	// Return a copy to avoid race conditions
	copy := *h
	return &copy, true
}

// GetHealthyNodes returns all nodes in healthy state
func (ht *HealthTracker) GetHealthyNodes() []string {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	
	healthy := make([]string, 0)
	for nodeID, h := range ht.health {
		if h.State == StateHealthy {
			healthy = append(healthy, nodeID)
		}
	}
	return healthy
}

// IsHealthy returns true if the node is in healthy state
func (ht *HealthTracker) IsHealthy(nodeID string) bool {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	
	h, exists := ht.health[nodeID]
	return exists && h.State == StateHealthy
}

// StartRecoveryCheck starts a background goroutine to periodically check unhealthy nodes
func (ht *HealthTracker) StartRecoveryCheck(ctx context.Context, checkFunc func(nodeID, addr string) error) {
	ticker := time.NewTicker(ht.recoveryCheckPeriod)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ht.checkUnhealthyNodes(checkFunc)
		}
	}
}

// checkUnhealthyNodes checks unhealthy nodes to see if they've recovered
func (ht *HealthTracker) checkUnhealthyNodes(checkFunc func(nodeID, addr string) error) {
	ht.mu.RLock()
	unhealthy := make(map[string]string) // nodeID -> addr
	for nodeID, h := range ht.health {
		if h.State != StateHealthy {
			unhealthy[nodeID] = h.Addr
		}
	}
	ht.mu.RUnlock()
	
	// Check each unhealthy node
	for nodeID, addr := range unhealthy {
		if err := checkFunc(nodeID, addr); err != nil {
			slog.Debug("recovery check failed", "node_id", nodeID, "error", err)
			ht.RecordFailure(nodeID)
		} else {
			slog.Info("recovery check succeeded", "node_id", nodeID)
			ht.RecordSuccess(nodeID)
		}
	}
}

// GetAllHealth returns health status for all tracked nodes
func (ht *HealthTracker) GetAllHealth() []*NodeHealth {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	
	all := make([]*NodeHealth, 0, len(ht.health))
	for _, h := range ht.health {
		copy := *h
		all = append(all, &copy)
	}
	return all
}
