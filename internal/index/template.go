package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Template defines settings and mappings for indices matching a pattern.
type Template struct {
	Name     string   `json:"name"`
	Pattern  string   `json:"pattern"`  // glob pattern (e.g., "logs-*")
	Priority int      `json:"priority"` // higher = matched first
	Settings Settings `json:"settings"`
	Mapping  *Mapping `json:"mapping,omitempty"`
}

// TemplateManager manages index templates with persistence.
type TemplateManager struct {
	mu        sync.RWMutex
	templates map[string]*Template
	baseDir   string
}

// NewTemplateManager creates a new template manager.
func NewTemplateManager(baseDir string) *TemplateManager {
	return &TemplateManager{
		templates: make(map[string]*Template),
		baseDir:   baseDir,
	}
}

// Put creates or updates a template.
func (tm *TemplateManager) Put(t *Template) error {
	if t.Name == "" {
		return fmt.Errorf("template name is required")
	}
	if t.Pattern == "" {
		return fmt.Errorf("template pattern is required")
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.templates[t.Name] = t
	return tm.saveLocked()
}

// Get returns a template by name.
func (tm *TemplateManager) Get(name string) (*Template, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	t, ok := tm.templates[name]
	if !ok {
		return nil, fmt.Errorf("template %q not found", name)
	}
	return t, nil
}

// Delete removes a template.
func (tm *TemplateManager) Delete(name string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if _, ok := tm.templates[name]; !ok {
		return fmt.Errorf("template %q not found", name)
	}
	delete(tm.templates, name)
	return tm.saveLocked()
}

// List returns all templates sorted by name.
func (tm *TemplateManager) List() []*Template {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make([]*Template, 0, len(tm.templates))
	for _, t := range tm.templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// Match returns templates whose patterns match the given index name,
// sorted by priority descending (highest first).
func (tm *TemplateManager) Match(indexName string) []*Template {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var matched []*Template
	for _, t := range tm.templates {
		if MatchGlob(t.Pattern, indexName) {
			matched = append(matched, t)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Priority > matched[j].Priority })
	return matched
}

// LoadAll reads templates from disk.
func (tm *TemplateManager) LoadAll() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	path := filepath.Join(tm.baseDir, "indices", "_templates.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read templates: %w", err)
	}

	var templates map[string]*Template
	if err := json.Unmarshal(data, &templates); err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}
	tm.templates = templates
	return nil
}

func (tm *TemplateManager) saveLocked() error {
	dir := filepath.Join(tm.baseDir, "indices")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create indices directory: %w", err)
	}

	data, err := json.MarshalIndent(tm.templates, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal templates: %w", err)
	}

	return os.WriteFile(filepath.Join(dir, "_templates.json"), data, 0o644)
}

// MatchGlob performs simple wildcard matching with '*'.
// Supports patterns like "logs-*", "*-events", "*search*".
func MatchGlob(pattern, s string) bool {
	// Simple cases
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}

	parts := strings.Split(pattern, "*")

	// Pattern starts with non-wildcard: must match prefix
	if parts[0] != "" && !strings.HasPrefix(s, parts[0]) {
		return false
	}

	// Pattern ends with non-wildcard: must match suffix
	last := parts[len(parts)-1]
	if last != "" && !strings.HasSuffix(s, last) {
		return false
	}

	// Check all parts appear in order
	pos := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(s[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	return true
}
