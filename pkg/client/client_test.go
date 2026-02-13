package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	c := New("http://localhost:8080")
	if c.baseURL != "http://localhost:8080" {
		t.Errorf("expected baseURL http://localhost:8080, got %s", c.baseURL)
	}
	if c.retries != 3 {
		t.Errorf("expected 3 retries, got %d", c.retries)
	}
	if c.timeout != 60*time.Second {
		t.Errorf("expected 60s timeout, got %v", c.timeout)
	}
}

func TestWithOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 10 * time.Second}

	c := New("http://localhost:8080",
		WithHTTPClient(customClient),
		WithRetries(5),
		WithTimeout(30*time.Second),
	)

	if c.httpClient != customClient {
		t.Error("expected custom HTTP client")
	}
	if c.retries != 5 {
		t.Errorf("expected 5 retries, got %d", c.retries)
	}
	if c.timeout != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", c.timeout)
	}
}

func TestQuery(t *testing.T) {
	tests := []struct {
		name         string
		sql          string
		partitionKey string
		response     QueryResponse
		statusCode   int
		wantErr      bool
	}{
		{
			name:         "select query",
			sql:          "SELECT * FROM users",
			partitionKey: "",
			response: QueryResponse{
				Columns: []string{"id", "name"},
				Rows:    [][]interface{}{{1.0, "Alice"}, {2.0, "Bob"}},
			},
			statusCode: 200,
		},
		{
			name:         "insert query",
			sql:          "INSERT INTO users VALUES (1, 'Alice')",
			partitionKey: "user-1",
			response: QueryResponse{
				RowsAffected: 1,
				ShardID:      0,
			},
			statusCode: 200,
		},
		{
			name:         "error response",
			sql:          "INVALID SQL",
			partitionKey: "",
			statusCode:   400,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" || r.URL.Path != "/query" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}

				var req QueryRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatal(err)
				}

				if req.SQL != tt.sql {
					t.Errorf("expected sql %s, got %s", tt.sql, req.SQL)
				}
				if req.PartitionKey != tt.partitionKey {
					t.Errorf("expected partition_key %s, got %s", tt.partitionKey, req.PartitionKey)
				}

				w.WriteHeader(tt.statusCode)
				if tt.statusCode == 200 {
					json.NewEncoder(w).Encode(tt.response)
				} else {
					w.Write([]byte("error message"))
				}
			}))
			defer server.Close()

			client := New(server.URL, WithRetries(0)) // No retries for tests
			resp, err := client.Query(context.Background(), tt.sql, tt.partitionKey)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tt.response.Columns) > 0 {
				if len(resp.Columns) != len(tt.response.Columns) {
					t.Errorf("expected %d columns, got %d", len(tt.response.Columns), len(resp.Columns))
				}
			}

			if tt.response.RowsAffected > 0 {
				if resp.RowsAffected != tt.response.RowsAffected {
					t.Errorf("expected %d rows affected, got %d", tt.response.RowsAffected, resp.RowsAffected)
				}
			}
		})
	}
}

func TestSelect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req QueryRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.PartitionKey != "" {
			t.Error("SELECT should not have partition_key")
		}

		resp := QueryResponse{
			Columns: []string{"count"},
			Rows:    [][]interface{}{{42.0}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	resp, err := client.Select(context.Background(), "SELECT COUNT(*) FROM users")
	if err != nil {
		t.Fatal(err)
	}

	if len(resp.Columns) != 1 || resp.Columns[0] != "count" {
		t.Errorf("unexpected columns: %v", resp.Columns)
	}
}

func TestInsert(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req QueryRequest
		json.NewDecoder(r.Body).Decode(&req)

		if req.PartitionKey != "user-123" {
			t.Errorf("expected partition_key user-123, got %s", req.PartitionKey)
		}

		resp := QueryResponse{RowsAffected: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)

	// Test missing partition key
	_, err := client.Insert(context.Background(), "INSERT INTO users VALUES (1)", "")
	if err == nil {
		t.Error("expected error for missing partition_key")
	}

	// Test with partition key
	resp, err := client.Insert(context.Background(), "INSERT INTO users VALUES (1)", "user-123")
	if err != nil {
		t.Fatal(err)
	}

	if resp.RowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", resp.RowsAffected)
	}
}

func TestHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/health" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		resp := HealthResponse{
			Status:     "healthy",
			ShardCount: 3,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	resp, err := client.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if resp.Status != "healthy" {
		t.Errorf("expected status healthy, got %s", resp.Status)
	}
	if resp.ShardCount != 3 {
		t.Errorf("expected 3 shards, got %d", resp.ShardCount)
	}
}

func TestRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(503) // Service unavailable
			return
		}
		resp := QueryResponse{Columns: []string{"id"}, Rows: [][]interface{}{{1.0}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL, WithRetries(3))
	_, err := client.Select(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}

	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryClientError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(400) // Bad request - should not retry
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	client := New(server.URL, WithRetries(3))
	_, err := client.Select(context.Background(), "INVALID")

	if err == nil {
		t.Error("expected error")
	}

	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry on 4xx), got %d", attempts)
	}
}

func TestContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	client := New(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.Select(ctx, "SELECT 1")
	if err == nil {
		t.Error("expected context cancellation error")
	}
}
