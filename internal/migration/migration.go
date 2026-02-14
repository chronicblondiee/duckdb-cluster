package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/backup"
	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

// MigrationFunc applies a schema change to a single shard.
type MigrationFunc func(ctx context.Context, s *shard.Shard) error

// MigrationDef defines a single registered migration.
type MigrationDef struct {
	// ID is a unique, sortable identifier like "001_create_metadata_table".
	ID string
	// Description is a human-readable summary.
	Description string
	// Up applies the migration to a single shard.
	Up MigrationFunc
}

// AppliedMigration records a migration that has been applied.
type AppliedMigration struct {
	ID        string    `json:"id"`
	AppliedAt time.Time `json:"applied_at"`
}

// MigrationState is persisted as JSON in the data directory.
type MigrationState struct {
	CurrentVersion    string             `json:"current_version"`
	AppliedMigrations []AppliedMigration `json:"applied_migrations"`
	LastMigrationAt   time.Time          `json:"last_migration_at,omitempty"`
}

// MigrationResult is returned from RunPending.
type MigrationResult struct {
	Applied     []string `json:"applied"`
	BackupID    string   `json:"backup_id,omitempty"`
	PrevVersion string   `json:"previous_version"`
	NewVersion  string   `json:"new_version"`
	Error       string   `json:"error,omitempty"`
}

// Manager handles migration registration, state, and execution.
type Manager struct {
	dataDir       string
	shardManager  *shard.Manager
	backupManager *backup.BackupManager
	migrations    []MigrationDef
	mu            sync.Mutex
}

// NewManager creates a migration manager.
// backupManager may be nil (backups will be skipped).
func NewManager(dataDir string, sm *shard.Manager, bm *backup.BackupManager) *Manager {
	return &Manager{
		dataDir:       dataDir,
		shardManager:  sm,
		backupManager: bm,
	}
}

// Register adds a migration definition to the registry.
func (m *Manager) Register(def MigrationDef) {
	m.migrations = append(m.migrations, def)
}

func (m *Manager) stateFilePath() string {
	return filepath.Join(m.dataDir, "migration_state.json")
}

// LoadState reads migration state from disk. Returns empty state if file is missing.
func (m *Manager) LoadState() (*MigrationState, error) {
	data, err := os.ReadFile(m.stateFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &MigrationState{}, nil
		}
		return nil, fmt.Errorf("read migration state: %w", err)
	}
	var state MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse migration state: %w", err)
	}
	return &state, nil
}

// SaveState writes migration state to disk atomically (write tmp + rename).
func (m *Manager) SaveState(state *MigrationState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal migration state: %w", err)
	}
	tmpPath := m.stateFilePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write migration state: %w", err)
	}
	if err := os.Rename(tmpPath, m.stateFilePath()); err != nil {
		return fmt.Errorf("rename migration state: %w", err)
	}
	return nil
}

// PendingMigrations returns migrations that have not yet been applied, sorted by ID.
func (m *Manager) PendingMigrations() ([]MigrationDef, error) {
	state, err := m.LoadState()
	if err != nil {
		return nil, err
	}
	applied := make(map[string]bool, len(state.AppliedMigrations))
	for _, am := range state.AppliedMigrations {
		applied[am.ID] = true
	}

	sorted := make([]MigrationDef, len(m.migrations))
	copy(sorted, m.migrations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	var pending []MigrationDef
	for _, def := range sorted {
		if !applied[def.ID] {
			pending = append(pending, def)
		}
	}
	return pending, nil
}

// RunPending runs all pending migrations with auto-backup and per-shard application.
// On failure, it returns the error and backup ID so the operator can restore manually.
func (m *Manager) RunPending(ctx context.Context) (*MigrationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pending, err := m.PendingMigrations()
	if err != nil {
		return nil, fmt.Errorf("check pending migrations: %w", err)
	}

	result := &MigrationResult{
		PrevVersion: Version,
		NewVersion:  Version,
	}

	if len(pending) == 0 {
		return result, nil
	}

	// Auto-backup before migration
	if m.backupManager != nil {
		slog.Info("creating pre-migration backup", "pending_count", len(pending))
		backupMeta, err := m.backupManager.CreateBackup(ctx, backup.BackupTypeFull)
		if err != nil {
			return nil, fmt.Errorf("pre-migration backup failed: %w", err)
		}
		if backupMeta.Status != "success" {
			return nil, fmt.Errorf("pre-migration backup not successful: %s", backupMeta.Error)
		}
		result.BackupID = backupMeta.ID
		slog.Info("pre-migration backup created", "backup_id", backupMeta.ID)
	}

	// Load current state
	state, err := m.LoadState()
	if err != nil {
		return nil, err
	}

	// Apply each migration to all shards
	for _, def := range pending {
		slog.Info("applying migration", "id", def.ID, "description", def.Description)

		shardCount := m.shardManager.ShardCount()
		for i := 0; i < shardCount; i++ {
			s := m.shardManager.GetShard(i)
			if s == nil {
				result.Error = fmt.Sprintf("shard %d not found", i)
				return result, fmt.Errorf("shard %d not found during migration %s", i, def.ID)
			}
			if err := def.Up(ctx, s); err != nil {
				result.Error = fmt.Sprintf("migration %s failed on shard %d: %v", def.ID, i, err)
				slog.Error("migration failed", "id", def.ID, "shard", i, "error", err,
					"restore_backup", result.BackupID)
				return result, fmt.Errorf("migration %s failed on shard %d: %w", def.ID, i, err)
			}
		}

		// Record applied migration and persist state after each
		state.AppliedMigrations = append(state.AppliedMigrations, AppliedMigration{
			ID:        def.ID,
			AppliedAt: time.Now(),
		})
		state.CurrentVersion = Version
		state.LastMigrationAt = time.Now()

		if err := m.SaveState(state); err != nil {
			return result, fmt.Errorf("save state after migration %s: %w", def.ID, err)
		}

		result.Applied = append(result.Applied, def.ID)
		slog.Info("migration applied", "id", def.ID)
	}

	return result, nil
}

// GetStatus returns current migration status for the API.
func (m *Manager) GetStatus() (map[string]any, error) {
	state, err := m.LoadState()
	if err != nil {
		return nil, err
	}
	pending, err := m.PendingMigrations()
	if err != nil {
		return nil, err
	}
	pendingIDs := make([]string, len(pending))
	for i, p := range pending {
		pendingIDs[i] = p.ID
	}
	return map[string]any{
		"binary_version":     Version,
		"state_version":      state.CurrentVersion,
		"applied_count":      len(state.AppliedMigrations),
		"pending_count":      len(pending),
		"pending_migrations": pendingIDs,
		"last_migration_at":  state.LastMigrationAt,
	}, nil
}
