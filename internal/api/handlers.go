package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/rebalance"
)

type queryRequest struct {
	SQL          string `json:"sql"`
	PartitionKey string `json:"partition_key"`
	Index        string `json:"index,omitempty"` // target index (defaults to _default)
	Offset       int    `json:"offset,omitempty"`
	Limit        int    `json:"limit,omitempty"`
}

type queryResponse struct {
	Success      bool             `json:"success"`
	Columns      []string         `json:"columns,omitempty"`
	Rows         []map[string]any `json:"rows"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	ShardID      int              `json:"shard_id"`
	Error        string           `json:"error,omitempty"`
}

// Bulk operations types
type bulkStatement struct {
	SQL          string `json:"sql"`
	PartitionKey string `json:"partition_key"`
	Index        string `json:"index,omitempty"` // target index (defaults to _default)
}

type bulkRequest struct {
	Statements []bulkStatement `json:"statements"`
}

type bulkItemResult struct {
	Shard        int    `json:"shard"`
	RowsAffected int64  `json:"rows_affected"`
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
}

type bulkResponse struct {
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	TookMs    int64            `json:"took_ms"`
	Results   []bulkItemResult `json:"results"`
}

// Multi-query types
type multiQueryRequest struct {
	Queries []queryRequest `json:"queries"`
}

type multiQueryResult struct {
	Success      bool             `json:"success"`
	Columns      []string         `json:"columns,omitempty"`
	Rows         []map[string]any `json:"rows,omitempty"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	Error        string           `json:"error,omitempty"`
}

type multiQueryResponse struct {
	Results []multiQueryResult `json:"results"`
	TookMs  int64              `json:"took_ms"`
}

type shardInfo struct {
	ID     int    `json:"id"`
	Path   string `json:"path"`
	Status string `json:"status"`
}

func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, queryResponse{Error: "invalid JSON: " + err.Error()})
		return
	}
	if req.SQL == "" {
		writeJSON(w, http.StatusBadRequest, queryResponse{Error: "sql field is required"})
		return
	}

	if s.rebalancer.WriteGateRef().IsPaused() && isWriteSQL(req.SQL) {
		writeJSON(w, http.StatusServiceUnavailable, queryResponse{Error: "writes paused: rebalance in progress"})
		return
	}

	// Resolve target index
	idx, err := s.resolveIndex(req.Index)
	if err != nil {
		writeJSON(w, http.StatusNotFound, queryResponse{Error: err.Error()})
		return
	}

	result, err := idx.Router.Route(r.Context(), req.SQL, req.PartitionKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, queryResponse{Error: err.Error()})
		return
	}

	// Apply pagination if specified
	rows := result.Rows
	if req.Limit > 0 || req.Offset > 0 {
		rows = applyPagination(rows, req.Offset, req.Limit)
	}

	writeJSON(w, http.StatusOK, queryResponse{
		Success:      true,
		Columns:      result.Columns,
		Rows:         rows,
		RowsAffected: result.RowsAffected,
		ShardID:      result.ShardID,
	})
}

func (s *Server) handleListShards(w http.ResponseWriter, r *http.Request) {
	count := s.Cluster.Manager.ShardCount()
	shards := make([]shardInfo, count)
	for i := 0; i < count; i++ {
		sh := s.Cluster.Manager.GetShard(i)
		shards[i] = shardInfo{
			ID:     sh.ID,
			Path:   sh.Path,
			Status: "open",
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"shards": shards})
}

type addShardRequest struct {
	Rebalance *rebalance.Config `json:"rebalance,omitempty"`
}

func (s *Server) handleAddShard(w http.ResponseWriter, r *http.Request) {
	var req addShardRequest
	json.NewDecoder(r.Body).Decode(&req) // body may be empty

	sh, err := s.Cluster.Manager.AddShard()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	resp := map[string]any{
		"id":     sh.ID,
		"path":   sh.Path,
		"status": "open",
	}

	if req.Rebalance != nil {
		cfg := *req.Rebalance
		go func() {
			s.rebalancer.Execute(context.Background(), cfg)
		}()
		resp["rebalance"] = "started"
	}

	writeJSON(w, http.StatusCreated, resp)
}

type removeShardRequest struct {
	Rebalance *rebalance.Config `json:"rebalance,omitempty"`
}

func (s *Server) handleRemoveShard(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid shard id"})
		return
	}

	var req removeShardRequest
	json.NewDecoder(r.Body).Decode(&req) // body may be empty

	if req.Rebalance != nil {
		cfg := *req.Rebalance
		cfg.TargetShardCount = s.Cluster.Manager.ShardCount() - 1

		if _, err := s.rebalancer.Execute(r.Context(), cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "rebalance before remove failed: " + err.Error(),
			})
			return
		}
		// After rebalancing to N-1, remove the last shard (now empty)
		id = s.Cluster.Manager.ShardCount() - 1
	}

	if err := s.Cluster.Manager.RemoveShard(id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status, err := s.Cluster.Status()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	var req bulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, bulkResponse{
			Failed: 1,
			Results: []bulkItemResult{{
				Success: false,
				Error:   "invalid JSON: " + err.Error(),
			}},
		})
		return
	}
	
	if len(req.Statements) == 0 {
		writeJSON(w, http.StatusBadRequest, bulkResponse{
			Failed: 1,
			Results: []bulkItemResult{{
				Success: false,
				Error:   "statements field is required",
			}},
		})
		return
	}

	if s.rebalancer.WriteGateRef().IsPaused() {
		writeJSON(w, http.StatusServiceUnavailable, bulkResponse{
			Failed: len(req.Statements),
			Results: []bulkItemResult{{
				Success: false,
				Error:   "writes paused: rebalance in progress",
			}},
		})
		return
	}

	// Group statements by shard
	type shardGroup struct {
		shardID    int
		statements []bulkStatement
		indices    []int // track original indices for response
	}
	
	groups := make(map[int]*shardGroup)
	
	for i, stmt := range req.Statements {
		if stmt.SQL == "" {
			continue
		}
		if stmt.PartitionKey == "" {
			// For statements without partition key, we'll handle them individually
			continue
		}
		
		shardID := s.Cluster.Router.HashRoute(stmt.PartitionKey)
		if _, exists := groups[shardID]; !exists {
			groups[shardID] = &shardGroup{
				shardID:    shardID,
				statements: []bulkStatement{},
				indices:    []int{},
			}
		}
		groups[shardID].statements = append(groups[shardID].statements, stmt)
		groups[shardID].indices = append(groups[shardID].indices, i)
	}
	
	// Execute groups in parallel
	results := make([]bulkItemResult, len(req.Statements))
	var wg sync.WaitGroup
	var mu sync.Mutex
	
	for _, group := range groups {
		wg.Add(1)
		go func(g *shardGroup) {
			defer wg.Done()
			
			shard := s.Cluster.Manager.GetShard(g.shardID)
			if shard == nil {
				mu.Lock()
				for _, idx := range g.indices {
					results[idx] = bulkItemResult{
						Shard:   g.shardID,
						Success: false,
						Error:   fmt.Sprintf("shard %d not found", g.shardID),
					}
				}
				mu.Unlock()
				return
			}
			
			// Execute each statement in the group
			for i, stmt := range g.statements {
				idx := g.indices[i]
				result, err := shard.Execute(r.Context(), stmt.SQL)
				
				mu.Lock()
				if err != nil {
					results[idx] = bulkItemResult{
						Shard:   g.shardID,
						Success: false,
						Error:   err.Error(),
					}
				} else {
					affected, _ := result.RowsAffected()
					results[idx] = bulkItemResult{
						Shard:        g.shardID,
						RowsAffected: affected,
						Success:      true,
					}
				}
				mu.Unlock()
			}
		}(group)
	}
	
	wg.Wait()
	
	// Count successes and failures
	succeeded := 0
	failed := 0
	for _, res := range results {
		if res.Success {
			succeeded++
		} else {
			failed++
		}
	}
	
	elapsed := time.Since(startTime).Milliseconds()
	
	writeJSON(w, http.StatusOK, bulkResponse{
		Succeeded: succeeded,
		Failed:    failed,
		TookMs:    elapsed,
		Results:   results,
	})
}

func (s *Server) handleMultiQuery(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()
	
	var req multiQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, multiQueryResponse{
			Results: []multiQueryResult{{
				Success: false,
				Error:   "invalid JSON: " + err.Error(),
			}},
		})
		return
	}
	
	if len(req.Queries) == 0 {
		writeJSON(w, http.StatusBadRequest, multiQueryResponse{
			Results: []multiQueryResult{{
				Success: false,
				Error:   "queries field is required",
			}},
		})
		return
	}

	if s.rebalancer.WriteGateRef().IsPaused() {
		for _, q := range req.Queries {
			if isWriteSQL(q.SQL) {
				writeJSON(w, http.StatusServiceUnavailable, multiQueryResponse{
					Results: []multiQueryResult{{
						Success: false,
						Error:   "writes paused: rebalance in progress",
					}},
				})
				return
			}
		}
	}

	// Execute all queries in parallel
	results := make([]multiQueryResult, len(req.Queries))
	var wg sync.WaitGroup
	
	for i, query := range req.Queries {
		wg.Add(1)
		go func(idx int, q queryRequest) {
			defer wg.Done()
			
			result, err := s.Cluster.Router.Route(r.Context(), q.SQL, q.PartitionKey)
			if err != nil {
				results[idx] = multiQueryResult{
					Success: false,
					Error:   err.Error(),
				}
				return
			}
			
			// Apply pagination if specified
			rows := result.Rows
			if q.Limit > 0 || q.Offset > 0 {
				rows = applyPagination(rows, q.Offset, q.Limit)
			}
			
			results[idx] = multiQueryResult{
				Success:      true,
				Columns:      result.Columns,
				Rows:         rows,
				RowsAffected: result.RowsAffected,
			}
		}(i, query)
	}
	
	wg.Wait()
	
	elapsed := time.Since(startTime).Milliseconds()
	
	writeJSON(w, http.StatusOK, multiQueryResponse{
		Results: results,
		TookMs:  elapsed,
	})
}

func (s *Server) handleListTables(w http.ResponseWriter, r *http.Request) {
	// Query information_schema from shard 0 (all shards have same schema)
	shard := s.Cluster.Manager.GetShard(0)
	if shard == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no shards available"})
		return
	}
	
	rows, err := shard.Query(r.Context(), "SELECT table_name FROM information_schema.tables WHERE table_schema = 'main' ORDER BY table_name")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	
	tables := make([]string, 0, len(rows))
	for _, row := range rows {
		if tableName, ok := row["table_name"].(string); ok {
			tables = append(tables, tableName)
		}
	}
	
	writeJSON(w, http.StatusOK, map[string]any{"tables": tables})
}

func (s *Server) handleTableSchema(w http.ResponseWriter, r *http.Request) {
	tableName := r.PathValue("name")
	if tableName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "table name is required"})
		return
	}
	
	// Query information_schema from shard 0
	shard := s.Cluster.Manager.GetShard(0)
	if shard == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no shards available"})
		return
	}
	
	sql := fmt.Sprintf(`SELECT column_name, data_type, is_nullable 
		FROM information_schema.columns 
		WHERE table_schema = 'main' AND table_name = '%s'
		ORDER BY ordinal_position`, tableName)
	
	rows, err := shard.Query(r.Context(), sql)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	
	if len(rows) == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found"})
		return
	}
	
	type column struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Nullable bool   `json:"nullable"`
	}
	
	columns := make([]column, 0, len(rows))
	for _, row := range rows {
		col := column{}
		if name, ok := row["column_name"].(string); ok {
			col.Name = name
		}
		if typ, ok := row["data_type"].(string); ok {
			col.Type = typ
		}
		if nullable, ok := row["is_nullable"].(string); ok {
			col.Nullable = (nullable == "YES")
		}
		columns = append(columns, col)
	}
	
	writeJSON(w, http.StatusOK, map[string]any{
		"name":    tableName,
		"columns": columns,
	})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	// Get list of tables
	shard0 := s.Cluster.Manager.GetShard(0)
	if shard0 == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no shards available"})
		return
	}
	
	tableRows, err := shard0.Query(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = 'main'")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	
	tables := make([]string, 0, len(tableRows))
	for _, row := range tableRows {
		if tableName, ok := row["table_name"].(string); ok {
			tables = append(tables, tableName)
		}
	}
	
	// Collect per-shard stats
	type shardStat struct {
		ID        int    `json:"id"`
		Path      string `json:"path"`
		SizeMB    int64  `json:"size_mb"`
		TableCount int   `json:"table_count"`
		RowCount  int64  `json:"row_count"`
	}
	
	shardCount := s.Cluster.Manager.ShardCount()
	shardStats := make([]shardStat, shardCount)
	totalRows := int64(0)
	
	for i := 0; i < shardCount; i++ {
		shard := s.Cluster.Manager.GetShard(i)
		if shard == nil {
			continue
		}
		
		// Get file size
		var sizeMB int64
		if info, err := os.Stat(shard.Path); err == nil {
			sizeMB = info.Size() / (1024 * 1024)
		}
		
		// Count rows across all tables for this shard
		rowCount := int64(0)
		for _, table := range tables {
			countSQL := fmt.Sprintf("SELECT COUNT(*) as cnt FROM %s", table)
			countRows, err := shard.Query(ctx, countSQL)
			if err == nil && len(countRows) > 0 {
				if cnt, ok := countRows[0]["cnt"].(int64); ok {
					rowCount += cnt
				}
			}
		}
		
		totalRows += rowCount
		
		shardStats[i] = shardStat{
			ID:         i,
			Path:       shard.Path,
			SizeMB:     sizeMB,
			TableCount: len(tables),
			RowCount:   rowCount,
		}
	}
	
	writeJSON(w, http.StatusOK, map[string]any{
		"cluster": map[string]any{
			"total_shards": shardCount,
			"total_tables": len(tables),
			"total_rows":   totalRows,
		},
		"shards": shardStats,
	})
}

// isWriteSQL returns true if the SQL statement is a write operation.
func isWriteSQL(sql string) bool {
	kw := strings.ToUpper(strings.TrimSpace(sql))
	return strings.HasPrefix(kw, "INSERT") ||
		strings.HasPrefix(kw, "UPDATE") ||
		strings.HasPrefix(kw, "DELETE")
}

// applyPagination applies offset and limit to result rows
func applyPagination(rows []map[string]any, offset, limit int) []map[string]any {
	if offset < 0 {
		offset = 0
	}
	
	if offset >= len(rows) {
		return []map[string]any{}
	}
	
	rows = rows[offset:]
	
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	
	return rows
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

