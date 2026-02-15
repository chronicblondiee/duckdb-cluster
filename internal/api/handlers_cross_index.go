package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
	"github.com/chronicblondiee/duckdb-cluster/internal/router"
)

// isMultiIndexSpec returns true if the spec refers to multiple indices.
func isMultiIndexSpec(spec string) bool {
	return strings.Contains(spec, ",") || strings.Contains(spec, "*")
}

type indexResult struct {
	columns []string
	rows    []map[string]any
	err     error
}

// handleCrossIndexQuery handles queries that target multiple indices.
func (s *Server) handleCrossIndexQuery(w http.ResponseWriter, r *http.Request, req queryRequest) {
	if isWriteSQL(req.SQL) {
		writeJSON(w, http.StatusBadRequest, queryResponse{
			Error: "write operations not supported on multi-index queries",
		})
		return
	}

	indices, err := s.resolveMultipleIndices(req.Index)
	if err != nil {
		writeJSON(w, http.StatusNotFound, queryResponse{Error: err.Error()})
		return
	}

	// For aggregation queries, use two-phase merge
	if router.RequiresMergeEngine(req.SQL) && s.mergeEngine != nil {
		s.handleCrossIndexAggregation(w, r, req, indices)
		return
	}

	// Fan-out query to all indices in parallel
	results := fanOutToIndices(r, req.SQL, indices)

	// Merge results
	merged := mergeQueryResults(results)

	// Apply pagination
	rows := merged.rows
	if req.Limit > 0 || req.Offset > 0 {
		rows = applyPagination(rows, req.Offset, req.Limit)
	}

	writeJSON(w, http.StatusOK, queryResponse{
		Success: true,
		Columns: merged.columns,
		Rows:    rows,
		ShardID: -1, // indicates cross-index result
	})
}

type mergedResult struct {
	columns []string
	rows    []map[string]any
}

// fanOutToIndices executes a SQL query on all indices in parallel.
func fanOutToIndices(r *http.Request, sql string, indices []*index.Index) []indexResult {
	results := make([]indexResult, len(indices))
	var wg sync.WaitGroup

	for i, idx := range indices {
		wg.Add(1)
		go func(pos int, idx *index.Index) {
			defer wg.Done()
			result, err := idx.Route(r.Context(), sql, "")
			if err != nil {
				results[pos] = indexResult{err: err}
				return
			}
			results[pos] = indexResult{
				columns: result.Columns,
				rows:    result.Rows,
			}
		}(i, idx)
	}

	wg.Wait()
	return results
}

// handleCrossIndexAggregation handles aggregation queries across multiple indices.
// It collects raw data from each index, then re-executes the original query
// against the combined dataset using the server's MergeEngine.
func (s *Server) handleCrossIndexAggregation(w http.ResponseWriter, r *http.Request, req queryRequest, indices []*index.Index) {
	tableName := router.ExtractTableName(req.SQL)
	if tableName == "" {
		writeJSON(w, http.StatusBadRequest, queryResponse{
			Error: "could not extract table name from query",
		})
		return
	}

	// Phase 1: Collect raw data from all indices
	rawQuery := fmt.Sprintf("SELECT * FROM %s", tableName)
	results := fanOutToIndices(r, rawQuery, indices)

	// Merge raw rows from all indices
	merged := mergeQueryResults(results)
	if len(merged.rows) == 0 {
		writeJSON(w, http.StatusOK, queryResponse{
			Success: true,
			Columns: merged.columns,
			Rows:    []map[string]any{},
			ShardID: -1,
		})
		return
	}

	// Phase 2: Re-execute original query against combined raw data
	aggRows, aggCols, err := s.mergeEngine.MergeAndQueryFromMaps(
		r.Context(), req.SQL, tableName, merged.columns, merged.rows,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, queryResponse{
			Error: fmt.Sprintf("cross-index aggregation: %s", err),
		})
		return
	}

	// Apply pagination
	rows := aggRows
	if req.Limit > 0 || req.Offset > 0 {
		rows = applyPagination(rows, req.Offset, req.Limit)
	}

	writeJSON(w, http.StatusOK, queryResponse{
		Success: true,
		Columns: aggCols,
		Rows:    rows,
		ShardID: -1,
	})
}

// mergeQueryResults merges results from multiple indices.
func mergeQueryResults(results []indexResult) mergedResult {
	columnSet := make(map[string]bool)
	var allRows []map[string]any
	var columns []string

	for _, r := range results {
		if r.err != nil {
			continue // skip failed indices
		}
		for _, col := range r.columns {
			if !columnSet[col] {
				columnSet[col] = true
				columns = append(columns, col)
			}
		}
		allRows = append(allRows, r.rows...)
	}

	return mergedResult{columns: columns, rows: allRows}
}
