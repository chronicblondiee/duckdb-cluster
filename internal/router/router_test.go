package router

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

func setupRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	m, err := shard.NewManager(dir, 3)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	r := NewRouter(m)
	t.Cleanup(func() {
		r.Close()
		m.CloseAll()
	})
	return r
}

func TestDDLBroadcast(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	result, err := r.Route(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("DDL route: %v", err)
	}
	if result.ShardID != -1 {
		t.Errorf("DDL shard_id should be -1, got %d", result.ShardID)
	}

	// Verify table exists on all shards by querying each
	for i := 0; i < r.Manager.ShardCount(); i++ {
		s := r.Manager.GetShard(i)
		_, err := s.Query(ctx, "SELECT * FROM test")
		if err != nil {
			t.Errorf("shard %d: table not created: %v", i, err)
		}
	}
}

func TestWriteRoutesToOneShard(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	// Create table first
	_, err := r.Route(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("DDL: %v", err)
	}

	// Insert with partition key
	result, err := r.Route(ctx, "INSERT INTO test VALUES (1, 'alice')", "1")
	if err != nil {
		t.Fatalf("INSERT: %v", err)
	}
	if result.ShardID < 0 || result.ShardID >= r.Manager.ShardCount() {
		t.Errorf("unexpected shard_id: %d", result.ShardID)
	}
}

func TestWriteRequiresPartitionKey(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "INSERT INTO test VALUES (1, 'alice')", "")
	if err == nil {
		t.Fatal("expected error for INSERT without partition_key")
	}
}

func TestReadFanOut(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "CREATE TABLE test (id INTEGER, name VARCHAR)", "")
	if err != nil {
		t.Fatalf("DDL: %v", err)
	}

	// Insert to different shards
	for i := 0; i < 10; i++ {
		key := string(rune('0' + i))
		sql := "INSERT INTO test VALUES (" + key + ", 'user" + key + "')"
		_, err := r.Route(ctx, sql, key)
		if err != nil {
			t.Fatalf("INSERT %d: %v", i, err)
		}
	}

	// Select should fan out and merge
	result, err := r.Route(ctx, "SELECT * FROM test", "")
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if result.ShardID != -1 {
		t.Errorf("SELECT shard_id should be -1, got %d", result.ShardID)
	}
	if len(result.Rows) != 10 {
		t.Errorf("expected 10 merged rows, got %d", len(result.Rows))
	}
}

// TestMergeAggregations tests that aggregations are properly merged
func TestMergeAggregations(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	// Create table with numeric data
	_, err := r.Route(ctx, "CREATE TABLE sales (id INTEGER, amount INTEGER, region VARCHAR)", "")
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	// Insert data across shards
	testData := []struct {
		id     int
		amount int
		region string
	}{
		{1, 100, "north"},
		{2, 200, "south"},
		{3, 150, "north"},
		{4, 300, "south"},
		{5, 250, "north"},
		{6, 400, "south"},
	}

	for _, d := range testData {
		sql := fmt.Sprintf("INSERT INTO sales VALUES (%d, %d, '%s')", d.id, d.amount, d.region)
		_, err := r.Route(ctx, sql, fmt.Sprintf("%d", d.id))
		if err != nil {
			t.Fatalf("INSERT: %v", err)
		}
	}

	tests := []struct {
		name     string
		sql      string
		expected map[string]any
	}{
		{
			name: "COUNT",
			sql:  "SELECT COUNT(*) as cnt FROM sales",
			expected: map[string]any{
				"cnt": int64(6),
			},
		},
		{
			name: "SUM",
			sql:  "SELECT SUM(amount) as total FROM sales",
			expected: map[string]any{
				"total": int64(1400), // 100+200+150+300+250+400
			},
		},
		{
			name: "AVG",
			sql:  "SELECT AVG(amount) as avg FROM sales",
			expected: map[string]any{
				"avg": float64(233.33), // approximate
			},
		},
		{
			name: "MIN",
			sql:  "SELECT MIN(amount) as min FROM sales",
			expected: map[string]any{
				"min": int64(100),
			},
		},
		{
			name: "MAX",
			sql:  "SELECT MAX(amount) as max FROM sales",
			expected: map[string]any{
				"max": int64(400),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.Route(ctx, tt.sql, "")
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}

			if len(result.Rows) != 1 {
				t.Fatalf("expected 1 row, got %d", len(result.Rows))
			}

			row := result.Rows[0]
			for key, expectedVal := range tt.expected {
				actualVal, ok := row[key]
				if !ok {
					t.Errorf("column %s not found in result", key)
					continue
				}

				// Handle numeric comparisons with type flexibility
				if tt.name == "AVG" {
					// For averages, check approximate equality
					actual := toFloat64(actualVal)
					expected := toFloat64(expectedVal)
					if abs(actual-expected) > 0.1 {
						t.Errorf("%s: expected %v, got %v", key, expected, actual)
					}
				} else {
					// For other aggregations, convert to int64 for comparison
					actual := toInt64(actualVal)
					expected := toInt64(expectedVal)
					if actual != expected {
						t.Errorf("%s: expected %v, got %v", key, expected, actual)
					}
				}
			}
		})
	}
}

// TestMergeGroupBy tests GROUP BY aggregations
func TestMergeGroupBy(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "CREATE TABLE sales (id INTEGER, amount INTEGER, region VARCHAR)", "")
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	testData := []struct {
		id     int
		amount int
		region string
	}{
		{1, 100, "north"},
		{2, 200, "south"},
		{3, 150, "north"},
		{4, 300, "south"},
		{5, 250, "north"},
		{6, 400, "south"},
	}

	for _, d := range testData {
		sql := fmt.Sprintf("INSERT INTO sales VALUES (%d, %d, '%s')", d.id, d.amount, d.region)
		_, err := r.Route(ctx, sql, fmt.Sprintf("%d", d.id))
		if err != nil {
			t.Fatalf("INSERT: %v", err)
		}
	}

	result, err := r.Route(ctx, "SELECT region, COUNT(*) as cnt, SUM(amount) as total FROM sales GROUP BY region", "")
	if err != nil {
		t.Fatalf("GROUP BY query: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result.Rows))
	}

	// Build map of results by region
	results := make(map[string]map[string]any)
	for _, row := range result.Rows {
		region := row["region"].(string)
		results[region] = row
	}

	// Verify north region
	if north, ok := results["north"]; ok {
		if toInt64(north["cnt"]) != 3 {
			t.Errorf("north count: expected 3, got %v", north["cnt"])
		}
		if toInt64(north["total"]) != 500 { // 100+150+250
			t.Errorf("north total: expected 500, got %v", north["total"])
		}
	} else {
		t.Error("north region not found")
	}

	// Verify south region
	if south, ok := results["south"]; ok {
		if toInt64(south["cnt"]) != 3 {
			t.Errorf("south count: expected 3, got %v", south["cnt"])
		}
		if toInt64(south["total"]) != 900 { // 200+300+400
			t.Errorf("south total: expected 900, got %v", south["total"])
		}
	} else {
		t.Error("south region not found")
	}
}

// TestMergeOrderBy tests ORDER BY clause
func TestMergeOrderBy(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "CREATE TABLE items (id INTEGER, value INTEGER)", "")
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	// Insert unsorted data
	testData := []struct{ id, value int }{
		{5, 50}, {2, 20}, {8, 80}, {1, 10}, {9, 90}, {3, 30}, {7, 70}, {4, 40}, {6, 60},
	}

	for _, d := range testData {
		sql := fmt.Sprintf("INSERT INTO items VALUES (%d, %d)", d.id, d.value)
		_, err := r.Route(ctx, sql, fmt.Sprintf("%d", d.id))
		if err != nil {
			t.Fatalf("INSERT: %v", err)
		}
	}

	// Test ORDER BY ASC
	result, err := r.Route(ctx, "SELECT * FROM items ORDER BY value ASC", "")
	if err != nil {
		t.Fatalf("ORDER BY ASC: %v", err)
	}

	if len(result.Rows) != len(testData) {
		t.Fatalf("expected %d rows, got %d", len(testData), len(result.Rows))
	}

	// Verify ascending order
	for i := 0; i < len(result.Rows)-1; i++ {
		curr := toInt64(result.Rows[i]["value"])
		next := toInt64(result.Rows[i+1]["value"])
		if curr > next {
			t.Errorf("not sorted ascending: row %d value %d > row %d value %d", i, curr, i+1, next)
		}
	}

	// Test ORDER BY DESC
	result, err = r.Route(ctx, "SELECT * FROM items ORDER BY value DESC", "")
	if err != nil {
		t.Fatalf("ORDER BY DESC: %v", err)
	}

	// Verify descending order
	for i := 0; i < len(result.Rows)-1; i++ {
		curr := toInt64(result.Rows[i]["value"])
		next := toInt64(result.Rows[i+1]["value"])
		if curr < next {
			t.Errorf("not sorted descending: row %d value %d < row %d value %d", i, curr, i+1, next)
		}
	}
}

// TestMergeLimit tests LIMIT and OFFSET
func TestMergeLimit(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "CREATE TABLE items (id INTEGER)", "")
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	// Insert 30 rows
	for i := 1; i <= 30; i++ {
		sql := fmt.Sprintf("INSERT INTO items VALUES (%d)", i)
		_, err := r.Route(ctx, sql, fmt.Sprintf("%d", i))
		if err != nil {
			t.Fatalf("INSERT: %v", err)
		}
	}

	// Test LIMIT
	result, err := r.Route(ctx, "SELECT * FROM items ORDER BY id LIMIT 10", "")
	if err != nil {
		t.Fatalf("LIMIT: %v", err)
	}

	if len(result.Rows) != 10 {
		t.Errorf("LIMIT 10: expected 10 rows, got %d", len(result.Rows))
	}

	// Test LIMIT with OFFSET
	result, err = r.Route(ctx, "SELECT * FROM items ORDER BY id LIMIT 5 OFFSET 10", "")
	if err != nil {
		t.Fatalf("LIMIT OFFSET: %v", err)
	}

	if len(result.Rows) != 5 {
		t.Errorf("LIMIT 5 OFFSET 10: expected 5 rows, got %d", len(result.Rows))
	}

	// Verify correct offset (should be ids 11-15)
	if len(result.Rows) > 0 {
		firstID := toInt64(result.Rows[0]["id"])
		if firstID != 11 {
			t.Errorf("OFFSET 10: expected first id to be 11, got %d", firstID)
		}
	}
}

// TestMergeDistinct tests SELECT DISTINCT
func TestMergeDistinct(t *testing.T) {
	r := setupRouter(t)
	ctx := context.Background()

	_, err := r.Route(ctx, "CREATE TABLE items (category VARCHAR)", "")
	if err != nil {
		t.Fatalf("CREATE: %v", err)
	}

	// Insert duplicate categories across shards
	categories := []string{"A", "B", "C", "A", "B", "C", "A", "B", "C"}
	for i, cat := range categories {
		sql := fmt.Sprintf("INSERT INTO items VALUES ('%s')", cat)
		_, err := r.Route(ctx, sql, fmt.Sprintf("%d", i))
		if err != nil {
			t.Fatalf("INSERT: %v", err)
		}
	}

	// Test DISTINCT
	result, err := r.Route(ctx, "SELECT DISTINCT category FROM items ORDER BY category", "")
	if err != nil {
		t.Fatalf("DISTINCT: %v", err)
	}

	if len(result.Rows) != 3 {
		t.Errorf("DISTINCT: expected 3 unique categories, got %d", len(result.Rows))
	}

	// Verify correct categories
	expected := []string{"A", "B", "C"}
	for i, row := range result.Rows {
		cat := row["category"].(string)
		if cat != expected[i] {
			t.Errorf("row %d: expected %s, got %s", i, expected[i], cat)
		}
	}
}

// TestFirstKeyword tests SQL keyword extraction with comments, CTEs, and edge cases
func TestFirstKeyword(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		// Basic cases
		{"simple select", "SELECT * FROM t", "SELECT"},
		{"simple insert", "INSERT INTO t VALUES (1)", "INSERT"},
		{"simple update", "UPDATE t SET x = 1", "UPDATE"},
		{"simple delete", "DELETE FROM t", "DELETE"},
		{"simple create", "CREATE TABLE t (id INT)", "CREATE"},
		{"simple drop", "DROP TABLE t", "DROP"},
		{"simple alter", "ALTER TABLE t ADD COLUMN x INT", "ALTER"},
		{"lowercase", "select * from t", "SELECT"},
		{"mixed case", "SeLeCt * from t", "SELECT"},

		// Whitespace
		{"leading spaces", "   SELECT * FROM t", "SELECT"},
		{"leading tabs", "\t\tSELECT * FROM t", "SELECT"},
		{"leading newlines", "\n\nSELECT * FROM t", "SELECT"},
		{"leading mixed whitespace", "  \t\n  INSERT INTO t VALUES (1)", "INSERT"},

		// Line comments
		{"line comment before select", "-- this is a comment\nSELECT * FROM t", "SELECT"},
		{"line comment before insert", "-- write op\nINSERT INTO t VALUES (1)", "INSERT"},
		{"multiple line comments", "-- comment 1\n-- comment 2\nSELECT * FROM t", "SELECT"},
		{"line comment no newline", "-- comment", ""},

		// Block comments
		{"block comment before select", "/* comment */ SELECT * FROM t", "SELECT"},
		{"block comment before insert", "/* write */ INSERT INTO t VALUES (1)", "INSERT"},
		{"multi-line block comment", "/* line1\nline2\nline3 */ DELETE FROM t", "DELETE"},
		{"nested-looking block comment", "/* a /* b */ SELECT * FROM t", "SELECT"},
		{"unterminated block comment", "/* never closed", ""},

		// CTE (WITH ... AS)
		{"CTE with select", "WITH cte AS (SELECT 1) SELECT * FROM cte", "SELECT"},
		{"CTE with insert", "WITH cte AS (SELECT 1) INSERT INTO t SELECT * FROM cte", "INSERT"},
		{"CTE with nested select", "WITH a AS (SELECT * FROM x), b AS (SELECT * FROM y) DELETE FROM t", "DELETE"},

		// Parenthesized subexpressions
		{"subquery", "(SELECT 1) SELECT * FROM t", "SELECT"},

		// Quoted strings containing keywords
		{"keyword in single quotes", "SELECT 'INSERT INTO' FROM t", "SELECT"},
		{"keyword in double quotes", `SELECT "DELETE" FROM t`, "SELECT"},
		{"keyword in backticks", "SELECT `UPDATE` FROM t", "SELECT"},
		{"escaped single quote", "SELECT 'it''s INSERT' FROM t", "SELECT"},

		// Edge cases
		{"empty string", "", ""},
		{"whitespace only", "   \t\n  ", ""},
		{"comment only", "-- just a comment\n", ""},
		{"semicolon prefix", ";SELECT * FROM t", "SELECT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstKeyword(tt.sql)
			if got != tt.expected {
				t.Errorf("firstKeyword(%q) = %q, want %q", tt.sql, got, tt.expected)
			}
		})
	}
}

// TestIsWriteSQL tests the exported write detection function
func TestIsWriteSQL(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected bool
	}{
		// Write operations
		{"insert", "INSERT INTO t VALUES (1)", true},
		{"update", "UPDATE t SET x = 1", true},
		{"delete", "DELETE FROM t", true},
		{"insert with comment", "-- comment\nINSERT INTO t VALUES (1)", true},
		{"insert with block comment", "/* comment */ INSERT INTO t VALUES (1)", true},
		{"CTE insert", "WITH cte AS (SELECT 1) INSERT INTO t SELECT * FROM cte", true},

		// Non-write operations
		{"select", "SELECT * FROM t", false},
		{"create", "CREATE TABLE t (id INT)", false},
		{"drop", "DROP TABLE t", false},
		{"alter", "ALTER TABLE t ADD COLUMN x INT", false},
		{"empty", "", false},

		// Tricky: SELECT with write keywords in strings/comments
		{"select with insert in string", "SELECT 'INSERT' FROM t", false},
		{"select with delete in comment", "-- DELETE\nSELECT * FROM t", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsWriteSQL(tt.sql)
			if got != tt.expected {
				t.Errorf("IsWriteSQL(%q) = %v, want %v", tt.sql, got, tt.expected)
			}
		})
	}
}

// Helper functions for type conversions
func toInt64(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int32:
		return int64(val)
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case float32:
		return int64(val)
	case *big.Int:
		if val != nil {
			return val.Int64()
		}
		return 0
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	case int:
		return float64(val)
	default:
		return 0
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
