package grpc

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/chronicblondiee/duckdb-cluster/internal/querier"
	pb "github.com/chronicblondiee/duckdb-cluster/proto"
	"google.golang.org/grpc"
)

// QuerierServer implements the gRPC QuerierService
type QuerierServer struct {
	pb.UnimplementedQuerierServiceServer
	querier *querier.Querier
}

// NewQuerierServer creates a new gRPC server for the querier
func NewQuerierServer(q *querier.Querier) *QuerierServer {
	return &QuerierServer{
		querier: q,
	}
}

// Query executes a read query over gRPC
func (s *QuerierServer) Query(ctx context.Context, req *pb.QueryRequest) (*pb.QueryResponse, error) {
	slog.Debug("grpc querier received query", "sql", req.Sql, "shard_ids", req.ShardIds)
	
	// Convert gRPC request to internal request
	shardIDs := make([]int, len(req.ShardIds))
	for i, id := range req.ShardIds {
		shardIDs[i] = int(id)
	}
	
	internalReq := &querier.QueryRequest{
		SQL:      req.Sql,
		ShardIDs: shardIDs,
	}
	
	// Call internal querier
	resp, err := s.querier.Query(ctx, internalReq)
	if err != nil {
		return &pb.QueryResponse{
			Error: err.Error(),
		}, nil
	}
	
	// Convert internal response to gRPC response
	pbRows := make([]*pb.Row, len(resp.Rows))
	for i, row := range resp.Rows {
		values := make(map[string]string)
		for k, v := range row {
			// Convert value to JSON string
			jsonBytes, err := json.Marshal(v)
			if err != nil {
				return &pb.QueryResponse{
					Error: "failed to marshal row value: " + err.Error(),
				}, nil
			}
			values[k] = string(jsonBytes)
		}
		pbRows[i] = &pb.Row{Values: values}
	}
	
	return &pb.QueryResponse{
		Columns: resp.Columns,
		Rows:    pbRows,
		Error:   resp.Error,
	}, nil
}

// Health returns the health status of the querier
func (s *QuerierServer) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	router := s.querier.GetRouter()
	if router == nil {
		return &pb.HealthResponse{
			Status: "unhealthy",
			Error:  "querier not initialized",
		}, nil
	}
	
	// Get shard count from router's manager
	manager := router.Manager
	if manager == nil {
		return &pb.HealthResponse{
			Status: "unhealthy",
			Error:  "shard manager not available",
		}, nil
	}
	
	return &pb.HealthResponse{
		Status:     "healthy",
		ShardCount: int32(manager.ShardCount()),
	}, nil
}

// RegisterQuerierServer registers the querier gRPC server
func RegisterQuerierServer(grpcServer *grpc.Server, q *querier.Querier) {
	pb.RegisterQuerierServiceServer(grpcServer, NewQuerierServer(q))
}
