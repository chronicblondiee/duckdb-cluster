package module

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
)

// Module represents a component that can be started and stopped independently
type Module interface {
	// Name returns the unique identifier for this module
	Name() string
	
	// Dependencies returns the names of modules that must be started before this one
	Dependencies() []string
	
	// Init initializes the module with the given context
	Init(ctx context.Context) error
	
	// Start starts the module
	Start(ctx context.Context) error
	
	// Stop stops the module gracefully
	Stop() error
}

// Manager manages the lifecycle of multiple modules
type Manager struct {
	modules  map[string]Module
	running  map[string]bool
	mu       sync.RWMutex
	startCtx context.Context
}

// NewManager creates a new module manager
func NewManager() *Manager {
	return &Manager{
		modules: make(map[string]Module),
		running: make(map[string]bool),
	}
}

// Register registers a module with the manager
func (m *Manager) Register(mod Module) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	name := mod.Name()
	if _, exists := m.modules[name]; exists {
		return fmt.Errorf("module %s already registered", name)
	}
	
	m.modules[name] = mod
	slog.Debug("registered module", "name", name)
	return nil
}

// Start starts modules based on the target configuration
// Target can be: "all", "write", "read", "backend", or a comma-separated list of module names
func (m *Manager) Start(ctx context.Context, target string) error {
	m.mu.Lock()
	m.startCtx = ctx
	m.mu.Unlock()
	
	moduleNames, err := m.resolveTarget(target)
	if err != nil {
		return err
	}
	
	// Resolve dependencies and determine start order
	startOrder, err := m.resolveDependencies(moduleNames)
	if err != nil {
		return err
	}
	
	slog.Info("starting modules", "target", target, "count", len(startOrder), "order", startOrder)
	
	// Initialize and start modules in order
	for _, name := range startOrder {
		if err := m.startModule(ctx, name); err != nil {
			// On failure, stop all started modules
			m.StopAll()
			return fmt.Errorf("failed to start module %s: %w", name, err)
		}
	}
	
	slog.Info("all modules started successfully")
	return nil
}

// StopAll stops all running modules in reverse order
func (m *Manager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Get list of running modules
	var running []string
	for name := range m.running {
		if m.running[name] {
			running = append(running, name)
		}
	}
	
	if len(running) == 0 {
		return nil
	}
	
	slog.Info("stopping modules", "count", len(running))
	
	// Stop in reverse order
	var lastErr error
	for i := len(running) - 1; i >= 0; i-- {
		name := running[i]
		mod := m.modules[name]
		
		slog.Debug("stopping module", "name", name)
		if err := mod.Stop(); err != nil {
			slog.Error("failed to stop module", "name", name, "error", err)
			lastErr = err
		}
		m.running[name] = false
	}
	
	return lastErr
}

// IsRunning returns true if the named module is currently running
func (m *Manager) IsRunning(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running[name]
}

// GetModule returns the module with the given name
func (m *Manager) GetModule(name string) (Module, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	mod, exists := m.modules[name]
	if !exists {
		return nil, fmt.Errorf("module %s not found", name)
	}
	return mod, nil
}

// startModule initializes and starts a single module
func (m *Manager) startModule(ctx context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	mod, exists := m.modules[name]
	if !exists {
		return fmt.Errorf("module %s not registered", name)
	}
	
	if m.running[name] {
		return nil // Already running
	}
	
	slog.Debug("initializing module", "name", name)
	if err := mod.Init(ctx); err != nil {
		return fmt.Errorf("init failed: %w", err)
	}
	
	slog.Debug("starting module", "name", name)
	if err := mod.Start(ctx); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}
	
	m.running[name] = true
	return nil
}

// resolveTarget converts a target string into a list of module names
func (m *Manager) resolveTarget(target string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	switch target {
	case "all":
		// All registered modules
		var names []string
		for name := range m.modules {
			names = append(names, name)
		}
		return names, nil
		
	case "write":
		// Write path: server, distributor, ingester
		return []string{"server", "distributor", "ingester"}, nil
		
	case "read":
		// Read path: server, query-frontend, querier
		return []string{"server", "query-frontend", "querier"}, nil
		
	case "backend":
		// Backend: server, admin, compactor
		return []string{"server", "admin", "compactor"}, nil
		
	default:
		// Assume it's a comma-separated list of module names
		// For now, just return error if not a known target
		return nil, fmt.Errorf("unknown target: %s (valid targets: all, write, read, backend)", target)
	}
}

// resolveDependencies performs topological sort on modules to determine start order
func (m *Manager) resolveDependencies(moduleNames []string) ([]string, error) {
	// Build adjacency list and in-degree map
	graph := make(map[string][]string)
	inDegree := make(map[string]int)
	
	// Initialize
	for _, name := range moduleNames {
		inDegree[name] = 0
		graph[name] = []string{}
	}
	
	// Build graph
	for _, name := range moduleNames {
		mod, exists := m.modules[name]
		if !exists {
			return nil, fmt.Errorf("module %s not registered", name)
		}
		
		deps := mod.Dependencies()
		for _, dep := range deps {
			// Check if dependency is in the list of modules to start
			if _, needed := inDegree[dep]; !needed {
				// Dependency not in target list, add it
				inDegree[dep] = 0
				graph[dep] = []string{}
				moduleNames = append(moduleNames, dep)
			}
			graph[dep] = append(graph[dep], name)
			inDegree[name]++
		}
	}
	
	// Kahn's algorithm for topological sort
	var queue []string
	for name, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, name)
		}
	}
	
	// Sort queue for deterministic ordering
	sort.Strings(queue)
	
	var result []string
	for len(queue) > 0 {
		// Pop first element
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		
		// Process neighbors
		neighbors := graph[current]
		sort.Strings(neighbors) // Deterministic ordering
		
		for _, neighbor := range neighbors {
			inDegree[neighbor]--
			if inDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
				sort.Strings(queue) // Keep sorted
			}
		}
	}
	
	// Check for cycles
	if len(result) != len(inDegree) {
		return nil, fmt.Errorf("circular dependency detected in modules")
	}
	
	return result, nil
}
