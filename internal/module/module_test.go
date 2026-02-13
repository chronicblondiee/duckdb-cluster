package module

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Mock module for testing
type mockModule struct {
	name         string
	dependencies []string
	initCalled   bool
	startCalled  bool
	stopCalled   bool
	initErr      error
	startErr     error
	stopErr      error
	mu           sync.Mutex
}

func (m *mockModule) Name() string {
	return m.name
}

func (m *mockModule) Dependencies() []string {
	return m.dependencies
}

func (m *mockModule) Init(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCalled = true
	return m.initErr
}

func (m *mockModule) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = true
	return m.startErr
}

func (m *mockModule) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
	return m.stopErr
}

func (m *mockModule) wasCalled(method string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch method {
	case "init":
		return m.initCalled
	case "start":
		return m.startCalled
	case "stop":
		return m.stopCalled
	}
	return false
}

func TestModuleRegistration(t *testing.T) {
	mgr := NewManager()
	
	mod1 := &mockModule{name: "test-module"}
	
	// Register should succeed
	if err := mgr.Register(mod1); err != nil {
		t.Fatalf("failed to register module: %v", err)
	}
	
	// Duplicate registration should fail
	if err := mgr.Register(mod1); err == nil {
		t.Fatal("expected error when registering duplicate module")
	}
	
	// GetModule should return the registered module
	retrieved, err := mgr.GetModule("test-module")
	if err != nil {
		t.Fatalf("failed to get module: %v", err)
	}
	if retrieved != mod1 {
		t.Fatal("retrieved module is not the same as registered")
	}
	
	// GetModule for non-existent module should fail
	if _, err := mgr.GetModule("non-existent"); err == nil {
		t.Fatal("expected error when getting non-existent module")
	}
}

func TestModuleStartStop(t *testing.T) {
	mgr := NewManager()
	
	mod := &mockModule{name: "test"}
	if err := mgr.Register(mod); err != nil {
		t.Fatalf("failed to register module: %v", err)
	}
	
	ctx := context.Background()
	
	// Start the module
	if err := mgr.Start(ctx, "all"); err != nil {
		t.Fatalf("failed to start modules: %v", err)
	}
	
	// Check that init and start were called
	if !mod.wasCalled("init") {
		t.Error("Init was not called")
	}
	if !mod.wasCalled("start") {
		t.Error("Start was not called")
	}
	
	// Check that module is running
	if !mgr.IsRunning("test") {
		t.Error("module should be running")
	}
	
	// Stop the module
	if err := mgr.StopAll(); err != nil {
		t.Fatalf("failed to stop modules: %v", err)
	}
	
	// Check that stop was called
	if !mod.wasCalled("stop") {
		t.Error("Stop was not called")
	}
	
	// Check that module is not running
	if mgr.IsRunning("test") {
		t.Error("module should not be running")
	}
}

func TestModuleDependencyResolution(t *testing.T) {
	mgr := NewManager()
	
	// Create modules with dependencies: C depends on B, B depends on A
	modA := &mockModule{name: "A", dependencies: []string{}}
	modB := &mockModule{name: "B", dependencies: []string{"A"}}
	modC := &mockModule{name: "C", dependencies: []string{"B"}}
	
	mgr.Register(modA)
	mgr.Register(modB)
	mgr.Register(modC)
	
	ctx := context.Background()
	
	// Start all modules
	if err := mgr.Start(ctx, "all"); err != nil {
		t.Fatalf("failed to start modules: %v", err)
	}
	
	// All modules should be running
	if !mgr.IsRunning("A") || !mgr.IsRunning("B") || !mgr.IsRunning("C") {
		t.Error("all modules should be running")
	}
	
	// Check that modules were started in dependency order
	// We can't easily check the order without instrumenting the mock,
	// but we can verify they all started successfully
	if !modA.wasCalled("start") || !modB.wasCalled("start") || !modC.wasCalled("start") {
		t.Error("all modules should have been started")
	}
}

// trackingModule wraps mockModule to track start order
type trackingModule struct {
	*mockModule
	startOrder *[]string
	mu         *sync.Mutex
}

func (t *trackingModule) Start(ctx context.Context) error {
	t.mu.Lock()
	*t.startOrder = append(*t.startOrder, t.name)
	t.mu.Unlock()
	// Small delay to make race conditions more likely if ordering is wrong
	time.Sleep(time.Millisecond)
	return t.mockModule.Start(ctx)
}

func TestModuleDependencyOrder(t *testing.T) {
	mgr := NewManager()
	
	// Track start order
	var startOrder []string
	var mu sync.Mutex
	
	createModule := func(name string, deps []string) *trackingModule {
		return &trackingModule{
			mockModule: &mockModule{name: name, dependencies: deps},
			startOrder: &startOrder,
			mu:         &mu,
		}
	}
	
	// Create dependency chain: D -> C -> B -> A
	modA := createModule("A", []string{})
	modB := createModule("B", []string{"A"})
	modC := createModule("C", []string{"B"})
	modD := createModule("D", []string{"C"})
	
	mgr.Register(modA)
	mgr.Register(modB)
	mgr.Register(modC)
	mgr.Register(modD)
	
	ctx := context.Background()
	
	if err := mgr.Start(ctx, "all"); err != nil {
		t.Fatalf("failed to start modules: %v", err)
	}
	
	// Verify start order
	expectedOrder := []string{"A", "B", "C", "D"}
	if len(startOrder) != len(expectedOrder) {
		t.Fatalf("wrong number of modules started: got %d, want %d", len(startOrder), len(expectedOrder))
	}
	
	for i, name := range expectedOrder {
		if startOrder[i] != name {
			t.Errorf("wrong start order at position %d: got %s, want %s (full order: %v)", 
				i, startOrder[i], name, startOrder)
		}
	}
}

func TestModuleCircularDependency(t *testing.T) {
	mgr := NewManager()
	
	// Create circular dependency: A -> B -> C -> A
	modA := &mockModule{name: "A", dependencies: []string{"C"}}
	modB := &mockModule{name: "B", dependencies: []string{"A"}}
	modC := &mockModule{name: "C", dependencies: []string{"B"}}
	
	mgr.Register(modA)
	mgr.Register(modB)
	mgr.Register(modC)
	
	ctx := context.Background()
	
	// Starting should fail due to circular dependency
	err := mgr.Start(ctx, "all")
	if err == nil {
		t.Fatal("expected error due to circular dependency")
	}
	
	if err.Error() != "circular dependency detected in modules" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestModuleStartupFailure(t *testing.T) {
	mgr := NewManager()
	
	modA := &mockModule{name: "A"}
	modB := &mockModule{name: "B", dependencies: []string{"A"}, startErr: fmt.Errorf("startup failed")}
	modC := &mockModule{name: "C", dependencies: []string{"B"}}
	
	mgr.Register(modA)
	mgr.Register(modB)
	mgr.Register(modC)
	
	ctx := context.Background()
	
	// Starting should fail when B fails to start
	err := mgr.Start(ctx, "all")
	if err == nil {
		t.Fatal("expected error due to module startup failure")
	}
	
	// A should have been started but then stopped during cleanup
	// B should have been initialized but failed to start
	// C should not have been started
	
	// All modules should be stopped after failure
	if mgr.IsRunning("A") || mgr.IsRunning("B") || mgr.IsRunning("C") {
		t.Error("no modules should be running after startup failure")
	}
}

func TestModuleComplexDependencies(t *testing.T) {
	mgr := NewManager()
	
	// Create a diamond dependency:
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	modA := &mockModule{name: "A", dependencies: []string{}}
	modB := &mockModule{name: "B", dependencies: []string{"A"}}
	modC := &mockModule{name: "C", dependencies: []string{"A"}}
	modD := &mockModule{name: "D", dependencies: []string{"B", "C"}}
	
	mgr.Register(modA)
	mgr.Register(modB)
	mgr.Register(modC)
	mgr.Register(modD)
	
	ctx := context.Background()
	
	if err := mgr.Start(ctx, "all"); err != nil {
		t.Fatalf("failed to start modules: %v", err)
	}
	
	// All modules should be running
	for _, name := range []string{"A", "B", "C", "D"} {
		if !mgr.IsRunning(name) {
			t.Errorf("module %s should be running", name)
		}
	}
}

func TestModuleTargets(t *testing.T) {
	tests := []struct {
		name           string
		target         string
		expectedModules []string
		shouldError    bool
	}{
		{
			name:           "all target",
			target:         "all",
			expectedModules: []string{"server", "distributor", "ingester"},
			shouldError:    false,
		},
		{
			name:           "write target",
			target:         "write",
			expectedModules: []string{"server", "distributor", "ingester"},
			shouldError:    false,
		},
		{
			name:           "read target",
			target:         "read",
			expectedModules: []string{"server", "query-frontend", "querier"},
			shouldError:    false,
		},
		{
			name:           "backend target",
			target:         "backend",
			expectedModules: []string{"server", "admin", "compactor"},
			shouldError:    false,
		},
		{
			name:        "unknown target",
			target:      "unknown",
			shouldError: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := NewManager()
			
			// Register all possible modules
			for _, name := range []string{"server", "distributor", "ingester", "query-frontend", "querier", "admin", "compactor"} {
				mgr.Register(&mockModule{name: name})
			}
			
			ctx := context.Background()
			err := mgr.Start(ctx, tt.target)
			
			if tt.shouldError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			
			// Check that expected modules are running
			for _, name := range tt.expectedModules {
				if !mgr.IsRunning(name) {
					t.Errorf("module %s should be running", name)
				}
			}
		})
	}
}
