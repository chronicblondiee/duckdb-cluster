package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/querier"
	pb "github.com/chronicblondiee/duckdb-cluster/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// QuerierClient wraps a gRPC client for remote querier calls
type QuerierClient struct {
	conn   *grpc.ClientConn
	client pb.QuerierServiceClient
	addr   string
}

// NewQuerierClient creates a new gRPC client for a querier
func NewQuerierClient(addr string) (*QuerierClient, error) {
	slog.Debug("connecting to remote querier", "addr", addr)
	
	// Create gRPC connection
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to querier at %s: %w", addr, err)
	}
	
	return &QuerierClient{
		conn:   conn,
		client: pb.NewQuerierServiceClient(conn),
		addr:   addr,
	}, nil
}

// Query sends a query to the remote querier
func (c *QuerierClient) Query(ctx context.Context, req *querier.QueryRequest) (*querier.QueryResponse, error) {
	slog.Debug("grpc client querying", "addr", c.addr, "sql", req.SQL)
	
	// Convert internal request to gRPC request
	shardIDs := make([]int32, len(req.ShardIDs))
	for i, id := range req.ShardIDs {
		shardIDs[i] = int32(id)
	}
	
	pbReq := &pb.QueryRequest{
		Sql:      req.SQL,
		ShardIds: shardIDs,
	}
	
	// Call remote querier
	pbResp, err := c.client.Query(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("grpc query failed: %w", err)
	}
	
	// Convert gRPC response to internal response
	rows := make([]map[string]any, len(pbResp.Rows))
	for i, pbRow := range pbResp.Rows {
		row := make(map[string]any)
		for k, jsonStr := range pbRow.Values {
			// Unmarshal JSON string back to value
			var value any
			if err := json.Unmarshal([]byte(jsonStr), &value); err != nil {
				return &querier.QueryResponse{
					Error: fmt.Sprintf("failed to unmarshal row value: %v", err),
				}, nil
			}
			row[k] = value
		}
		rows[i] = row
	}
	
	return &querier.QueryResponse{
		Columns: pbResp.Columns,
		Rows:    rows,
		Error:   pbResp.Error,
	}, nil
}

// Health checks the health of the remote querier
func (c *QuerierClient) Health(ctx context.Context) error {
	resp, err := c.client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	
	if resp.Status != "healthy" {
		return fmt.Errorf("querier unhealthy: %s", resp.Error)
	}
	
	return nil
}

// Close closes the gRPC connection
func (c *QuerierClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
