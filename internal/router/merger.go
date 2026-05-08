package router

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"

	_ "github.com/duckdb/duckdb-go/v2"
)

// MergeEngine merges query results from multiple shards using DuckDB.
// Each merge operation opens its own in-memory DuckDB to avoid contention.
type MergeEngine struct{}

// NewMergeEngine creates a new merge engine.
func NewMergeEngine() (*MergeEngine, error) {
	return &MergeEngine{}, nil
}

// Close is a no-op since each operation manages its own database.
func (m *MergeEngine) Close() error {
	return nil
}

// openMergeDB opens a fresh in-memory DuckDB for a single merge operation.
func openMergeDB() (*sql.DB, error) {
	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("create merge db: %w", err)
	}
	return db, nil
}

// Merge takes results from multiple shards and properly merges them by:
// 1. Creating a temporary table with the result schema
// 2. Inserting all shard results in batches
// 3. Re-executing the original SQL against the merged data
// 4. Returning the correctly merged results
func (m *MergeEngine) Merge(ctx context.Context, sqlQuery string, results []*shard.QueryResultSet) ([]map[string]any, error) {
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

	// Fast path: if the query doesn't need re-aggregation or re-ordering,
	// just concatenate shard results — no need for a temp DuckDB.
	if !RequiresMergeEngine(sqlQuery) {
		var merged []map[string]any
		for _, rs := range results {
			merged = append(merged, rs.Rows...)
		}
		return merged, nil
	}

	// For aggregations, just concatenate — the caller (handleRead)
	// uses MergeAndQuery for proper re-aggregation when needed.
	if containsAggregation(sqlQuery) {
		var merged []map[string]any
		for _, rs := range results {
			merged = append(merged, rs.Rows...)
		}
		return merged, nil
	}

	db, err := openMergeDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tempTable := "merge_data"

	// Build CREATE TABLE statement
	var colDefs []string
	for i, col := range schema.Columns {
		typeName := schema.Types[i]
		sqlType := mapDuckDBType(typeName)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col), sqlType))
	}
	createSQL := fmt.Sprintf("CREATE TABLE %s (%s)", tempTable, strings.Join(colDefs, ", "))

	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return nil, fmt.Errorf("create temp table: %w", err)
	}

	// Batch insert all rows
	if err := batchInsertResultSets(ctx, db, tempTable, schema.Columns, results); err != nil {
		return nil, err
	}

	// Replace table name in original query with our table
	mergeSQL := replaceTableWithTemp(sqlQuery, tempTable)

	return queryRows(ctx, db, mergeSQL)
}

// MergeAndQuery handles queries with aggregations, ORDER BY, LIMIT, etc.
// It takes raw data from shards and applies the full query logic.
func (m *MergeEngine) MergeAndQuery(ctx context.Context, originalQuery string, tableName string, results []*shard.QueryResultSet) ([]map[string]any, error) {
	if len(results) == 0 {
		return []map[string]any{}, nil
	}

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

	db, err := openMergeDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	tempTable := "merge_data"

	var colDefs []string
	for i, col := range schema.Columns {
		typeName := schema.Types[i]
		sqlType := mapDuckDBType(typeName)
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col), sqlType))
	}
	createSQL := fmt.Sprintf("CREATE TABLE %s (%s)", tempTable, strings.Join(colDefs, ", "))

	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return nil, fmt.Errorf("create temp table: %w", err)
	}

	if err := batchInsertResultSets(ctx, db, tempTable, schema.Columns, results); err != nil {
		return nil, err
	}

	mergeSQL := replaceTableWithTemp(originalQuery, tempTable)
	return queryRows(ctx, db, mergeSQL)
}

// MergeAndQueryFromMaps merges map-based results (e.g. from cross-index queries)
// and re-executes the original query against the combined data.
func (m *MergeEngine) MergeAndQueryFromMaps(ctx context.Context, originalQuery string, tableName string, columns []string, allRows []map[string]any) ([]map[string]any, []string, error) {
	if len(allRows) == 0 {
		return []map[string]any{}, columns, nil
	}

	// Infer types from first row
	types := make([]string, len(columns))
	for i, col := range columns {
		types[i] = inferDuckDBType(allRows[0][col])
	}

	db, err := openMergeDB()
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	tempTable := "merge_data"

	var colDefs []string
	for i, col := range columns {
		colDefs = append(colDefs, fmt.Sprintf("%s %s", quoteIdentifier(col), types[i]))
	}
	createSQL := fmt.Sprintf("CREATE TABLE %s (%s)", tempTable, strings.Join(colDefs, ", "))

	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return nil, nil, fmt.Errorf("create temp table: %w", err)
	}

	// Batch insert all rows
	if err := batchInsertMaps(ctx, db, tempTable, columns, allRows); err != nil {
		return nil, nil, err
	}

	mergeSQL := replaceTableWithTemp(originalQuery, tempTable)

	rows, err := queryRows(ctx, db, mergeSQL)
	if err != nil {
		return nil, nil, err
	}

	var cols []string
	if len(rows) > 0 {
		for k := range rows[0] {
			cols = append(cols, k)
		}
	} else {
		cols = columns
	}

	return rows, cols, nil
}

// batchInsertResultSets inserts rows from QueryResultSets in batches.
func batchInsertResultSets(ctx context.Context, db *sql.DB, table string, columns []string, results []*shard.QueryResultSet) error {
	const batchSize = 5000

	quotedCols := strings.Join(quoteIdentifiers(columns), ", ")
	numCols := len(columns)

	var batch []map[string]any
	for _, rs := range results {
		for _, row := range rs.Rows {
			batch = append(batch, row)
			if len(batch) >= batchSize {
				if err := insertBatch(ctx, db, table, quotedCols, columns, numCols, batch); err != nil {
					return err
				}
				batch = batch[:0]
			}
		}
	}
	if len(batch) > 0 {
		return insertBatch(ctx, db, table, quotedCols, columns, numCols, batch)
	}
	return nil
}

// batchInsertMaps inserts map rows in batches.
func batchInsertMaps(ctx context.Context, db *sql.DB, table string, columns []string, rows []map[string]any) error {
	const batchSize = 5000

	quotedCols := strings.Join(quoteIdentifiers(columns), ", ")
	numCols := len(columns)

	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := insertBatch(ctx, db, table, quotedCols, columns, numCols, rows[i:end]); err != nil {
			return err
		}
	}
	return nil
}

// insertBatch inserts a batch of rows with a single multi-row INSERT statement.
func insertBatch(ctx context.Context, db *sql.DB, table, quotedCols string, columns []string, numCols int, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}

	// Build: INSERT INTO t (c1, c2) VALUES (?, ?), (?, ?), ...
	rowPlaceholder := "(" + strings.Repeat("?, ", numCols-1) + "?)"
	allPlaceholders := make([]string, len(rows))
	values := make([]any, 0, len(rows)*numCols)

	for i, row := range rows {
		allPlaceholders[i] = rowPlaceholder
		for _, col := range columns {
			values = append(values, row[col])
		}
	}

	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s",
		table, quotedCols, strings.Join(allPlaceholders, ", "))

	if _, err := db.ExecContext(ctx, insertSQL, values...); err != nil {
		return fmt.Errorf("batch insert into merge table: %w", err)
	}
	return nil
}

// queryRows executes a query and returns results as maps.
func queryRows(ctx context.Context, db *sql.DB, query string) ([]map[string]any, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("execute merge query: %w", err)
	}
	defer rows.Close()

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
func replaceTableWithTemp(sqlQuery, tempTable string) string {
	query := sqlQuery

	fromIdx := strings.Index(strings.ToUpper(query), "FROM")
	if fromIdx != -1 {
		afterFrom := query[fromIdx+4:]
		tokens := strings.Fields(afterFrom)
		if len(tokens) > 0 {
			oldTable := tokens[0]
			oldTable = strings.TrimSuffix(oldTable, ";")
			query = strings.Replace(query, "FROM "+oldTable, "FROM "+tempTable, 1)
		}
	}

	return query
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
