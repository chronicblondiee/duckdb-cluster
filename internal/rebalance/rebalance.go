package rebalance

import (
	"log/slog"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

// Config controls what and how to rebalance.
type Config struct {
	PartitionKeyColumn string   `json:"partition_key_column"`
	Tables             []string `json:"tables,omitempty"`
	BatchSize          int      `json:"batch_size,omitempty"`
	TargetShardCount   int      `json:"target_shard_count,omitempty"`
}

// Status tracks rebalance progress.
type Status struct {
	State       string    `json:"state"`
	StartTime   time.Time `json:"start_time,omitempty"`
	EndTime     time.Time `json:"end_time,omitempty"`
	TablesTotal int       `json:"tables_total"`
	TablesDone  int       `json:"tables_done"`
	RowsMoved   int64     `json:"rows_moved"`
	RowsScanned int64     `json:"rows_scanned"`
	Errors      []string  `json:"errors,omitempty"`
}

// Migration represents a single row that needs to move between shards.
type Migration struct {
	Table        string `json:"table"`
	SourceShard  int    `json:"source_shard"`
	TargetShard  int    `json:"target_shard"`
	PartitionKey string `json:"partition_key"`
}

// TableSummary summarizes migration for one table.
type TableSummary struct {
	TotalRows  int64 `json:"total_rows"`
	RowsToMove int64 `json:"rows_to_move"`
}

// Plan is the computed set of migrations.
type Plan struct {
	ShardCount int                    `json:"shard_count"`
	Tables     []string               `json:"tables"`
	Migrations []Migration            `json:"migrations"`
	Summary    map[string]TableSummary `json:"summary"`
	TotalRows  int64                  `json:"total_rows"`
	RowsToMove int64                  `json:"rows_to_move"`
}

// Rebalancer orchestrates data migration between shards.
type Rebalancer struct {
	manager *shard.Manager
	logger  *slog.Logger

	mu     sync.RWMutex
	status Status
}

func NewRebalancer(manager *shard.Manager, logger *slog.Logger) *Rebalancer {
	return &Rebalancer{
		manager: manager,
		logger:  logger,
		status:  Status{State: "idle"},
	}
}

// GetStatus returns a snapshot of current rebalance progress.
func (rb *Rebalancer) GetStatus() Status {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	s := rb.status
	// Copy errors slice to avoid data race on the backing array
	if rb.status.Errors != nil {
		s.Errors = make([]string, len(rb.status.Errors))
		copy(s.Errors, rb.status.Errors)
	}
	return s
}

func (rb *Rebalancer) setStatus(fn func(*Status)) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	fn(&rb.status)
}
