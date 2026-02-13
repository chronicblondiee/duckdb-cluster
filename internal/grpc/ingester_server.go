package grpc

import (
	"context"
	"log/slog"

	"github.com/brown/duckdb-cluster/internal/distributor"
	"github.com/brown/duckdb-cluster/internal/ingester"
	pb "github.com/brown/duckdb-cluster/proto"
	"google.golang.org/grpc"
)

// IngesterServer implements the gRPC IngesterService
type IngesterServer struct {
	pb.UnimplementedIngesterServiceServer
	ingester *ingester.Ingester
}

// NewIngesterServer creates a new gRPC server for the ingester
func NewIngesterServer(ing *ingester.Ingester) *IngesterServer {
	return &IngesterServer{
		ingester: ing,
	}
}

// Push handles a write request over gRPC
func (s *IngesterServer) Push(ctx context.Context, req *pb.PushRequest) (*pb.PushResponse, error) {
	slog.Debug("grpc ingester received push", "sql", req.Sql, "partition_key", req.PartitionKey)
	
	// Convert gRPC request to internal request
	internalReq := &distributor.PushRequest{
		SQL:          req.Sql,
		PartitionKey: req.PartitionKey,
		TenantID:     req.TenantId,
	}
	
	// Call internal ingester
	resp, err := s.ingester.Push(ctx, internalReq)
	if err != nil {
		return &pb.PushResponse{
			Error: err.Error(),
		}, nil
	}
	
	// Convert internal response to gRPC response
	return &pb.PushResponse{
		RowsAffected: resp.RowsAffected,
		ShardId:      int32(resp.ShardID),
		Error:        resp.Error,
	}, nil
}

// Health returns the health status of the ingester
func (s *IngesterServer) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	manager := s.ingester.GetManager()
	if manager == nil {
		return &pb.HealthResponse{
			Status: "unhealthy",
			Error:  "ingester not initialized",
		}, nil
	}
	
	return &pb.HealthResponse{
		Status:     "healthy",
		ShardCount: int32(manager.ShardCount()),
	}, nil
}

// RegisterIngesterServer registers the ingester gRPC server
func RegisterIngesterServer(grpcServer *grpc.Server, ing *ingester.Ingester) {
	pb.RegisterIngesterServiceServer(grpcServer, NewIngesterServer(ing))
}
