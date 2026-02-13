package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// Client is a high-level client for duckdb-cluster.
type Client struct {
	baseURL    string
	httpClient *http.Client
	retries    int
	timeout    time.Duration
}

// Option is a functional option for configuring the Client.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

// WithRetries sets the number of retry attempts for failed requests.
// Default is 3.
func WithRetries(retries int) Option {
	return func(c *Client) {
		c.retries = retries
	}
}

// WithTimeout sets the request timeout.
// Default is 60 seconds.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// New creates a new Client with the given base URL and options.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		retries: 3,
		timeout: 60 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	// Apply timeout to HTTP client if not already set
	if c.httpClient.Timeout == 0 {
		c.httpClient.Timeout = c.timeout
	}

	return c
}

// QueryRequest represents a query request.
type QueryRequest struct {
	SQL          string `json:"sql"`
	PartitionKey string `json:"partition_key,omitempty"`
}

// QueryResponse represents a query response.
type QueryResponse struct {
	Columns      []string        `json:"columns,omitempty"`
	Rows         [][]interface{} `json:"rows,omitempty"`
	RowsAffected int64           `json:"rows_affected,omitempty"`
	ShardID      int             `json:"shard_id,omitempty"`
}

// HealthResponse represents a health check response.
type HealthResponse struct {
	Status     string `json:"status"`
	ShardCount int    `json:"shard_count"`
}

// Query executes a SQL query against the cluster.
// For write queries (INSERT/UPDATE/DELETE), partitionKey must be provided.
func (c *Client) Query(ctx context.Context, sql string, partitionKey string) (*QueryResponse, error) {
	req := QueryRequest{
		SQL:          sql,
		PartitionKey: partitionKey,
	}

	var resp QueryResponse
	if err := c.doWithRetry(ctx, "POST", "/query", req, &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// Select executes a SELECT query and returns the results.
func (c *Client) Select(ctx context.Context, sql string) (*QueryResponse, error) {
	return c.Query(ctx, sql, "")
}

// Insert executes an INSERT query with the given partition key.
func (c *Client) Insert(ctx context.Context, sql string, partitionKey string) (*QueryResponse, error) {
	if partitionKey == "" {
		return nil, fmt.Errorf("partition_key is required for INSERT")
	}
	return c.Query(ctx, sql, partitionKey)
}

// Health checks the cluster health.
func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	var resp HealthResponse
	if err := c.doWithRetry(ctx, "GET", "/health", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// doWithRetry executes an HTTP request with exponential backoff retry logic.
func (c *Client) doWithRetry(ctx context.Context, method, path string, reqBody, respBody interface{}) error {
	var lastErr error

	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 100ms, 200ms, 400ms, ...
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * 100 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		err := c.do(ctx, method, path, reqBody, respBody)
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry on client errors (4xx)
		if httpErr, ok := err.(*HTTPError); ok {
			if httpErr.StatusCode >= 400 && httpErr.StatusCode < 500 {
				return err
			}
		}

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

// do executes a single HTTP request.
func (c *Client) do(ctx context.Context, method, path string, reqBody, respBody interface{}) error {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	if respBody != nil {
		if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}

	return nil
}

// HTTPError represents an HTTP error response.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}
