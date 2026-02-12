package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/brown/duckdb-cluster/internal/shard"
)

type QueryResult struct {
	Columns      []string         `json:"columns,omitempty"`
	Rows         []map[string]any `json:"rows"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	ShardID      int              `json:"shard_id"`
}

type Router struct {
	Manager *shard.Manager
}

func NewRouter(m *shard.Manager) *Router {
	return &Router{Manager: m}
}

func (r *Router) Route(ctx context.Context, sqlStr string, partitionKey string) (*QueryResult, error) {
	keyword := firstKeyword(sqlStr)

	switch keyword {
	case "CREATE", "DROP", "ALTER":
		return r.handleDDL(ctx, sqlStr)
	case "INSERT", "UPDATE", "DELETE":
		return r.handleWrite(ctx, sqlStr, partitionKey)
	case "SELECT":
		return r.handleRead(ctx, sqlStr)
	default:
		return nil, fmt.Errorf("unsupported SQL statement: %s", keyword)
	}
}

func (r *Router) handleDDL(ctx context.Context, sqlStr string) (*QueryResult, error) {
	if err := r.Manager.ExecuteOnAll(ctx, sqlStr); err != nil {
		return nil, err
	}
	return &QueryResult{ShardID: -1}, nil
}

func (r *Router) handleWrite(ctx context.Context, sqlStr string, partitionKey string) (*QueryResult, error) {
	if partitionKey == "" {
		return nil, fmt.Errorf("partition_key is required for write operations")
	}
	shardID := HashRoute(partitionKey, r.Manager.ShardCount())
	s := r.Manager.GetShard(shardID)
	if s == nil {
		return nil, fmt.Errorf("shard %d not found", shardID)
	}
	result, err := s.Execute(ctx, sqlStr)
	if err != nil {
		return nil, fmt.Errorf("shard %d: %w", shardID, err)
	}
	affected, _ := result.RowsAffected()
	return &QueryResult{
		RowsAffected: affected,
		ShardID:      shardID,
	}, nil
}

func (r *Router) handleRead(ctx context.Context, sqlStr string) (*QueryResult, error) {
	results, err := r.Manager.QueryAll(ctx, sqlStr)
	if err != nil {
		return nil, err
	}
	merged := MergeResults(results)

	var cols []string
	if len(merged) > 0 {
		for k := range merged[0] {
			cols = append(cols, k)
		}
	}

	return &QueryResult{
		Columns: cols,
		Rows:    merged,
		ShardID: -1,
	}, nil
}

func firstKeyword(sql string) string {
	trimmed := strings.TrimSpace(sql)
	end := strings.IndexAny(trimmed, " \t\n\r")
	if end == -1 {
		return strings.ToUpper(trimmed)
	}
	return strings.ToUpper(trimmed[:end])
}
