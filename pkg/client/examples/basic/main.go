package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/pkg/client"
)

func main() {
	// Create a client
	c := client.New("http://localhost:8080",
		client.WithRetries(3),
		client.WithTimeout(30*time.Second),
	)

	ctx := context.Background()

	// Check cluster health
	health, err := c.Health(ctx)
	if err != nil {
		log.Fatalf("health check failed: %v", err)
	}
	fmt.Printf("Cluster status: %s, shards: %d\n", health.Status, health.ShardCount)

	// Execute DDL (broadcast to all shards)
	_, err = c.Query(ctx, "CREATE TABLE users (id INTEGER, name VARCHAR, email VARCHAR)", "")
	if err != nil {
		log.Fatalf("create table failed: %v", err)
	}
	fmt.Println("Table created")

	// Insert data with partition key
	_, err = c.Insert(ctx,
		"INSERT INTO users VALUES (1, 'Alice', 'alice@example.com')",
		"user-1", // partition key
	)
	if err != nil {
		log.Fatalf("insert failed: %v", err)
	}
	fmt.Println("Row inserted")

	// Insert more users
	users := []struct {
		id    int
		name  string
		email string
		key   string
	}{
		{2, "Bob", "bob@example.com", "user-2"},
		{3, "Charlie", "charlie@example.com", "user-3"},
		{4, "Diana", "diana@example.com", "user-4"},
	}

	for _, u := range users {
		sql := fmt.Sprintf("INSERT INTO users VALUES (%d, '%s', '%s')", u.id, u.name, u.email)
		_, err = c.Insert(ctx, sql, u.key)
		if err != nil {
			log.Printf("insert failed for %s: %v", u.name, err)
		}
	}

	// Query data (fan-out to all shards)
	resp, err := c.Select(ctx, "SELECT * FROM users ORDER BY id")
	if err != nil {
		log.Fatalf("select failed: %v", err)
	}

	fmt.Printf("\nQuery results (%d rows):\n", len(resp.Rows))
	fmt.Printf("Columns: %v\n", resp.Columns)
	for _, row := range resp.Rows {
		fmt.Printf("  %v\n", row)
	}

	// Aggregate query
	countResp, err := c.Select(ctx, "SELECT COUNT(*) as total FROM users")
	if err != nil {
		log.Fatalf("count failed: %v", err)
	}
	fmt.Printf("\nTotal users: %v\n", countResp.Rows[0][0])
}
