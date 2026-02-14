package api

import (
	"net/http"
	"strings"
	"sync"

	"github.com/chronicblondiee/duckdb-cluster/internal/index"
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

	// Fan-out query to all indices in parallel
	results := make([]indexResult, len(indices))
	var wg sync.WaitGroup

	for i, idx := range indices {
		wg.Add(1)
		go func(pos int, idx *index.Index) {
			defer wg.Done()
			result, err := idx.Router.Route(r.Context(), req.SQL, req.PartitionKey)
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
