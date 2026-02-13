package main

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/brown/duckdb-cluster/pkg/client"
)

func main() {
	// Create a client
	c := client.New("http://localhost:8080")

	ctx := context.Background()

	// Create table
	_, err := c.Query(ctx, "CREATE TABLE events (id INTEGER, timestamp TIMESTAMP, event_type VARCHAR, user_id VARCHAR)", "")
	if err != nil {
		log.Fatalf("create table failed: %v", err)
	}
	fmt.Println("Table created")

	// Track statistics
	var successCount, errorCount int64

	// Create bulk indexer
	bi := c.NewBulkIndexer(client.BulkIndexerConfig{
		FlushSize:     1000,               // Flush every 1000 items
		FlushInterval: 5 * time.Second,    // Or every 5 seconds
		Workers:       4,                  // 4 concurrent workers
		SuccessHandler: func(resp *client.QueryResponse) {
			atomic.AddInt64(&successCount, 1)
		},
		ErrorHandler: func(err error) {
			atomic.AddInt64(&errorCount, 1)
			log.Printf("bulk insert error: %v", err)
		},
	})
	defer bi.Close(ctx)

	// Generate and insert 10,000 events
	start := time.Now()
	eventCount := 10000

	fmt.Printf("Inserting %d events...\n", eventCount)

	for i := 0; i < eventCount; i++ {
		sql := fmt.Sprintf(
			"INSERT INTO events VALUES (%d, TIMESTAMP '%s', '%s', 'user-%d')",
			i,
			time.Now().Format("2006-01-02 15:04:05"),
			getEventType(i),
			i%100, // 100 different users
		)

		err := bi.Add(ctx, client.BulkItem{
			SQL:          sql,
			PartitionKey: fmt.Sprintf("user-%d", i%100),
		})
		if err != nil {
			log.Printf("add failed: %v", err)
		}

		// Print progress
		if (i+1)%1000 == 0 {
			fmt.Printf("  Added %d events...\n", i+1)
		}
	}

	// Wait for all items to be flushed
	fmt.Println("Flushing remaining items...")
	if err := bi.Close(ctx); err != nil {
		log.Fatalf("close failed: %v", err)
	}

	elapsed := time.Since(start)

	// Print statistics
	fmt.Printf("\nBulk insert complete!\n")
	fmt.Printf("  Total events: %d\n", eventCount)
	fmt.Printf("  Successful: %d\n", successCount)
	fmt.Printf("  Failed: %d\n", errorCount)
	fmt.Printf("  Duration: %s\n", elapsed)
	fmt.Printf("  Throughput: %.0f events/sec\n", float64(eventCount)/elapsed.Seconds())

	// Verify data
	time.Sleep(100 * time.Millisecond) // Give it a moment
	countResp, err := c.Select(ctx, "SELECT COUNT(*) FROM events")
	if err != nil {
		log.Fatalf("count failed: %v", err)
	}
	fmt.Printf("\nVerification: %v events in database\n", countResp.Rows[0][0])

	// Query by event type
	typeResp, err := c.Select(ctx, "SELECT event_type, COUNT(*) as count FROM events GROUP BY event_type ORDER BY count DESC")
	if err != nil {
		log.Fatalf("group by failed: %v", err)
	}

	fmt.Println("\nEvents by type:")
	for _, row := range typeResp.Rows {
		fmt.Printf("  %s: %v\n", row[0], row[1])
	}
}

func getEventType(i int) string {
	types := []string{"login", "logout", "page_view", "click", "purchase"}
	return types[i%len(types)]
}
