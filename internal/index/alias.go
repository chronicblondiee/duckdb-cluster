package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Alias maps a name to one or more index names.
type Alias struct {
	Name    string   `json:"name"`
	Indices []string `json:"indices"`
}

// AliasManager manages index aliases with persistence.
type AliasManager struct {
	mu      sync.RWMutex
	aliases map[string]*Alias
	baseDir string
}

// NewAliasManager creates a new alias manager.
func NewAliasManager(baseDir string) *AliasManager {
	return &AliasManager{
		aliases: make(map[string]*Alias),
		baseDir: baseDir,
	}
}

// Put creates or updates an alias.
func (am *AliasManager) Put(name string, indices []string) error {
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("invalid alias name: %w", err)
	}
	if len(indices) == 0 {
		return fmt.Errorf("alias must reference at least one index")
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	am.aliases[name] = &Alias{Name: name, Indices: indices}
	return am.saveLocked()
}

// Get returns an alias by name.
func (am *AliasManager) Get(name string) (*Alias, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	a, ok := am.aliases[name]
	if !ok {
		return nil, fmt.Errorf("alias %q not found", name)
	}
	return a, nil
}

// GetIndices returns the index names for an alias, or an error if not found.
func (am *AliasManager) GetIndices(name string) ([]string, error) {
	am.mu.RLock()
	defer am.mu.RUnlock()

	a, ok := am.aliases[name]
	if !ok {
		return nil, fmt.Errorf("alias %q not found", name)
	}
	return a.Indices, nil
}

// Delete removes an alias.
func (am *AliasManager) Delete(name string) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	if _, ok := am.aliases[name]; !ok {
		return fmt.Errorf("alias %q not found", name)
	}
	delete(am.aliases, name)
	return am.saveLocked()
}

// List returns all aliases sorted by name.
func (am *AliasManager) List() []*Alias {
	am.mu.RLock()
	defer am.mu.RUnlock()

	result := make([]*Alias, 0, len(am.aliases))
	for _, a := range am.aliases {
		result = append(result, a)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Resolve returns the index names for an alias. Returns nil if not an alias.
func (am *AliasManager) Resolve(name string) []string {
	am.mu.RLock()
	defer am.mu.RUnlock()

	if a, ok := am.aliases[name]; ok {
		return a.Indices
	}
	return nil
}

// LoadAll reads aliases from disk.
func (am *AliasManager) LoadAll() error {
	am.mu.Lock()
	defer am.mu.Unlock()

	path := filepath.Join(am.baseDir, "indices", "_aliases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read aliases: %w", err)
	}

	var aliases map[string]*Alias
	if err := json.Unmarshal(data, &aliases); err != nil {
		return fmt.Errorf("parse aliases: %w", err)
	}
	am.aliases = aliases
	return nil
}

func (am *AliasManager) saveLocked() error {
	dir := filepath.Join(am.baseDir, "indices")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create indices directory: %w", err)
	}

	data, err := json.MarshalIndent(am.aliases, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal aliases: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, "_aliases.json"), data, 0o644)
}
