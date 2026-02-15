package ism

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
	"gopkg.in/yaml.v3"
)

const (
	policyFile = "_ism_policies.json"
	stateFile  = "_ism_state.json"
)

// Manager manages ISM policies and per-index state.
type Manager struct {
	mu       sync.RWMutex
	policies map[string]*Policy
	states   map[string]*IndexISMState // key = index name
	baseDir  string
	registry *index.Registry
}

// NewManager creates a new ISM manager.
func NewManager(baseDir string, registry *index.Registry) *Manager {
	return &Manager{
		policies: make(map[string]*Policy),
		states:   make(map[string]*IndexISMState),
		baseDir:  baseDir,
		registry: registry,
	}
}

// PutPolicy creates or updates a policy.
func (m *Manager) PutPolicy(p *Policy) error {
	if err := ValidatePolicy(p); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.policies[p.Name]; ok {
		p.Version = existing.Version + 1
	} else {
		p.Version = 1
	}
	p.LastUpdated = time.Now().UTC()

	m.policies[p.Name] = p
	return m.savePoliciesLocked()
}

// GetPolicy returns a policy by name.
func (m *Manager) GetPolicy(name string) (*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.policies[name]
	if !ok {
		return nil, fmt.Errorf("policy %q not found", name)
	}
	return p, nil
}

// DeletePolicy removes a policy. Does not detach from existing indices.
func (m *Manager) DeletePolicy(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.policies[name]; !ok {
		return fmt.Errorf("policy %q not found", name)
	}
	delete(m.policies, name)
	return m.savePoliciesLocked()
}

// ListPolicies returns all policies sorted by name.
func (m *Manager) ListPolicies() []*Policy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Policy, 0, len(m.policies))
	for _, p := range m.policies {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// AttachPolicy attaches a policy to an index by name.
func (m *Manager) AttachPolicy(indexName, policyName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.policies[policyName]
	if !ok {
		return fmt.Errorf("policy %q not found", policyName)
	}

	// Verify the index exists
	if _, err := m.registry.GetAny(indexName); err != nil {
		return fmt.Errorf("index %q not found", indexName)
	}

	m.states[indexName] = &IndexISMState{
		PolicyName:     policyName,
		PolicyVersion:  p.Version,
		CurrentState:   p.DefaultState,
		StateEnteredAt: time.Now().UTC(),
	}

	slog.Info("ISM: policy attached", "index", indexName, "policy", policyName)
	return m.saveStateLocked()
}

// DetachPolicy detaches any ISM policy from an index.
func (m *Manager) DetachPolicy(indexName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.states[indexName]; !ok {
		return fmt.Errorf("index %q has no ISM policy attached", indexName)
	}
	delete(m.states, indexName)

	slog.Info("ISM: policy detached", "index", indexName)
	return m.saveStateLocked()
}

// GetStatus returns the ISM status for a single index.
func (m *Manager) GetStatus(indexName string) (*ISMStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.states[indexName]
	if !ok {
		return nil, fmt.Errorf("index %q has no ISM policy attached", indexName)
	}
	status := s.toStatus(indexName)
	return &status, nil
}

// GetAllStatuses returns ISM status for all managed indices.
func (m *Manager) GetAllStatuses() []ISMStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ISMStatus, 0, len(m.states))
	for indexName, s := range m.states {
		result = append(result, s.toStatus(indexName))
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	return result
}

// RetryFailed resets the failed state on an index so the runner can retry.
func (m *Manager) RetryFailed(indexName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.states[indexName]
	if !ok {
		return fmt.Errorf("index %q has no ISM policy attached", indexName)
	}
	if !s.Failed {
		return fmt.Errorf("index %q is not in a failed state", indexName)
	}

	s.Failed = false
	s.RetryCount = 0
	s.LastError = ""

	slog.Info("ISM: retry reset", "index", indexName)
	return m.saveStateLocked()
}

// AutoAttach checks all policies' ISMTemplate patterns and attaches the
// highest-priority matching policy to the given index.
func (m *Manager) AutoAttach(indexName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Already has a policy? Don't override.
	if _, ok := m.states[indexName]; ok {
		return
	}

	type match struct {
		policy   *Policy
		priority int
	}

	var best *match
	for _, p := range m.policies {
		for _, tmpl := range p.ISMTemplate {
			if index.MatchGlob(tmpl.Pattern, indexName) {
				if best == nil || tmpl.Priority > best.priority {
					best = &match{policy: p, priority: tmpl.Priority}
				}
			}
		}
	}

	if best == nil {
		return
	}

	m.states[indexName] = &IndexISMState{
		PolicyName:     best.policy.Name,
		PolicyVersion:  best.policy.Version,
		CurrentState:   best.policy.DefaultState,
		StateEnteredAt: time.Now().UTC(),
	}

	slog.Info("ISM: auto-attached policy", "index", indexName, "policy", best.policy.Name)
	if err := m.saveStateLocked(); err != nil {
		slog.Error("ISM: failed to save state after auto-attach", "error", err)
	}
}

// LoadAll reads policies and state from disk.
func (m *Manager) LoadAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load policies
	pPath := filepath.Join(m.baseDir, "indices", policyFile)
	data, err := os.ReadFile(pPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read ISM policies: %w", err)
		}
	} else {
		var policies map[string]*Policy
		if err := json.Unmarshal(data, &policies); err != nil {
			return fmt.Errorf("parse ISM policies: %w", err)
		}
		m.policies = policies
	}

	// Load state
	sPath := filepath.Join(m.baseDir, "indices", stateFile)
	data, err = os.ReadFile(sPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read ISM state: %w", err)
		}
	} else {
		var states map[string]*IndexISMState
		if err := json.Unmarshal(data, &states); err != nil {
			return fmt.Errorf("parse ISM state: %w", err)
		}
		m.states = states
	}

	return nil
}

// LoadPolicyDir loads all .yaml/.yml policy files from a directory.
func (m *Manager) LoadPolicyDir(dir string) error {
	if dir == "" {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read policy directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Error("ISM: failed to read policy file", "path", path, "error", err)
			continue
		}

		var p Policy
		if err := yaml.Unmarshal(data, &p); err != nil {
			slog.Error("ISM: failed to parse policy file", "path", path, "error", err)
			continue
		}

		if err := m.PutPolicy(&p); err != nil {
			slog.Error("ISM: failed to load policy from file", "path", path, "error", err)
			continue
		}

		slog.Info("ISM: loaded policy from file", "path", path, "name", p.Name)
	}

	return nil
}

// ManagedIndices returns the names of all indices with ISM state.
func (m *Manager) ManagedIndices() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]string, 0, len(m.states))
	for name := range m.states {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// SaveState persists the current ISM state (called by runner after evaluation).
func (m *Manager) SaveState() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saveStateLocked()
}

func (m *Manager) savePoliciesLocked() error {
	dir := filepath.Join(m.baseDir, "indices")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create indices directory: %w", err)
	}

	data, err := json.MarshalIndent(m.policies, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ISM policies: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, policyFile), data, 0o644)
}

func (m *Manager) saveStateLocked() error {
	dir := filepath.Join(m.baseDir, "indices")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create indices directory: %w", err)
	}

	data, err := json.MarshalIndent(m.states, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ISM state: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, stateFile), data, 0o644)
}
