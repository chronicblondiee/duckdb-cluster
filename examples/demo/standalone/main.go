// Standalone DuckDB HTTP server for benchmarking.
// Implements just enough of the duckdb-cluster API for stress.sh to work.
// No auth, no sharding, no routing, no merging — pure DuckDB performance.
package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	_ "github.com/duckdb/duckdb-go/v2"
)

type server struct {
	db *sql.DB
	mu sync.RWMutex // protects table creation
}

func main() {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	s := &server{db: db}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /admin/auth/token", s.handleAuthToken)
	mux.HandleFunc("GET /indices", s.handleListIndices)
	mux.HandleFunc("PUT /indices/{name}", s.handleCreateIndex)
	mux.HandleFunc("DELETE /indices/{name}", s.handleDeleteIndex)
	mux.HandleFunc("POST /indices/{name}/_doc", s.handleIndexDoc)
	mux.HandleFunc("POST /indices/{name}/_bulk", s.handleBulkDoc)
	mux.HandleFunc("POST /query", s.handleQuery)

	log.Println("standalone duckdb listening on :8090")
	log.Fatal(http.ListenAndServe(":8090", mux))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *server) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      "standalone-dummy-token",
		"expires_in": 3600,
	})
}

func (s *server) handleListIndices(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query("SELECT table_name FROM information_schema.tables WHERE table_schema='main'")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	defer rows.Close()

	var indices []map[string]any
	for rows.Next() {
		var name string
		rows.Scan(&name)
		indices = append(indices, map[string]any{"name": name, "state": "open"})
	}
	if indices == nil {
		indices = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"indices": indices})
}

func (s *server) handleCreateIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	table := sanitizeName(name)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %q (
		id BIGINT,
		value BIGINT,
		category VARCHAR,
		message VARCHAR,
		ts VARCHAR
	)`, table))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"acknowledged": true,
		"index":        name,
		"shards":       1,
	})
}

func (s *server) handleDeleteIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	table := sanitizeName(name)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %q", table))
	writeJSON(w, http.StatusOK, map[string]any{"acknowledged": true})
}

func (s *server) handleIndexDoc(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	table := sanitizeName(name)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if err := s.insertDoc(table, doc); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"result":   "created",
		"shard_id": 0,
	})
}

func (s *server) handleBulkDoc(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	table := sanitizeName(name)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	var docs []map[string]any
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			continue
		}
		docs = append(docs, doc)
	}

	failed := 0
	if len(docs) > 0 {
		if err := s.insertDocs(table, docs); err != nil {
			failed = len(docs)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"took":      0,
		"items":     len(docs),
		"succeeded": len(docs) - failed,
		"failed":    failed,
		"affected":  len(docs) - failed,
	})
}

func (s *server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SQL   string `json:"sql"`
		Index string `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	// Replace _docs with the actual table name
	index := req.Index
	if strings.Contains(index, ",") || strings.Contains(index, "*") {
		// Cross-index: pick the first concrete index for standalone
		parts := strings.Split(index, ",")
		index = strings.TrimSpace(parts[0])
		if strings.Contains(index, "*") {
			// Wildcard — just use the first table we can find
			index = strings.TrimSuffix(index, "*")
			index = strings.TrimSuffix(index, "-")
			// Try fanout-1 as a reasonable default for the stress test
			index = index + "-1"
		}
	}
	table := sanitizeName(index)
	sqlStr := strings.ReplaceAll(req.SQL, "_docs", fmt.Sprintf("%q", table))

	rows, err := s.db.Query(sqlStr)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": false,
			"error":   err.Error(),
			"rows":    []any{},
			"shard_id": 0,
		})
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		results = append(results, row)
	}
	if results == nil {
		results = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"columns":       cols,
		"rows":          results,
		"rows_affected": 0,
		"shard_id":      0,
	})
}

func (s *server) insertDoc(table string, doc map[string]any) error {
	_, err := s.db.Exec(
		fmt.Sprintf("INSERT INTO %q (id, value, category, message, ts) VALUES (?, ?, ?, ?, ?)", table),
		toInt64(doc["id"]), toInt64(doc["value"]),
		toString(doc["category"]), toString(doc["message"]), toString(doc["ts"]),
	)
	return err
}

func (s *server) insertDocs(table string, docs []map[string]any) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		fmt.Sprintf("INSERT INTO %q (id, value, category, message, ts) VALUES (?, ?, ?, ?, ?)", table),
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, doc := range docs {
		_, err := stmt.Exec(
			toInt64(doc["id"]), toInt64(doc["value"]),
			toString(doc["category"]), toString(doc["message"]), toString(doc["ts"]),
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sanitizeName(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(name, "'", ""), "\"", "")
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
