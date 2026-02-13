package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBulkIndexer(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		resp := QueryResponse{RowsAffected: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     10,
		FlushInterval: 100 * time.Millisecond,
		Workers:       2,
	})
	defer bi.Close(context.Background())

	// Add 5 items (should not trigger flush yet)
	for i := 0; i < 5; i++ {
		err := bi.Add(context.Background(), BulkItem{
			SQL:          "INSERT INTO users VALUES (1)",
			PartitionKey: "user-1",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	stats := bi.Stats()
	if stats.BufferedItems != 5 {
		t.Errorf("expected 5 buffered items, got %d", stats.BufferedItems)
	}

	// Flush manually
	if err := bi.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Wait for workers to complete
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&requestCount) != 5 {
		t.Errorf("expected 5 requests, got %d", requestCount)
	}

	stats = bi.Stats()
	if stats.BufferedItems != 0 {
		t.Errorf("expected 0 buffered items after flush, got %d", stats.BufferedItems)
	}
}

func TestBulkIndexerAutoFlush(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		resp := QueryResponse{RowsAffected: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     5,
		FlushInterval: 10 * time.Second, // High interval to test size-based flush
		Workers:       2,
	})
	defer bi.Close(context.Background())

	// Add exactly 5 items (should trigger auto-flush)
	for i := 0; i < 5; i++ {
		err := bi.Add(context.Background(), BulkItem{
			SQL:          "INSERT INTO users VALUES (1)",
			PartitionKey: "user-1",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Wait for workers to complete
	time.Sleep(100 * time.Millisecond)

	count := atomic.LoadInt32(&requestCount)
	if count != 5 {
		t.Errorf("expected 5 requests from auto-flush, got %d", count)
	}
}

func TestBulkIndexerPeriodicFlush(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		resp := QueryResponse{RowsAffected: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     100,                      // High size to test interval-based flush
		FlushInterval: 100 * time.Millisecond,
		Workers:       2,
	})
	defer bi.Close(context.Background())

	// Add 3 items
	for i := 0; i < 3; i++ {
		err := bi.Add(context.Background(), BulkItem{
			SQL:          "INSERT INTO users VALUES (1)",
			PartitionKey: "user-1",
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Wait for periodic flush
	time.Sleep(200 * time.Millisecond)

	count := atomic.LoadInt32(&requestCount)
	if count != 3 {
		t.Errorf("expected 3 requests from periodic flush, got %d", count)
	}
}

func TestBulkIndexerConcurrency(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		time.Sleep(10 * time.Millisecond) // Simulate work
		resp := QueryResponse{RowsAffected: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     50,
		FlushInterval: 1 * time.Second,
		Workers:       4,
	})

	// Add items concurrently
	var wg sync.WaitGroup
	itemCount := 100
	for i := 0; i < itemCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := bi.Add(context.Background(), BulkItem{
				SQL:          "INSERT INTO users VALUES (1)",
				PartitionKey: "user-1",
			})
			if err != nil {
				t.Errorf("add failed: %v", err)
			}
		}(i)
	}

	wg.Wait()

	// Close flushes remaining items
	if err := bi.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Wait for all workers to complete
	time.Sleep(200 * time.Millisecond)

	count := atomic.LoadInt32(&requestCount)
	if count != int32(itemCount) {
		t.Errorf("expected %d requests, got %d", itemCount, count)
	}
}

func TestBulkIndexerErrorHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500) // Simulate error
	}))
	defer server.Close()

	client := New(server.URL, WithRetries(0))

	var errorCount int32
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     5,
		FlushInterval: 1 * time.Second,
		Workers:       1,
		ErrorHandler: func(err error) {
			atomic.AddInt32(&errorCount, 1)
		},
	})
	defer bi.Close(context.Background())

	for i := 0; i < 3; i++ {
		bi.Add(context.Background(), BulkItem{
			SQL:          "INSERT INTO users VALUES (1)",
			PartitionKey: "user-1",
		})
	}

	bi.Flush(context.Background())
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&errorCount) != 3 {
		t.Errorf("expected 3 errors, got %d", errorCount)
	}
}

func TestBulkIndexerSuccessHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := QueryResponse{RowsAffected: 1, ShardID: 0}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)

	var successCount int32
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     5,
		FlushInterval: 1 * time.Second,
		Workers:       1,
		SuccessHandler: func(resp *QueryResponse) {
			atomic.AddInt32(&successCount, 1)
			if resp.RowsAffected != 1 {
				t.Errorf("expected 1 row affected, got %d", resp.RowsAffected)
			}
		},
	})
	defer bi.Close(context.Background())

	for i := 0; i < 3; i++ {
		bi.Add(context.Background(), BulkItem{
			SQL:          "INSERT INTO users VALUES (1)",
			PartitionKey: "user-1",
		})
	}

	bi.Flush(context.Background())
	time.Sleep(100 * time.Millisecond)

	if atomic.LoadInt32(&successCount) != 3 {
		t.Errorf("expected 3 successes, got %d", successCount)
	}
}

func TestBulkIndexerClose(t *testing.T) {
	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		resp := QueryResponse{RowsAffected: 1}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := New(server.URL)
	bi := client.NewBulkIndexer(BulkIndexerConfig{
		FlushSize:     100, // High size
		FlushInterval: 10 * time.Second,
		Workers:       2,
	})

	// Add items but don't flush
	for i := 0; i < 5; i++ {
		bi.Add(context.Background(), BulkItem{
			SQL:          "INSERT INTO users VALUES (1)",
			PartitionKey: "user-1",
		})
	}

	// Close should flush remaining items
	if err := bi.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	time.Sleep(100 * time.Millisecond)

	count := atomic.LoadInt32(&requestCount)
	if count != 5 {
		t.Errorf("expected 5 requests from close flush, got %d", count)
	}

	// Adding after close should fail
	err := bi.Add(context.Background(), BulkItem{
		SQL:          "INSERT INTO users VALUES (1)",
		PartitionKey: "user-1",
	})
	if err == nil {
		t.Error("expected error when adding to closed indexer")
	}
}
