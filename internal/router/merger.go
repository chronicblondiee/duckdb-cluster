package router

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"

	_ "github.com/duckdb/duckdb-go/v2"
)

// MergeEngine uses an in-memory DuckDB to properly merge results
type MergeEngine struct {
	db      *sql.DB
	mu      sync.Mutex
	counter uint64
}

// NewMergeEngine creates a new merge engine with an in-memory DuckDB instance
func NewMergeEngine() (*MergeEngine, error) {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("create merge engine: %w", err)
	}
	return &MergeEngine{db: db}, nil
}

// Close closes the merge engine database
func (m *MergeEngine) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// Merge takes results from multiple shards and properly merges them by:
// 1. Creating a temporary table with the result schema
// 2. Inserting all shard results
// 3. Re-executing the original SQL against the merged data
// 4. Returning the correctly merged results
func (m *MergeEngine) Merge(ctx context.Context, sqlQuery string, results []*shard.QueryResultSet) ([]map[string]any, error) {
	// Lock for concurrent access to the merge engine
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// If no results, return empty
	if len(results) == 0 {
		return []map[string]any{}, nil
	}

	// Find first non-empty result set for schema
	var schema *shard.QueryResultSet
	for _, rs := range results {
		if len(rs.Columns) > 0 {
			schema = rs
			break
		}
	}
	if schema == nil {
		return []map[string]any{}, nil
	}

	// Check if this is an aggregation query by looking at the results
	// Aggregations return different column names than the source tables
	// For now, use simple concatenation for aggregations as a fallback
	// This is a known limitation that will be fixed in a future iteration
	isAggregation := containsAggregation(sqlQuery)
	
	if isAggregation {
		// For aggregations, we just concatenate the results
		// This is a limitation: aggregations won't be fully correct across shards
		// A proper fix requires query rewriting which is complex
		var merged []map[string]any
		for _, rs := range results {
			merged = append(merged, rs.Rows...)
		}
		return merged, nil
	}

	// Create unique temporary table name using atomic counter
	tableID := atomic.AddUint64(&m.counter, 1)
	tempTable := fmt.Sprintf("temp_merge_%d", tableID)

	// Build CREATE TABLE statement
	var colDefs []string
	for i, col := range schema.Columns {
		typeName := schema.Types[i]
		// Map DuckDB types to proper SQL types
		sqlType := mapDuckDBType(typeName)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col), sqlType))
	}
	createSQL := fmt.Sprintf("CREATE TEMP TABLE %s (%s)", tempTable, strings.Join(colDefs, ", "))

	// Create temp table
	if _, err := m.db.ExecContext(ctx, createSQL); err != nil {
		return nil, fmt.Errorf("create temp table: %w", err)
	}

	// Defer cleanup
	defer m.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))

	// Insert all rows from all shards
	for _, rs := range results {
		if len(rs.Rows) == 0 {
			continue
		}

		for _, row := range rs.Rows {
			// Build INSERT statement
			placeholders := make([]string, len(schema.Columns))
			values := make([]any, len(schema.Columns))
			for i, col := range schema.Columns {
				placeholders[i] = "?"
				values[i] = row[col]
			}
			insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				tempTable,
				strings.Join(quoteIdentifiers(schema.Columns), ", "),
				strings.Join(placeholders, ", "))

			if _, err := m.db.ExecContext(ctx, insertSQL, values...); err != nil {
				return nil, fmt.Errorf("insert into temp table: %w", err)
			}
		}
	}

	// Replace table name in original query with temp table
	mergeSQL := replaceTableWithTemp(sqlQuery, tempTable)

	// Execute the modified query against temp table
	rows, err := m.db.QueryContext(ctx, mergeSQL)
	if err != nil {
		return nil, fmt.Errorf("execute merge query: %w", err)
	}
	defer rows.Close()

	// Read results
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var merged []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		merged = append(merged, row)
	}

	return merged, rows.Err()
}

// MergeAndQuery handles queries with aggregations, ORDER BY, LIMIT, etc.
// It takes raw data from shards and applies the full query logic
func (m *MergeEngine) MergeAndQuery(ctx context.Context, originalQuery string, tableName string, results []*shard.QueryResultSet) ([]map[string]any, error) {
	// Lock for concurrent access
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// If no results, return empty
	if len(results) == 0 {
		return []map[string]any{}, nil
	}

	// Find first non-empty result set for schema
	var schema *shard.QueryResultSet
	for _, rs := range results {
		if len(rs.Columns) > 0 {
			schema = rs
			break
		}
	}
	if schema == nil {
		return []map[string]any{}, nil
	}

	// Create unique temporary table name using atomic counter
	tableID := atomic.AddUint64(&m.counter, 1)
	tempTable := fmt.Sprintf("temp_merge_data_%d", tableID)

	// Build CREATE TABLE statement
	var colDefs []string
	for i, col := range schema.Columns {
		typeName := schema.Types[i]
		sqlType := mapDuckDBType(typeName)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col), sqlType))
	}
	createSQL := fmt.Sprintf("CREATE TEMP TABLE %s (%s)", tempTable, strings.Join(colDefs, ", "))

	// Create temp table
	if _, err := m.db.ExecContext(ctx, createSQL); err != nil {
		return nil, fmt.Errorf("create temp table: %w", err)
	}

	// Defer cleanup
	defer m.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))

	// Insert all rows from all shards
	for _, rs := range results {
		if len(rs.Rows) == 0 {
			continue
		}

		for _, row := range rs.Rows {
			placeholders := make([]string, len(schema.Columns))
			values := make([]any, len(schema.Columns))
			for i, col := range schema.Columns {
				placeholders[i] = "?"
				values[i] = row[col]
			}
			insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
				tempTable,
				strings.Join(quoteIdentifiers(schema.Columns), ", "),
				strings.Join(placeholders, ", "))

			if _, err := m.db.ExecContext(ctx, insertSQL, values...); err != nil {
				return nil, fmt.Errorf("insert into temp table: %w", err)
			}
		}
	}

	// Replace table name in original query with temp table
	mergeSQL := replaceTableWithTemp(originalQuery, tempTable)

	// Execute the query against merged data
	rows, err := m.db.QueryContext(ctx, mergeSQL)
	if err != nil {
		return nil, fmt.Errorf("execute merge query: %w", err)
	}
	defer rows.Close()

	// Read results
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var merged []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		merged = append(merged, row)
	}

	return merged, rows.Err()
}

// containsAggregation checks if a query contains aggregation functions
func containsAggregation(sql string) bool {
	upper := strings.ToUpper(sql)
	aggregations := []string{"COUNT(", "SUM(", "AVG(", "MIN(", "MAX(", "GROUP BY"}
	for _, agg := range aggregations {
		if strings.Contains(upper, agg) {
			return true
		}
	}
	return false
}

// mapDuckDBType maps DuckDB type names to SQL types
func mapDuckDBType(typeName string) string {
	upper := strings.ToUpper(typeName)
	switch {
	case strings.Contains(upper, "INT"):
		return "INTEGER"
	case strings.Contains(upper, "VARCHAR"), strings.Contains(upper, "TEXT"), strings.Contains(upper, "STRING"):
		return "VARCHAR"
	case strings.Contains(upper, "DECIMAL"), strings.Contains(upper, "NUMERIC"):
		return "DECIMAL"
	case strings.Contains(upper, "DOUBLE"), strings.Contains(upper, "FLOAT"):
		return "DOUBLE"
	case strings.Contains(upper, "BOOL"):
		return "BOOLEAN"
	case strings.Contains(upper, "DATE"):
		return "DATE"
	case strings.Contains(upper, "TIME"):
		if strings.Contains(upper, "STAMP") {
			return "TIMESTAMP"
		}
		return "TIME"
	case strings.Contains(upper, "BLOB"), strings.Contains(upper, "BINARY"):
		return "BLOB"
	default:
		return "VARCHAR" // Safe default
	}
}

// quoteIdentifier quotes a SQL identifier
func quoteIdentifier(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `""`) + `"`
}

// quoteIdentifiers quotes multiple identifiers
func quoteIdentifiers(ids []string) []string {
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = quoteIdentifier(id)
	}
	return quoted
}

// replaceTableWithTemp is a simple table name replacement
// For a production system, this would need proper SQL parsing
func replaceTableWithTemp(sqlQuery, tempTable string) string {
	// This is a simplified implementation
	// In production, you'd want to use a proper SQL parser
	// For now, we'll use a simple FROM clause replacement
	
	// Handle common patterns
	query := sqlQuery
	
	// Find FROM clause and replace table name
	// This is a basic heuristic - assumes single table queries
	fromIdx := strings.Index(strings.ToUpper(query), "FROM")
	if fromIdx != -1 {
		// Extract the part after FROM
		afterFrom := query[fromIdx+4:]
		
		// Find the table name (up to space, WHERE, GROUP, ORDER, LIMIT, or end)
		tokens := strings.Fields(afterFrom)
		if len(tokens) > 0 {
			oldTable := tokens[0]
			// Remove any semicolon
			oldTable = strings.TrimSuffix(oldTable, ";")
			// Replace in the query
			query = strings.Replace(query, "FROM "+oldTable, "FROM "+tempTable, 1)
		}
	}
	
	return query
}

// MergeAndQueryFromMaps merges map-based results (e.g. from cross-index queries)
// and re-executes the original query against the combined data.
// Returns the merged rows and result column names.
func (m *MergeEngine) MergeAndQueryFromMaps(ctx context.Context, originalQuery string, tableName string, columns []string, allRows []map[string]any) ([]map[string]any, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(allRows) == 0 {
		return []map[string]any{}, columns, nil
	}

	// Infer types from first row
	types := make([]string, len(columns))
	for i, col := range columns {
		types[i] = inferDuckDBType(allRows[0][col])
	}

	// Create unique temporary table
	tableID := atomic.AddUint64(&m.counter, 1)
	tempTable := fmt.Sprintf("temp_cross_%d", tableID)

	var colDefs []string
	for i, col := range columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col), types[i]))
	}
	createSQL := fmt.Sprintf("CREATE TEMP TABLE %s (%s)", tempTable, strings.Join(colDefs, ", "))

	if _, err := m.db.ExecContext(ctx, createSQL); err != nil {
		return nil, nil, fmt.Errorf("create temp table: %w", err)
	}
	defer m.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTable))

	// Insert all rows
	for _, row := range allRows {
		placeholders := make([]string, len(columns))
		values := make([]any, len(columns))
		for i, col := range columns {
			placeholders[i] = "?"
			values[i] = row[col]
		}
		insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
			tempTable,
			strings.Join(quoteIdentifiers(columns), ", "),
			strings.Join(placeholders, ", "))

		if _, err := m.db.ExecContext(ctx, insertSQL, values...); err != nil {
			return nil, nil, fmt.Errorf("insert into temp table: %w", err)
		}
	}

	// Replace table name and execute original query
	mergeSQL := replaceTableWithTemp(originalQuery, tempTable)

	rows, err := m.db.QueryContext(ctx, mergeSQL)
	if err != nil {
		return nil, nil, fmt.Errorf("execute merge query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var merged []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = vals[i]
		}
		merged = append(merged, row)
	}

	return merged, cols, rows.Err()
}

// inferDuckDBType infers a DuckDB SQL type from a Go value.
func inferDuckDBType(val any) string {
	switch val.(type) {
	case int, int8, int16, int32, int64:
		return "BIGINT"
	case uint, uint8, uint16, uint32, uint64:
		return "BIGINT"
	case float32, float64:
		return "DOUBLE"
	case bool:
		return "BOOLEAN"
	default:
		return "VARCHAR"
	}
}

func MergeResults(results [][]map[string]any) []map[string]any {
	var merged []map[string]any
	for _, r := range results {
		merged = append(merged, r...)
	}
	return merged
}
