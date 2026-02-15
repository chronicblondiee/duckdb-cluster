package index

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// catalogFile is the name of the metadata file within the indices directory.
const catalogFile = "_metadata.json"

// catalog is the on-disk format for index metadata.
type catalog struct {
	Indices map[string]Metadata `json:"indices"`
}

// Registry manages all indices in the cluster.
type Registry struct {
	mu              sync.RWMutex
	indices         map[string]*Index
	baseDir         string // root data dir (e.g., ./data)
	templateManager *TemplateManager
}

// NewRegistry creates a new empty registry.
func NewRegistry(baseDir string) *Registry {
	return &Registry{
		indices: make(map[string]*Index),
		baseDir: baseDir,
	}
}

// SetTemplateManager sets the template manager for auto-applying templates on index creation.
func (r *Registry) SetTemplateManager(tm *TemplateManager) {
	r.templateManager = tm
}

// Create creates a new index with the given name and settings.
func (r *Registry) Create(name string, settings Settings) (*Index, error) {
	if err := validateNameInternal(name); err != nil {
		return nil, err
	}

	var templateMapping *Mapping

	// Apply matching template defaults before validation
	if r.templateManager != nil {
		matched := r.templateManager.Match(name)
		if len(matched) > 0 {
			tmpl := matched[0] // highest priority
			if settings.ShardCount == 0 {
				settings.ShardCount = tmpl.Settings.ShardCount
			}
			if settings.PartitionKeyField == "" {
				settings.PartitionKeyField = tmpl.Settings.PartitionKeyField
			}
			if tmpl.Mapping != nil {
				templateMapping = tmpl.Mapping.Clone()
			}
		}
	}

	if err := ValidateShardCount(settings.ShardCount); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.indices[name]; exists {
		return nil, fmt.Errorf("index %q already exists", name)
	}

	meta := Metadata{
		Name:     name,
		Settings: settings,
		State:    StateOpen,
		Mapping:  templateMapping,
	}

	idx, err := NewIndex(r.baseDir, meta)
	if err != nil {
		return nil, err
	}

	r.indices[name] = idx
	if err := r.saveCatalogLocked(); err != nil {
		// Best-effort cleanup
		idx.Delete()
		delete(r.indices, name)
		return nil, fmt.Errorf("save catalog: %w", err)
	}

	slog.Info("index created", "name", name, "shards", settings.ShardCount)
	return idx, nil
}

// Get returns an open index by name.
func (r *Registry) Get(name string) (*Index, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	idx, ok := r.indices[name]
	if !ok {
		return nil, fmt.Errorf("index %q not found", name)
	}
	if idx.Meta.State != StateOpen {
		return nil, fmt.Errorf("index %q is closed", name)
	}
	return idx, nil
}

// GetAny returns an index by name regardless of state.
func (r *Registry) GetAny(name string) (*Index, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	idx, ok := r.indices[name]
	if !ok {
		return nil, fmt.Errorf("index %q not found", name)
	}
	return idx, nil
}

// Delete removes an index, closing its shards and removing data from disk.
func (r *Registry) Delete(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx, ok := r.indices[name]
	if !ok {
		return fmt.Errorf("index %q not found", name)
	}

	if err := idx.Delete(); err != nil {
		return err
	}

	// Remove schema file if it exists
	schemaPath := filepath.Join(r.baseDir, "indices", name, "_schema.pb")
	os.Remove(schemaPath) // ignore error, may not exist

	delete(r.indices, name)
	if err := r.saveCatalogLocked(); err != nil {
		return fmt.Errorf("save catalog after delete: %w", err)
	}

	slog.Info("index deleted", "name", name)
	return nil
}

// List returns metadata for all indices.
func (r *Registry) List() []Metadata {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Metadata, 0, len(r.indices))
	for _, idx := range r.indices {
		result = append(result, idx.Meta)
	}
	return result
}

// CloseIndex closes an index (keeps data on disk).
func (r *Registry) CloseIndex(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx, ok := r.indices[name]
	if !ok {
		return fmt.Errorf("index %q not found", name)
	}
	if idx.Meta.State == StateClosed {
		return nil
	}

	if err := idx.Close(); err != nil {
		return err
	}

	return r.saveCatalogLocked()
}

// OpenIndex reopens a closed index.
func (r *Registry) OpenIndex(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx, ok := r.indices[name]
	if !ok {
		return fmt.Errorf("index %q not found", name)
	}

	if err := idx.Reopen(); err != nil {
		return err
	}

	return r.saveCatalogLocked()
}

// LoadAll reads the catalog from disk and opens all indices marked as open.
// If no catalog exists but flat shard files are found, performs a migration.
func (r *Registry) LoadAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	catalogPath := filepath.Join(r.baseDir, "indices", catalogFile)

	data, err := os.ReadFile(catalogPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Check for flat layout migration
			return r.migrateFlatLayoutLocked()
		}
		return fmt.Errorf("read catalog: %w", err)
	}

	var cat catalog
	if err := json.Unmarshal(data, &cat); err != nil {
		return fmt.Errorf("parse catalog: %w", err)
	}

	for name, meta := range cat.Indices {
		if meta.State == StateOpen {
			idx, err := OpenIndex(r.baseDir, meta)
			if err != nil {
				slog.Error("failed to open index", "name", name, "error", err)
				continue
			}
			r.indices[name] = idx
			slog.Info("index loaded", "name", name, "shards", idx.Manager.ShardCount())
		} else {
			// Keep metadata for closed indices without opening shards
			r.indices[name] = &Index{
				Meta:    meta,
				Mapping: meta.Mapping,
				dataDir: filepath.Join(r.baseDir, "indices", name),
			}
			slog.Info("index registered (closed)", "name", name)
		}
	}

	return nil
}

// CloseAll closes all open indices. Called on shutdown.
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, idx := range r.indices {
		if idx.Meta.State == StateOpen && idx.Manager != nil {
			if err := idx.Manager.CloseAll(); err != nil {
				slog.Error("failed to close index", "name", name, "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// SaveCatalog persists the current index catalog to disk.
func (r *Registry) SaveCatalog() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.saveCatalogLocked()
}

// saveCatalogLocked writes the catalog file. Caller must hold the lock.
func (r *Registry) saveCatalogLocked() error {
	indicesDir := filepath.Join(r.baseDir, "indices")
	if err := os.MkdirAll(indicesDir, 0o755); err != nil {
		return fmt.Errorf("create indices directory: %w", err)
	}

	cat := catalog{Indices: make(map[string]Metadata, len(r.indices))}
	for name, idx := range r.indices {
		meta := idx.Meta
		meta.Mapping = idx.Mapping
		cat.Indices[name] = meta
	}

	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal catalog: %w", err)
	}

	catalogPath := filepath.Join(indicesDir, catalogFile)
	return os.WriteFile(catalogPath, data, 0o644)
}

// migrateFlatLayoutLocked migrates a flat shard layout (shard_*.duckdb in baseDir)
// to the new indices layout by moving files into a _default index.
func (r *Registry) migrateFlatLayoutLocked() error {
	entries, err := os.ReadDir(r.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No data dir yet, nothing to migrate
		}
		return fmt.Errorf("read data dir: %w", err)
	}

	// Count flat shard files
	var shardFiles []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".duckdb" {
			shardFiles = append(shardFiles, e.Name())
		}
	}

	if len(shardFiles) == 0 {
		// No flat shards, no migration needed
		return nil
	}

	slog.Info("migrating flat shard layout to indices", "shard_count", len(shardFiles))

	// Create _default index directory
	defaultDir := filepath.Join(r.baseDir, "indices", "_default")
	if err := os.MkdirAll(defaultDir, 0o755); err != nil {
		return fmt.Errorf("create _default index dir: %w", err)
	}

	// Move shard files
	for _, f := range shardFiles {
		src := filepath.Join(r.baseDir, f)
		dst := filepath.Join(defaultDir, f)
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("move shard file %s: %w", f, err)
		}
	}

	// Open the migrated index
	meta := Metadata{
		Name: "_default",
		Settings: Settings{
			ShardCount:        len(shardFiles),
			PartitionKeyField: "_id",
		},
		State: StateOpen,
	}

	idx, err := OpenIndex(r.baseDir, meta)
	if err != nil {
		return fmt.Errorf("open migrated _default index: %w", err)
	}

	r.indices["_default"] = idx

	// Save the new catalog
	if err := r.saveCatalogLocked(); err != nil {
		return fmt.Errorf("save catalog after migration: %w", err)
	}

	slog.Info("migration complete", "index", "_default", "shards", len(shardFiles))
	return nil
}

// BaseDir returns the registry's base data directory.
func (r *Registry) BaseDir() string {
	return r.baseDir
}
