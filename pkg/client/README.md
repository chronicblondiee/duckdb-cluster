# DuckDB Cluster Go Client

High-level Go client library for `duckdb-cluster` with connection pooling, automatic retries, and efficient bulk indexing.

## Features

- **Simple API** — Intuitive methods for queries, inserts, and health checks
- **Functional Options** — Flexible configuration using the options pattern
- **Connection Pooling** — Built-in HTTP connection reuse and pooling
- **Automatic Retries** — Exponential backoff for failed requests (except 4xx errors)
- **Bulk Indexer** — High-throughput async batch inserts with automatic flushing
- **Context Support** — Full context.Context support for cancellation and timeouts

## Installation

```bash
go get github.com/chronicblondiee/duckdb-cluster/pkg/client
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/chronicblondiee/duckdb-cluster/pkg/client"
)

func main() {
    // Create a client
    c := client.New("http://localhost:8080")
    
    ctx := context.Background()
    
    // Create a table (DDL broadcasts to all shards)
    _, err := c.Query(ctx, 
        "CREATE TABLE users (id INTEGER, name VARCHAR)", 
        "", // no partition key for DDL
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // Insert data (requires partition key)
    _, err = c.Insert(ctx,
        "INSERT INTO users VALUES (1, 'Alice')",
        "user-1", // partition key determines target shard
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // Query data (fans out to all shards)
    resp, err := c.Select(ctx, "SELECT * FROM users")
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Columns: %v\n", resp.Columns)
    for _, row := range resp.Rows {
        fmt.Printf("Row: %v\n", row)
    }
}
```

## Configuration

Use functional options to customize the client:

```go
c := client.New("http://localhost:8080",
    client.WithRetries(5),              // Retry failed requests up to 5 times
    client.WithTimeout(30*time.Second), // Set request timeout to 30s
    client.WithHTTPClient(customClient), // Use a custom http.Client
)
```

### Available Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithRetries(n)` | 3 | Number of retry attempts for failed requests |
| `WithTimeout(d)` | 60s | Request timeout duration |
| `WithHTTPClient(c)` | (default) | Custom `http.Client` with connection pooling |

## API Methods

### Query

Execute any SQL query. For write queries (INSERT/UPDATE/DELETE), `partitionKey` is required.

```go
resp, err := c.Query(ctx, sql, partitionKey)
```

### Select

Convenience method for SELECT queries (no partition key needed).

```go
resp, err := c.Select(ctx, "SELECT * FROM users WHERE age > 18")
```

### Insert

Convenience method for INSERT queries. Requires a partition key.

```go
resp, err := c.Insert(ctx, 
    "INSERT INTO users VALUES (1, 'Bob')", 
    "user-1",
)
```

### Health

Check cluster health.

```go
health, err := c.Health(ctx)
fmt.Printf("Status: %s, Shards: %d\n", health.Status, health.ShardCount)
```

## Bulk Indexer

For high-throughput batch inserts, use the `BulkIndexer`:

```go
// Create bulk indexer
bi := c.NewBulkIndexer(client.BulkIndexerConfig{
    FlushSize:     1000,            // Flush every 1000 items
    FlushInterval: 5 * time.Second, // Or every 5 seconds
    Workers:       4,                // 4 concurrent workers
    
    SuccessHandler: func(resp *client.QueryResponse) {
        fmt.Printf("Inserted: shard %d, %d rows\n", 
            resp.ShardID, resp.RowsAffected)
    },
    
    ErrorHandler: func(err error) {
        log.Printf("Error: %v", err)
    },
})
defer bi.Close(ctx) // Flushes remaining items

// Add items
for i := 0; i < 10000; i++ {
    err := bi.Add(ctx, client.BulkItem{
        SQL:          fmt.Sprintf("INSERT INTO events VALUES (%d, 'event')", i),
        PartitionKey: fmt.Sprintf("event-%d", i),
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

### BulkIndexer Configuration

| Option | Default | Description |
|--------|---------|-------------|
| `FlushSize` | 1000 | Number of items to buffer before auto-flush |
| `FlushInterval` | 5s | Maximum time between flushes |
| `Workers` | 4 | Number of concurrent workers for processing |
| `ErrorHandler` | nil | Callback for failed items |
| `SuccessHandler` | nil | Callback for successful items |

### How It Works

1. Items are buffered in memory
2. Auto-flush triggers when buffer reaches `FlushSize` OR `FlushInterval` elapses
3. Worker goroutines process items concurrently
4. Errors and successes are reported via handlers (if configured)
5. `Close()` flushes all remaining items

## Error Handling

### HTTP Errors

```go
resp, err := c.Select(ctx, "INVALID SQL")
if httpErr, ok := err.(*client.HTTPError); ok {
    fmt.Printf("HTTP %d: %s\n", httpErr.StatusCode, httpErr.Message)
}
```

### Retry Logic

- **Server errors (5xx)**: Automatically retried with exponential backoff
- **Client errors (4xx)**: NOT retried (indicates bad request)
- **Network errors**: Retried up to the configured retry count
- **Context cancellation**: Immediately stops retrying

Retry backoff: 100ms, 200ms, 400ms, 800ms, ...

## Examples

See the [`examples/`](examples/) directory for complete examples:

- [`basic/`](examples/basic/) — Simple queries and inserts
- [`bulk/`](examples/bulk/) — High-throughput bulk inserts

### Running Examples

```bash
# Start the cluster
./duckdb-cluster init --shards=3
./duckdb-cluster start

# Run basic example
cd pkg/client/examples/basic
go run main.go

# Run bulk indexer example
cd ../bulk
go run main.go
```

## Performance Tips

1. **Use BulkIndexer for batch inserts** — 10-100x faster than individual inserts
2. **Tune flush settings** — Larger `FlushSize` = better throughput, but higher memory
3. **Adjust worker count** — Match your CPU cores and cluster capacity
4. **Reuse client instances** — Connection pooling is most effective with a single client
5. **Use appropriate partition keys** — Distribute writes evenly across shards

## Thread Safety

- **Client**: Safe for concurrent use across goroutines
- **BulkIndexer**: Safe for concurrent `Add()` calls
- **HTTP Connection Pool**: Automatically managed by `http.Client`

## License

Same as parent project.
