package router

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/chronicblondiee/duckdb-cluster/internal/shard"
)

type QueryResult struct {
	Columns      []string         `json:"columns,omitempty"`
	Rows         []map[string]any `json:"rows"`
	RowsAffected int64            `json:"rows_affected,omitempty"`
	ShardID      int              `json:"shard_id"`
}

type Router struct {
	Manager     *shard.Manager
	MergeEngine *MergeEngine
}

func NewRouter(m *shard.Manager) *Router {
	mergeEngine, err := NewMergeEngine()
	if err != nil {
		// In case of error, create router without merge engine
		// It will fall back to simple concatenation
		return &Router{Manager: m}
	}
	return &Router{
		Manager:     m,
		MergeEngine: mergeEngine,
	}
}

// Close closes the router and its resources
func (r *Router) Close() error {
	if r.MergeEngine != nil {
		return r.MergeEngine.Close()
	}
	return nil
}

// HashRoute returns the shard ID for a given partition key
func (r *Router) HashRoute(partitionKey string) int {
	return HashRoute(partitionKey, r.Manager.ShardCount())
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

// IsWriteSQL reports whether the provided SQL statement is a write operation.
func IsWriteSQL(sql string) bool {
	return isWriteKeyword(firstKeyword(sql))
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
	// Fast path: single shard — execute directly, no merge needed.
	if r.Manager.ShardCount() == 1 {
		s := r.Manager.GetShard(0)
		rs, err := s.QueryWithSchema(ctx, sqlStr)
		if err != nil {
			return nil, err
		}
		// log.Printf("DEBUG: Single-shard fast path used for query, returned %d rows", len(rs.Rows))
		return &QueryResult{Columns: rs.Columns, Rows: rs.Rows, ShardID: 0}, nil
	}

	needsMergeEngine := RequiresMergeEngine(sqlStr)

	var resultSets []*shard.QueryResultSet
	var err error

	if needsMergeEngine && r.MergeEngine != nil {
		tableName := ExtractTableName(sqlStr)
		if tableName == "" {
			return nil, fmt.Errorf("could not extract table name from query")
		}

		// Push WHERE, ORDER BY, and LIMIT to shards to reduce data transferred.
		baseQuery := buildShardQuery(tableName, sqlStr)
		resultSets, err = r.Manager.QueryAllWithSchema(ctx, baseQuery)
		if err != nil {
			return nil, err
		}

		merged, err := r.MergeEngine.MergeAndQuery(ctx, sqlStr, tableName, resultSets)
		if err != nil {
			return nil, fmt.Errorf("merge and query: %w", err)
		}

		var cols []string
		if len(merged) > 0 {
			for k := range merged[0] {
				cols = append(cols, k)
			}
		} else if len(resultSets) > 0 && len(resultSets[0].Columns) > 0 {
			cols = resultSets[0].Columns
		}

		return &QueryResult{
			Columns: cols,
			Rows:    merged,
			ShardID: -1,
		}, nil
	}

	// For simple queries (no aggregation/ORDER BY/LIMIT), fan out and concatenate.
	resultSets, err = r.Manager.QueryAllWithSchema(ctx, sqlStr)
	if err != nil {
		return nil, err
	}

	var merged []map[string]any
	var cols []string

	// Use MergeEngine if available for proper SQL semantics
	if r.MergeEngine != nil {
		merged, err = r.MergeEngine.Merge(ctx, sqlStr, resultSets)
		if err != nil {
			return nil, fmt.Errorf("merge results: %w", err)
		}

		// Get columns from merged results or from first result set
		if len(merged) > 0 {
			for k := range merged[0] {
				cols = append(cols, k)
			}
		} else if len(resultSets) > 0 && len(resultSets[0].Columns) > 0 {
			cols = resultSets[0].Columns
		}
	} else {
		// Fallback to simple concatenation if merge engine unavailable
		results := make([][]map[string]any, len(resultSets))
		for i, rs := range resultSets {
			results[i] = rs.Rows
		}
		merged = MergeResults(results)

		if len(merged) > 0 {
			for k := range merged[0] {
				cols = append(cols, k)
			}
		}
	}

	return &QueryResult{
		Columns: cols,
		Rows:    merged,
		ShardID: -1,
	}, nil
}

// requiresMergeEngine determines if a query requires the merge engine
func RequiresMergeEngine(sql string) bool {
	upper := strings.ToUpper(sql)
	keywords := []string{
		"COUNT(", "SUM(", "AVG(", "MIN(", "MAX(",
		"GROUP BY", "ORDER BY", "LIMIT", "DISTINCT",
	}
	for _, kw := range keywords {
		if strings.Contains(upper, kw) {
			return true
		}
	}
	return false
}

// extractTableName extracts the table name from a SELECT query
func ExtractTableName(sql string) string {
	upper := strings.ToUpper(sql)
	fromIdx := strings.Index(upper, "FROM")
	if fromIdx == -1 {
		return ""
	}
	
	afterFrom := strings.TrimSpace(sql[fromIdx+4:])
	tokens := strings.FieldsFunc(afterFrom, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ';'
	})
	
	if len(tokens) > 0 {
		return tokens[0]
	}
	return ""
}

// buildShardQuery constructs a query to push down to each shard.
// It keeps the WHERE, ORDER BY, and LIMIT clauses from the original query
// so each shard returns a pre-filtered, pre-sorted, bounded result set.
// The merge engine then re-applies the full query for correct global semantics.
func buildShardQuery(tableName, sqlStr string) string {
	q := fmt.Sprintf("SELECT * FROM %s", tableName)

	where := extractClause(sqlStr, "WHERE", []string{"GROUP BY", "ORDER BY", "LIMIT", "HAVING"})
	if where != "" {
		q += " WHERE " + where
	}

	orderBy := extractClause(sqlStr, "ORDER BY", []string{"LIMIT"})
	if orderBy != "" {
		q += " ORDER BY " + orderBy
	}

	// For LIMIT+OFFSET, push LIMIT (limit+offset) to each shard so enough
	// candidate rows survive for the merge engine to apply the real OFFSET.
	limitStr := extractClause(sqlStr, "LIMIT", []string{"OFFSET"})
	offsetStr := extractClause(sqlStr, "OFFSET", nil)
	if limitStr != "" {
		limit, lErr := strconv.Atoi(strings.TrimSpace(limitStr))
		offset := 0
		if offsetStr != "" {
			offset, _ = strconv.Atoi(strings.TrimSpace(offsetStr))
		}
		if lErr == nil {
			q += fmt.Sprintf(" LIMIT %d", limit+offset)
		}
	}

	return q
}

// extractClause extracts the content after a SQL keyword up to any of the
// terminator keywords (or end of string). Returns "" if the keyword is absent.
func extractClause(sql, keyword string, terminators []string) string {
	upper := strings.ToUpper(sql)
	idx := strings.Index(upper, keyword)
	if idx == -1 {
		return ""
	}

	after := sql[idx+len(keyword):]
	upperAfter := strings.ToUpper(after)

	end := len(after)
	for _, term := range terminators {
		ti := strings.Index(upperAfter, term)
		if ti != -1 && ti < end {
			end = ti
		}
	}

	clause := strings.TrimSpace(after[:end])
	clause = strings.TrimRight(clause, "; ")
	return clause
}

var statementKeywords = map[string]struct{}{
	"CREATE": {},
	"DROP":   {},
	"ALTER":  {},
	"INSERT": {},
	"UPDATE": {},
	"DELETE": {},
	"SELECT": {},
}

func firstKeyword(sql string) string {
	scanner := &sqlTokenScanner{input: sql}
	for {
		token, depth := scanner.nextToken()
		if token == "" {
			return ""
		}
		upper := strings.ToUpper(token)
		// Skip keywords inside sub-expressions, especially CTE bodies.
		// The "WITH ... AS (...)" form can contain SELECT/INSERT/UPDATE tokens in body;
		// only top-level tokens are eligible as routing keys.
		if depth == 0 {
			if _, ok := statementKeywords[upper]; ok {
				return upper
			}
		}
	}
}

func isWriteKeyword(keyword string) bool {
	switch keyword {
	case "INSERT", "UPDATE", "DELETE":
		return true
	default:
		return false
	}
}

type sqlTokenScanner struct {
	input string
	pos   int
	depth int
}

func (s *sqlTokenScanner) nextToken() (string, int) {
	for s.pos < len(s.input) {
		ch := s.input[s.pos]

		switch {
		case isWhitespace(ch):
			s.pos++
			continue
		case isLineCommentStart(s.input, s.pos):
			s.pos += 2
			for s.pos < len(s.input) {
				if s.input[s.pos] == '\n' {
					s.pos++
					break
				}
				s.pos++
			}
			continue
		case isBlockCommentStart(s.input, s.pos):
			s.pos += 2
			for s.pos+1 < len(s.input) {
				if s.input[s.pos] == '*' && s.input[s.pos+1] == '/' {
					s.pos += 2
					break
				}
				s.pos++
			}
			// If unterminated, consume remaining input
			if s.pos < len(s.input) && !(s.pos >= 2 && s.input[s.pos-2] == '*' && s.input[s.pos-1] == '/') {
				s.pos = len(s.input)
			}
			continue
		case ch == '(':
			s.depth++
			s.pos++
			continue
		case ch == ')':
			if s.depth > 0 {
				s.depth--
			}
			s.pos++
			continue
		case ch == ',' || ch == ';':
			s.pos++
			continue
		case ch == '\'':
			s.skipSingleQuotedString()
			continue
		case ch == '"':
			s.skipDoubleQuotedString()
			continue
		case ch == '`':
			s.skipBacktickQuotedString()
			continue
		case isIdentStart(ch):
			start := s.pos
			s.pos++
			for s.pos < len(s.input) && isIdentPart(s.input[s.pos]) {
				s.pos++
			}
			return s.input[start:s.pos], s.depth
		default:
			s.pos++
		}
	}
	return "", s.depth
}

func (s *sqlTokenScanner) skipSingleQuotedString() {
	s.pos++
	for s.pos < len(s.input) {
		if s.input[s.pos] == '\'' {
			if s.pos+1 < len(s.input) && s.input[s.pos+1] == '\'' {
				s.pos += 2
				continue
			}
			s.pos++
			return
		}
		s.pos++
	}
}

func (s *sqlTokenScanner) skipDoubleQuotedString() {
	s.pos++
	for s.pos < len(s.input) {
		if s.input[s.pos] == '"' {
			if s.pos+1 < len(s.input) && s.input[s.pos+1] == '"' {
				s.pos += 2
				continue
			}
			s.pos++
			return
		}
		s.pos++
	}
}

func (s *sqlTokenScanner) skipBacktickQuotedString() {
	s.pos++
	for s.pos < len(s.input) {
		if s.input[s.pos] == '`' {
			s.pos++
			return
		}
		s.pos++
	}
}

func isWhitespace(ch byte) bool {
	return unicode.IsSpace(rune(ch))
}

func isLineCommentStart(s string, pos int) bool {
	return pos+1 < len(s) && s[pos] == '-' && s[pos+1] == '-'
}

func isBlockCommentStart(s string, pos int) bool {
	return pos+1 < len(s) && s[pos] == '/' && s[pos+1] == '*'
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		ch == '_' ||
		ch == '@' ||
		ch == '#'
}

func isIdentPart(ch byte) bool {
	return isIdentStart(ch) ||
		(ch >= '0' && ch <= '9') ||
		ch == '.'
}
