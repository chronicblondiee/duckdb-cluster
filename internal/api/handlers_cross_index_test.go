package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCrossIndexQueryCommaSeparated(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create two indices with tables
	for _, name := range []string{"idx-a", "idx-b"} {
		req := httptest.NewRequest("PUT", "/indices/"+name, bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d: %s", name, w.Code, w.Body.String())
		}

		// Create table and insert data
		body := `{"sql": "CREATE TABLE items (id INTEGER, source VARCHAR)", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("create table on %s: %d: %s", name, w.Code, w.Body.String())
		}

		body = `{"sql": "INSERT INTO items VALUES (1, '` + name + `')", "partition_key": "1", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("insert on %s: %d: %s", name, w.Code, w.Body.String())
		}
	}

	// Cross-index query
	body := `{"sql": "SELECT * FROM items", "index": "idx-a,idx-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("cross-index query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows from cross-index query, got %d", len(resp.Rows))
	}
	if resp.ShardID != -1 {
		t.Errorf("expected shard_id -1 for cross-index, got %d", resp.ShardID)
	}
}

func TestCrossIndexQueryWildcard(t *testing.T) {
	srv := setupServerWithInit(t)

	// Create indices matching a pattern
	for _, name := range []string{"logs-a", "logs-b"} {
		req := httptest.NewRequest("PUT", "/indices/"+name, bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d", name, w.Code)
		}

		body := `{"sql": "CREATE TABLE data (id INTEGER)", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)

		body = `{"sql": "INSERT INTO data VALUES (1)", "partition_key": "1", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
	}

	// Wildcard query
	body := `{"sql": "SELECT * FROM data", "index": "logs-*"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("wildcard query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 2 {
		t.Errorf("expected 2 rows from wildcard query, got %d", len(resp.Rows))
	}
}

// createIndexWithData creates an index, a table, and inserts rows.
// vals is a list of SQL value tuples like "(1, 10)" to insert.
func createIndexWithData(t *testing.T, srv *Server, name, createTable string, vals []string) {
	t.Helper()
	req := httptest.NewRequest("PUT", "/indices/"+name, bytes.NewBufferString(`{"settings":{"shard_count":1}}`))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create index %s: %d: %s", name, w.Code, w.Body.String())
	}

	body := `{"sql": "` + createTable + `", "index": "` + name + `"}`
	req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create table on %s: %d: %s", name, w.Code, w.Body.String())
	}

	for i, v := range vals {
		pk := fmt.Sprintf("%d", i)
		body = `{"sql": "INSERT INTO items VALUES ` + v + `", "partition_key": "` + pk + `", "index": "` + name + `"}`
		req = httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
		w = httptest.NewRecorder()
		srv.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("insert on %s: %d: %s", name, w.Code, w.Body.String())
		}
	}
}

func TestCrossIndexAggregationCount(t *testing.T) {
	srv := setupServerWithInit(t)

	createIndexWithData(t, srv, "cnt-a", "CREATE TABLE items (id INTEGER)", []string{"(1)", "(2)", "(3)"})
	createIndexWithData(t, srv, "cnt-b", "CREATE TABLE items (id INTEGER)", []string{"(4)", "(5)"})

	body := `{"sql": "SELECT COUNT(*) AS total FROM items", "index": "cnt-a,cnt-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("count query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Rows))
	}
	total, ok := resp.Rows[0]["total"].(float64)
	if !ok {
		t.Fatalf("expected numeric total, got %T: %v", resp.Rows[0]["total"], resp.Rows[0]["total"])
	}
	if total != 5 {
		t.Errorf("expected COUNT=5, got %v", total)
	}
}

func TestCrossIndexAggregationSum(t *testing.T) {
	srv := setupServerWithInit(t)

	createIndexWithData(t, srv, "sum-a", "CREATE TABLE items (id INTEGER, amount INTEGER)", []string{"(1, 10)", "(2, 20)"})
	createIndexWithData(t, srv, "sum-b", "CREATE TABLE items (id INTEGER, amount INTEGER)", []string{"(3, 30)"})

	body := `{"sql": "SELECT SUM(amount) AS total FROM items", "index": "sum-a,sum-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("sum query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Rows))
	}
	total, _ := resp.Rows[0]["total"].(float64)
	if total != 60 {
		t.Errorf("expected SUM=60, got %v", total)
	}
}

func TestCrossIndexAggregationAvg(t *testing.T) {
	srv := setupServerWithInit(t)

	// idx-a has [10, 20] (avg=15), idx-b has [30] (avg=30)
	// correct global avg = (10+20+30)/3 = 20, not (15+30)/2 = 22.5
	createIndexWithData(t, srv, "avg-a", "CREATE TABLE items (id INTEGER, val INTEGER)", []string{"(1, 10)", "(2, 20)"})
	createIndexWithData(t, srv, "avg-b", "CREATE TABLE items (id INTEGER, val INTEGER)", []string{"(3, 30)"})

	body := `{"sql": "SELECT AVG(val) AS avg_val FROM items", "index": "avg-a,avg-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("avg query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(resp.Rows))
	}
	avg, _ := resp.Rows[0]["avg_val"].(float64)
	if avg != 20 {
		t.Errorf("expected AVG=20, got %v", avg)
	}
}

func TestCrossIndexAggregationGroupBy(t *testing.T) {
	srv := setupServerWithInit(t)

	createIndexWithData(t, srv, "grp-a", "CREATE TABLE items (id INTEGER, category VARCHAR)", []string{"(1, 'electronics')", "(2, 'books')"})
	createIndexWithData(t, srv, "grp-b", "CREATE TABLE items (id INTEGER, category VARCHAR)", []string{"(3, 'electronics')", "(4, 'electronics')"})

	body := `{"sql": "SELECT category, COUNT(*) AS cnt FROM items GROUP BY category ORDER BY category", "index": "grp-a,grp-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("group by query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("expected 2 groups, got %d: %v", len(resp.Rows), resp.Rows)
	}
	// Ordered by category: books=1, electronics=3
	if resp.Rows[0]["category"] != "books" {
		t.Errorf("expected first group 'books', got %v", resp.Rows[0]["category"])
	}
	booksCnt, _ := resp.Rows[0]["cnt"].(float64)
	if booksCnt != 1 {
		t.Errorf("expected books count=1, got %v", booksCnt)
	}
	elecCnt, _ := resp.Rows[1]["cnt"].(float64)
	if elecCnt != 3 {
		t.Errorf("expected electronics count=3, got %v", elecCnt)
	}
}

func TestCrossIndexAggregationOrderByLimit(t *testing.T) {
	srv := setupServerWithInit(t)

	createIndexWithData(t, srv, "olim-a", "CREATE TABLE items (id INTEGER, name VARCHAR)", []string{"(5, 'e')", "(1, 'a')", "(3, 'c')"})
	createIndexWithData(t, srv, "olim-b", "CREATE TABLE items (id INTEGER, name VARCHAR)", []string{"(4, 'd')", "(2, 'b')"})

	body := `{"sql": "SELECT * FROM items ORDER BY id LIMIT 3", "index": "olim-a,olim-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	var resp queryResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Fatalf("order+limit query failed: %s", resp.Error)
	}
	if len(resp.Rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(resp.Rows))
	}
	// Should be ids 1, 2, 3 in order
	for i, expectedID := range []float64{1, 2, 3} {
		id, _ := resp.Rows[i]["id"].(float64)
		if id != expectedID {
			t.Errorf("row %d: expected id=%v, got %v", i, expectedID, id)
		}
	}
}

func TestCrossIndexWriteRejected(t *testing.T) {
	srv := setupServerWithInit(t)

	body := `{"sql": "INSERT INTO t VALUES (1)", "index": "idx-a,idx-b"}`
	req := httptest.NewRequest("POST", "/query", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-index write, got %d", w.Code)
	}
}
