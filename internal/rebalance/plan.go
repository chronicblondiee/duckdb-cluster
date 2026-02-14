package rebalance

import (
	"context"
	"fmt"

	"github.com/chronicblondiee/duckdb-cluster/internal/router"
)

// Plan computes what needs to move without moving anything.
func (rb *Rebalancer) Plan(ctx context.Context, cfg Config) (*Plan, error) {
	if cfg.PartitionKeyColumn == "" {
		return nil, fmt.Errorf("partition_key_column is required")
	}

	shardCount := rb.manager.ShardCount()
	if shardCount == 0 {
		return &Plan{}, nil
	}

	targetCount := cfg.TargetShardCount
	if targetCount <= 0 {
		targetCount = shardCount
	}

	// Discover tables
	tables, err := rb.discoverTables(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("discover tables: %w", err)
	}

	plan := &Plan{
		ShardCount: targetCount,
		Tables:     tables,
		Summary:    make(map[string]TableSummary),
	}

	// Scan each shard for each table
	for shardID := 0; shardID < shardCount; shardID++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		s := rb.manager.GetShard(shardID)
		if s == nil {
			continue
		}

		for _, table := range tables {
			// Check if partition key column exists in this table
			exists, err := rb.columnExists(ctx, s, table, cfg.PartitionKeyColumn)
			if err != nil {
				return nil, fmt.Errorf("check column %s.%s on shard %d: %w", table, cfg.PartitionKeyColumn, shardID, err)
			}
			if !exists {
				continue
			}

			// Query partition keys from this shard
			query := fmt.Sprintf("SELECT CAST(%s AS VARCHAR) AS pk FROM %s",
				quoteIdent(cfg.PartitionKeyColumn), quoteIdent(table))

			rows, err := s.Query(ctx, query)
			if err != nil {
				return nil, fmt.Errorf("scan %s on shard %d: %w", table, shardID, err)
			}

			summary := plan.Summary[table]
			summary.TotalRows += int64(len(rows))
			plan.TotalRows += int64(len(rows))

			for _, row := range rows {
				pk, ok := row["pk"].(string)
				if !ok {
					continue
				}

				target := router.HashRoute(pk, targetCount)
				if target != shardID {
					plan.Migrations = append(plan.Migrations, Migration{
						Table:        table,
						SourceShard:  shardID,
						TargetShard:  target,
						PartitionKey: pk,
					})
					summary.RowsToMove++
					plan.RowsToMove++
				}
			}

			plan.Summary[table] = summary
		}
	}

	return plan, nil
}

// discoverTables returns the list of tables to rebalance.
func (rb *Rebalancer) discoverTables(ctx context.Context, cfg Config) ([]string, error) {
	if len(cfg.Tables) > 0 {
		return cfg.Tables, nil
	}

	// Query information_schema on shard 0 to find user tables
	s := rb.manager.GetShard(0)
	if s == nil {
		return nil, fmt.Errorf("shard 0 not available")
	}

	rows, err := s.Query(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = 'main'")
	if err != nil {
		return nil, fmt.Errorf("query information_schema: %w", err)
	}

	var tables []string
	for _, row := range rows {
		if name, ok := row["table_name"].(string); ok {
			tables = append(tables, name)
		}
	}
	return tables, nil
}

// columnExists checks if a column exists in a table on a given shard.
func (rb *Rebalancer) columnExists(ctx context.Context, s interface{ Query(context.Context, string, ...any) ([]map[string]any, error) }, table, column string) (bool, error) {
	query := fmt.Sprintf(
		"SELECT 1 FROM information_schema.columns WHERE table_schema = 'main' AND table_name = '%s' AND column_name = '%s'",
		table, column)
	rows, err := s.Query(ctx, query)
	if err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// quoteIdent quotes a SQL identifier with double quotes.
func quoteIdent(name string) string {
	return `"` + name + `"`
}
