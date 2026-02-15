package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/router"
	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

// State represents the lifecycle state of an index.
type State string

const (
	StateOpen   State = "open"
	StateClosed State = "closed"
)

// Settings holds per-index configuration.
type Settings struct {
	ShardCount        int    `json:"shard_count"`
	PartitionKeyField string `json:"partition_key_field,omitempty"` // defaults to "_id"
}

// Metadata holds persisted information about an index.
type Metadata struct {
	Name      string    `json:"name"`
	Settings  Settings  `json:"settings"`
	State     State     `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	Mapping   *Mapping  `json:"mapping,omitempty"`
	ReadOnly  bool      `json:"read_only,omitempty"`
}

// Index is a named logical namespace owning N shards.
type Index struct {
	Meta    Metadata
	Mapping *Mapping
	Manager *shard.Manager
	Router  *router.Router
	mu      sync.RWMutex
	dataDir string
}

// NewIndex creates a new index with fresh shards on disk.
func NewIndex(baseDataDir string, meta Metadata) (*Index, error) {
	if err := validateNameInternal(meta.Name); err != nil {
		return nil, err
	}
	if err := ValidateShardCount(meta.Settings.ShardCount); err != nil {
		return nil, err
	}

	if meta.Settings.PartitionKeyField == "" {
		meta.Settings.PartitionKeyField = "_id"
	}
	if meta.State == "" {
		meta.State = StateOpen
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}

	dataDir := filepath.Join(baseDataDir, "indices", meta.Name)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create index directory: %w", err)
	}

	m, err := shard.NewManager(dataDir, meta.Settings.ShardCount)
	if err != nil {
		return nil, fmt.Errorf("create shard manager: %w", err)
	}

	mapping := meta.Mapping
	if mapping == nil {
		mapping = &Mapping{Fields: make(map[string]*FieldMapping), Dynamic: true}
	}

	idx := &Index{
		Meta:    meta,
		Mapping: mapping,
		Manager: m,
		Router:  router.NewRouter(m),
		dataDir: dataDir,
	}
	return idx, nil
}

// OpenIndex opens an existing index from disk.
func OpenIndex(baseDataDir string, meta Metadata) (*Index, error) {
	dataDir := filepath.Join(baseDataDir, "indices", meta.Name)

	m := &shard.Manager{DataDir: dataDir}
	if err := m.OpenAll(); err != nil {
		return nil, fmt.Errorf("open shards for index %q: %w", meta.Name, err)
	}

	mapping := meta.Mapping
	if mapping == nil {
		mapping = &Mapping{Fields: make(map[string]*FieldMapping), Dynamic: true}
	}

	idx := &Index{
		Meta:    meta,
		Mapping: mapping,
		Manager: m,
		Router:  router.NewRouter(m),
		dataDir: dataDir,
	}
	return idx, nil
}

// Route delegates to the index's router.
func (idx *Index) Route(ctx context.Context, sql string, partitionKey string) (*router.QueryResult, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if idx.Meta.State != StateOpen {
		return nil, fmt.Errorf("index %q is closed", idx.Meta.Name)
	}
	return idx.Router.Route(ctx, sql, partitionKey)
}

// Close closes all shards. The index can be reopened later.
func (idx *Index) Close() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.Manager != nil {
		if err := idx.Manager.CloseAll(); err != nil {
			return fmt.Errorf("close index %q: %w", idx.Meta.Name, err)
		}
	}
	idx.Meta.State = StateClosed
	return nil
}

// Reopen reopens a closed index.
func (idx *Index) Reopen() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.Meta.State == StateOpen {
		return nil
	}
	m := &shard.Manager{DataDir: idx.dataDir}
	if err := m.OpenAll(); err != nil {
		return fmt.Errorf("reopen index %q: %w", idx.Meta.Name, err)
	}
	idx.Manager = m
	idx.Router = router.NewRouter(m)
	idx.Meta.State = StateOpen
	return nil
}

// Delete closes shards and removes the index directory from disk.
func (idx *Index) Delete() error {
	if err := idx.Close(); err != nil {
		return err
	}
	if err := os.RemoveAll(idx.dataDir); err != nil {
		return fmt.Errorf("delete index directory %q: %w", idx.dataDir, err)
	}
	return nil
}

// DataDir returns the index's data directory path.
func (idx *Index) DataDir() string {
	return idx.dataDir
}
