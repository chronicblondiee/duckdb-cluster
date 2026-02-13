package observability

import (
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

var (
	testMetrics     *Metrics
	testMetricsOnce sync.Once
)

// getTestMetrics returns a singleton metrics instance for testing
func getTestMetrics() *Metrics {
	testMetricsOnce.Do(func() {
		testMetrics = NewMetrics("test")
	})
	return testMetrics
}

func TestNewMetrics(t *testing.T) {
	// Test with default namespace
	m := getTestMetrics()
	if m == nil {
		t.Fatal("expected metrics to be non-nil")
	}

	// Verify metrics are registered
	if m.WriteLatency == nil {
		t.Error("WriteLatency should not be nil")
	}
	if m.WriteTotal == nil {
		t.Error("WriteTotal should not be nil")
	}
	if m.QueryLatency == nil {
		t.Error("QueryLatency should not be nil")
	}
	if m.ReplicationSuccess == nil {
		t.Error("ReplicationSuccess should not be nil")
	}
}

func TestMetricsRecording(t *testing.T) {
	metrics := getTestMetrics()

	// Record a write operation
	metrics.WriteTotal.WithLabelValues("push", "success").Inc()
	metrics.WriteLatency.WithLabelValues("push", "0").Observe(0.05)

	// Verify counters
	count := testutil.ToFloat64(metrics.WriteTotal.WithLabelValues("push", "success"))
	if count != 1 {
		t.Errorf("expected write count to be 1, got %f", count)
	}

	// Record a query
	metrics.QueryTotal.WithLabelValues("select", "success").Inc()
	metrics.QueryLatency.WithLabelValues("select", "one").Observe(0.1)

	queryCount := testutil.ToFloat64(metrics.QueryTotal.WithLabelValues("select", "success"))
	if queryCount != 1 {
		t.Errorf("expected query count to be 1, got %f", queryCount)
	}

	// Record replication metrics
	metrics.ReplicationSuccess.WithLabelValues("node-1").Inc()
	metrics.ReplicationLatency.WithLabelValues("node-1").Observe(0.02)

	replCount := testutil.ToFloat64(metrics.ReplicationSuccess.WithLabelValues("node-1"))
	if replCount != 1 {
		t.Errorf("expected replication success count to be 1, got %f", replCount)
	}
}

func TestNodeHealthMetrics(t *testing.T) {
	metrics := getTestMetrics()

	// Set node health status
	metrics.NodeHealthStatus.WithLabelValues("node-1").Set(2) // Healthy
	metrics.NodeHealthStatus.WithLabelValues("node-2").Set(1) // Unhealthy
	metrics.NodeHealthStatus.WithLabelValues("node-3").Set(0) // Down

	// Verify values
	node1Health := testutil.ToFloat64(metrics.NodeHealthStatus.WithLabelValues("node-1"))
	if node1Health != 2 {
		t.Errorf("expected node-1 health to be 2 (healthy), got %f", node1Health)
	}

	node2Health := testutil.ToFloat64(metrics.NodeHealthStatus.WithLabelValues("node-2"))
	if node2Health != 1 {
		t.Errorf("expected node-2 health to be 1 (unhealthy), got %f", node2Health)
	}

	// Record failures and recoveries
	metrics.NodeFailureCount.WithLabelValues("node-2").Inc()
	metrics.NodeRecoveryCount.WithLabelValues("node-2").Inc()

	failures := testutil.ToFloat64(metrics.NodeFailureCount.WithLabelValues("node-2"))
	if failures != 1 {
		t.Errorf("expected 1 failure, got %f", failures)
	}
}

func TestCircuitBreakerMetrics(t *testing.T) {
	metrics := getTestMetrics()

	// Test circuit breaker state transitions
	metrics.CircuitBreakerState.WithLabelValues("node-1").Set(0) // Closed (healthy)
	metrics.CircuitBreakerState.WithLabelValues("node-1").Set(1) // Half-open (unhealthy)
	metrics.CircuitBreakerState.WithLabelValues("node-1").Set(2) // Open (down)

	state := testutil.ToFloat64(metrics.CircuitBreakerState.WithLabelValues("node-1"))
	if state != 2 {
		t.Errorf("expected circuit breaker state to be 2 (open), got %f", state)
	}
}

func TestGRPCMetrics(t *testing.T) {
	metrics := getTestMetrics()

	// Record gRPC requests
	metrics.GRPCRequestTotal.WithLabelValues("IngesterService", "Push").Inc()
	metrics.GRPCRequestDuration.WithLabelValues("IngesterService", "Push", "OK").Observe(0.03)

	count := testutil.ToFloat64(metrics.GRPCRequestTotal.WithLabelValues("IngesterService", "Push"))
	if count != 1 {
		t.Errorf("expected gRPC request count to be 1, got %f", count)
	}

	// Record errors
	metrics.GRPCRequestErrors.WithLabelValues("IngesterService", "Push", "Unavailable").Inc()

	errorCount := testutil.ToFloat64(metrics.GRPCRequestErrors.WithLabelValues("IngesterService", "Push", "Unavailable"))
	if errorCount != 1 {
		t.Errorf("expected gRPC error count to be 1, got %f", errorCount)
	}
}

func TestClusterMetrics(t *testing.T) {
	metrics := getTestMetrics()

	// Set cluster-level metrics
	metrics.ShardCount.Set(16)
	metrics.ReplicationFactor.Set(3)
	metrics.ActiveConnections.Set(42)

	shardCount := testutil.ToFloat64(metrics.ShardCount)
	if shardCount != 16 {
		t.Errorf("expected shard count to be 16, got %f", shardCount)
	}

	replFactor := testutil.ToFloat64(metrics.ReplicationFactor)
	if replFactor != 3 {
		t.Errorf("expected replication factor to be 3, got %f", replFactor)
	}

	connections := testutil.ToFloat64(metrics.ActiveConnections)
	if connections != 42 {
		t.Errorf("expected active connections to be 42, got %f", connections)
	}
}
