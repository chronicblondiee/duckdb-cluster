package rebalance

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

// WriteGate controls whether writes are allowed. During rebalance, the gate
// is paused to prevent conflicting writes.
type WriteGate struct {
	paused atomic.Bool
}

func (g *WriteGate) Pause()         { g.paused.Store(true) }
func (g *WriteGate) Resume()        { g.paused.Store(false) }
func (g *WriteGate) IsPaused() bool { return g.paused.Load() }

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
	manager   *shard.Manager
	logger    *slog.Logger
	writeGate *WriteGate

	mu     sync.RWMutex
	status Status

	subMu       sync.Mutex
	subscribers map[int]chan Status
	nextSubID   int
}

func NewRebalancer(manager *shard.Manager, logger *slog.Logger) *Rebalancer {
	return &Rebalancer{
		manager:     manager,
		logger:      logger,
		writeGate:   &WriteGate{},
		status:      Status{State: "idle"},
		subscribers: make(map[int]chan Status),
	}
}

// WriteGateRef returns the write gate for external consumers to check.
func (rb *Rebalancer) WriteGateRef() *WriteGate {
	return rb.writeGate
}

// Subscribe returns a channel that receives status updates and an unsubscribe function.
func (rb *Rebalancer) Subscribe() (<-chan Status, func()) {
	rb.subMu.Lock()
	defer rb.subMu.Unlock()

	id := rb.nextSubID
	rb.nextSubID++

	ch := make(chan Status, 16)
	rb.subscribers[id] = ch

	unsubscribe := func() {
		rb.subMu.Lock()
		defer rb.subMu.Unlock()
		delete(rb.subscribers, id)
		close(ch)
	}

	return ch, unsubscribe
}

// notify sends current status to all subscribers (non-blocking).
func (rb *Rebalancer) notify() {
	rb.subMu.Lock()
	defer rb.subMu.Unlock()

	status := rb.GetStatus()
	for _, ch := range rb.subscribers {
		select {
		case ch <- status:
		default:
		}
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
	fn(&rb.status)
	rb.mu.Unlock()
	rb.notify()
}
