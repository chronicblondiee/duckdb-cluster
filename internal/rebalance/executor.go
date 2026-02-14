package rebalance

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const defaultBatchSize = 1000

// Execute computes the plan and executes all migrations.
func (rb *Rebalancer) Execute(ctx context.Context, cfg Config) (*Status, error) {
	// Check not already running
	current := rb.GetStatus()
	if current.State == "running" || current.State == "planning" {
		return nil, fmt.Errorf("rebalance already in progress (state: %s)", current.State)
	}

	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaultBatchSize
	}

	rb.setStatus(func(s *Status) {
		*s = Status{
			State:     "planning",
			StartTime: time.Now(),
		}
	})

	// Compute plan
	plan, err := rb.Plan(ctx, cfg)
	if err != nil {
		rb.setStatus(func(s *Status) {
			s.State = "failed"
			s.EndTime = time.Now()
			s.Errors = []string{err.Error()}
		})
		return nil, fmt.Errorf("plan: %w", err)
	}

	if plan.RowsToMove == 0 {
		rb.setStatus(func(s *Status) {
			s.State = "completed"
			s.EndTime = time.Now()
			s.TablesTotal = len(plan.Tables)
			s.TablesDone = len(plan.Tables)
			s.RowsScanned = plan.TotalRows
		})
		status := rb.GetStatus()
		return &status, nil
	}

	rb.setStatus(func(s *Status) {
		s.State = "running"
		s.TablesTotal = len(plan.Tables)
		s.RowsScanned = plan.TotalRows
	})

	// Group migrations by (table, source, target) for batching
	groups := groupMigrations(plan.Migrations)

	// Get column info for each table (from shard 0)
	tableColumns := make(map[string][]string)
	for _, table := range plan.Tables {
		s := rb.manager.GetShard(0)
		if s == nil {
			continue
		}
		rs, err := s.QueryWithSchema(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 0", quoteIdent(table)))
		if err != nil {
			rb.setStatus(func(st *Status) {
				st.State = "failed"
				st.EndTime = time.Now()
				st.Errors = append(st.Errors, fmt.Sprintf("get columns for %s: %v", table, err))
			})
			status := rb.GetStatus()
			return &status, fmt.Errorf("get columns for %s: %w", table, err)
		}
		tableColumns[table] = rs.Columns
	}

	// Execute each migration group in batches
	completedTables := make(map[string]bool)
	var execErrors []string

	for key, partitionKeys := range groups {
		if err := ctx.Err(); err != nil {
			rb.setStatus(func(s *Status) {
				s.State = "failed"
				s.EndTime = time.Now()
				s.Errors = append(s.Errors, "cancelled")
			})
			status := rb.GetStatus()
			return &status, err
		}

		columns := tableColumns[key.table]
		if columns == nil {
			continue
		}

		// Process in batches
		for i := 0; i < len(partitionKeys); i += cfg.BatchSize {
			if err := ctx.Err(); err != nil {
				rb.setStatus(func(s *Status) {
					s.State = "failed"
					s.EndTime = time.Now()
					s.Errors = append(s.Errors, "cancelled")
				})
				status := rb.GetStatus()
				return &status, err
			}

			end := i + cfg.BatchSize
			if end > len(partitionKeys) {
				end = len(partitionKeys)
			}
			batch := partitionKeys[i:end]

			moved, err := rb.migrateBatch(ctx, key.table, key.sourceShard, key.targetShard, batch, cfg.PartitionKeyColumn, columns)
			if err != nil {
				errMsg := fmt.Sprintf("migrate %s shard %d->%d: %v", key.table, key.sourceShard, key.targetShard, err)
				execErrors = append(execErrors, errMsg)
				rb.logger.Error("migration batch failed", "table", key.table, "source", key.sourceShard, "target", key.targetShard, "error", err)
				continue
			}

			rb.setStatus(func(s *Status) {
				s.RowsMoved += moved
			})
		}

		completedTables[key.table] = true
		rb.setStatus(func(s *Status) {
			s.TablesDone = len(completedTables)
		})
	}

	finalState := "completed"
	if len(execErrors) > 0 {
		finalState = "failed"
	}

	rb.setStatus(func(s *Status) {
		s.State = finalState
		s.EndTime = time.Now()
		s.Errors = execErrors
	})

	status := rb.GetStatus()
	if len(execErrors) > 0 {
		return &status, fmt.Errorf("rebalance completed with %d errors", len(execErrors))
	}
	return &status, nil
}

// migrationKey groups migrations by table and shard pair.
type migrationKey struct {
	table       string
	sourceShard int
	targetShard int
}

// groupMigrations groups partition keys by (table, source, target).
func groupMigrations(migrations []Migration) map[migrationKey][]string {
	groups := make(map[migrationKey][]string)
	for _, m := range migrations {
		key := migrationKey{
			table:       m.Table,
			sourceShard: m.SourceShard,
			targetShard: m.TargetShard,
		}
		groups[key] = append(groups[key], m.PartitionKey)
	}
	return groups
}

// migrateBatch moves a batch of rows from source to target shard.
// Order: INSERT into target first, then DELETE from source (prefer duplicates over data loss).
func (rb *Rebalancer) migrateBatch(ctx context.Context, table string, srcID, dstID int, keys []string, pkCol string, columns []string) (int64, error) {
	src := rb.manager.GetShard(srcID)
	dst := rb.manager.GetShard(dstID)
	if src == nil {
		return 0, fmt.Errorf("source shard %d not found", srcID)
	}
	if dst == nil {
		return 0, fmt.Errorf("target shard %d not found", dstID)
	}

	// Build WHERE clause for this batch
	whereClause := buildWhereIN(pkCol, keys)

	// SELECT full rows from source
	selectSQL := fmt.Sprintf("SELECT * FROM %s WHERE %s", quoteIdent(table), whereClause)
	rows, err := src.Query(ctx, selectSQL)
	if err != nil {
		return 0, fmt.Errorf("select from source: %w", err)
	}

	if len(rows) == 0 {
		return 0, nil
	}

	// INSERT into target
	insertSQL := buildInsertSQL(table, columns)
	var moved int64
	for _, row := range rows {
		vals := make([]any, len(columns))
		for i, col := range columns {
			vals[i] = row[col]
		}
		if _, err := dst.Execute(ctx, insertSQL, vals...); err != nil {
			return moved, fmt.Errorf("insert into target: %w", err)
		}
		moved++
	}

	// DELETE from source
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE %s", quoteIdent(table), whereClause)
	if _, err := src.Execute(ctx, deleteSQL); err != nil {
		return moved, fmt.Errorf("delete from source: %w", err)
	}

	return moved, nil
}

// buildInsertSQL builds a parameterized INSERT statement.
func buildInsertSQL(table string, columns []string) string {
	quoted := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		quoted[i] = quoteIdent(col)
		placeholders[i] = "?"
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteIdent(table),
		strings.Join(quoted, ", "),
		strings.Join(placeholders, ", "))
}

// buildWhereIN builds a WHERE clause for matching partition keys.
func buildWhereIN(pkCol string, keys []string) string {
	escaped := make([]string, len(keys))
	for i, k := range keys {
		escaped[i] = "'" + strings.ReplaceAll(k, "'", "''") + "'"
	}
	return fmt.Sprintf("CAST(%s AS VARCHAR) IN (%s)", quoteIdent(pkCol), strings.Join(escaped, ", "))
}
