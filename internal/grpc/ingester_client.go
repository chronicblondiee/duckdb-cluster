package grpc

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/distributor"
	pb "github.com/chronicblondiee/duckdb-cluster/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// IngesterClient wraps a gRPC client for remote ingester calls
type IngesterClient struct {
	conn   *grpc.ClientConn
	client pb.IngesterServiceClient
	addr   string
}

// NewIngesterClient creates a new gRPC client for an ingester
func NewIngesterClient(addr string) (*IngesterClient, error) {
	slog.Debug("connecting to remote ingester", "addr", addr)
	
	// Create gRPC connection
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ingester at %s: %w", addr, err)
	}
	
	return &IngesterClient{
		conn:   conn,
		client: pb.NewIngesterServiceClient(conn),
		addr:   addr,
	}, nil
}

// Push sends a write request to the remote ingester
func (c *IngesterClient) Push(ctx context.Context, req *distributor.PushRequest) (*distributor.PushResponse, error) {
	slog.Debug("grpc client pushing to ingester", "addr", c.addr, "sql", req.SQL)
	
	// Convert internal request to gRPC request
	pbReq := &pb.PushRequest{
		Sql:          req.SQL,
		PartitionKey: req.PartitionKey,
		TenantId:     req.TenantID,
	}
	
	// Call remote ingester
	pbResp, err := c.client.Push(ctx, pbReq)
	if err != nil {
		return nil, fmt.Errorf("grpc push failed: %w", err)
	}
	
	// Convert gRPC response to internal response
	return &distributor.PushResponse{
		RowsAffected: pbResp.RowsAffected,
		ShardID:      int(pbResp.ShardId),
		Error:        pbResp.Error,
	}, nil
}

// Health checks the health of the remote ingester
func (c *IngesterClient) Health(ctx context.Context) error {
	resp, err := c.client.Health(ctx, &pb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	
	if resp.Status != "healthy" {
		return fmt.Errorf("ingester unhealthy: %s", resp.Error)
	}
	
	return nil
}

// Close closes the gRPC connection
func (c *IngesterClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
