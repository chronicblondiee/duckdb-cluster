package shard

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Manager struct {
	DataDir string
	Shards  []*Shard
	mu      sync.RWMutex
}

func NewManager(dataDir string, numShards int) (*Manager, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	m := &Manager{DataDir: dataDir}
	for i := 0; i < numShards; i++ {
		s, err := NewShard(i, dataDir)
		if err != nil {
			m.CloseAll()
			return nil, err
		}
		m.Shards = append(m.Shards, s)
	}
	return m, nil
}

func (m *Manager) OpenAll() error {
	entries, err := os.ReadDir(m.DataDir)
	if err != nil {
		return fmt.Errorf("read data dir: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".duckdb") {
			paths = append(paths, e.Name())
		}
	}
	sort.Strings(paths)

	for i, name := range paths {
		s := &Shard{
			ID:   i,
			Path: filepath.Join(m.DataDir, name),
		}
		if err := s.Open(); err != nil {
			m.CloseAll()
			return err
		}
		m.Shards = append(m.Shards, s)
	}
	return nil
}

func (m *Manager) GetShard(id int) *Shard {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if id < 0 || id >= len(m.Shards) {
		return nil
	}
	return m.Shards[id]
}

func (m *Manager) ShardCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.Shards)
}

func (m *Manager) AddShard() (*Shard, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := len(m.Shards)
	s, err := NewShard(id, m.DataDir)
	if err != nil {
		return nil, err
	}
	m.Shards = append(m.Shards, s)
	return s, nil
}

func (m *Manager) RemoveShard(id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id < 0 || id >= len(m.Shards) {
		return fmt.Errorf("shard %d not found", id)
	}
	s := m.Shards[id]
	if err := s.Close(); err != nil {
		return fmt.Errorf("close shard %d: %w", id, err)
	}
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove shard file %d: %w", id, err)
	}
	m.Shards = append(m.Shards[:id], m.Shards[id+1:]...)
	// Re-index remaining shards
	for i := id; i < len(m.Shards); i++ {
		m.Shards[i].ID = i
	}
	return nil
}

func (m *Manager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var firstErr error
	for _, s := range m.Shards {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) ExecuteOnAll(ctx context.Context, query string) error {
	m.mu.RLock()
	shards := make([]*Shard, len(m.Shards))
	copy(shards, m.Shards)
	m.mu.RUnlock()

	var wg sync.WaitGroup
	errs := make([]error, len(shards))
	for i, s := range shards {
		wg.Add(1)
		go func(idx int, sh *Shard) {
			defer wg.Done()
			_, errs[idx] = sh.Execute(ctx, query)
		}(i, s)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return fmt.Errorf("shard %d: %w", i, err)
		}
	}
	return nil
}

func (m *Manager) QueryAll(ctx context.Context, query string) ([][]map[string]any, error) {
	resultSets, err := m.QueryAllWithSchema(ctx, query)
	if err != nil {
		return nil, err
	}

	// Convert to old format for backwards compatibility
	results := make([][]map[string]any, len(resultSets))
	for i, rs := range resultSets {
		results[i] = rs.Rows
	}
	return results, nil
}

func (m *Manager) QueryAllWithSchema(ctx context.Context, query string) ([]*QueryResultSet, error) {
	m.mu.RLock()
	shards := make([]*Shard, len(m.Shards))
	copy(shards, m.Shards)
	m.mu.RUnlock()

	var wg sync.WaitGroup
	results := make([]*QueryResultSet, len(shards))
	errs := make([]error, len(shards))
	for i, s := range shards {
		wg.Add(1)
		go func(idx int, sh *Shard) {
			defer wg.Done()
			results[idx], errs[idx] = sh.QueryWithSchema(ctx, query)
		}(i, s)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("shard %d: %w", i, err)
		}
	}
	return results, nil
}
