package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the cluster
type Metrics struct {
	// Write path metrics
	WriteLatency       *prometheus.HistogramVec
	WriteTotal         *prometheus.CounterVec
	WriteErrors        *prometheus.CounterVec
	ReplicationLatency *prometheus.HistogramVec
	ReplicationSuccess *prometheus.CounterVec
	ReplicationFailure *prometheus.CounterVec

	// Read path metrics
	QueryLatency       *prometheus.HistogramVec
	QueryTotal         *prometheus.CounterVec
	QueryErrors        *prometheus.CounterVec
	MergeLatency       prometheus.Histogram
	ShardQueryLatency  *prometheus.HistogramVec

	// Node health metrics
	NodeHealthStatus  *prometheus.GaugeVec
	NodeFailureCount  *prometheus.CounterVec
	NodeRecoveryCount *prometheus.CounterVec
	CircuitBreakerState *prometheus.GaugeVec

	// gRPC metrics
	GRPCRequestDuration *prometheus.HistogramVec
	GRPCRequestTotal    *prometheus.CounterVec
	GRPCRequestErrors   *prometheus.CounterVec

	// Cluster metrics
	ShardCount        prometheus.Gauge
	ActiveConnections prometheus.Gauge
	ReplicationFactor prometheus.Gauge
}

// NewMetrics initializes all Prometheus metrics
func NewMetrics(namespace string) *Metrics {
	if namespace == "" {
		namespace = "duckdb_cluster"
	}

	return &Metrics{
		// Write path metrics
		WriteLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "write_latency_seconds",
				Help:      "Histogram of write request latencies in seconds",
				Buckets:   prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
			},
			[]string{"operation", "shard"},
		),
		WriteTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "write_total",
				Help:      "Total number of write operations",
			},
			[]string{"operation", "status"},
		),
		WriteErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "write_errors_total",
				Help:      "Total number of write errors",
			},
			[]string{"operation", "error_type"},
		),
		ReplicationLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "replication_latency_seconds",
				Help:      "Histogram of replication latencies in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"target_node"},
		),
		ReplicationSuccess: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "replication_success_total",
				Help:      "Total number of successful replications",
			},
			[]string{"target_node"},
		),
		ReplicationFailure: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "replication_failure_total",
				Help:      "Total number of failed replications",
			},
			[]string{"target_node", "reason"},
		),

		// Read path metrics
		QueryLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "query_latency_seconds",
				Help:      "Histogram of query latencies in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"query_type", "consistency_level"},
		),
		QueryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "query_total",
				Help:      "Total number of queries",
			},
			[]string{"query_type", "status"},
		),
		QueryErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "query_errors_total",
				Help:      "Total number of query errors",
			},
			[]string{"query_type", "error_type"},
		),
		MergeLatency: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "merge_latency_seconds",
				Help:      "Histogram of result merge latencies in seconds",
				Buckets:   prometheus.DefBuckets,
			},
		),
		ShardQueryLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "shard_query_latency_seconds",
				Help:      "Histogram of per-shard query latencies in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"shard_id"},
		),

		// Node health metrics
		NodeHealthStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "node_health_status",
				Help:      "Node health status (0=down, 1=unhealthy, 2=healthy)",
			},
			[]string{"node_id"},
		),
		NodeFailureCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "node_failure_total",
				Help:      "Total number of node failures",
			},
			[]string{"node_id"},
		),
		NodeRecoveryCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "node_recovery_total",
				Help:      "Total number of node recoveries",
			},
			[]string{"node_id"},
		),
		CircuitBreakerState: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "circuit_breaker_state",
				Help:      "Circuit breaker state (0=closed/healthy, 1=half-open/unhealthy, 2=open/down)",
			},
			[]string{"node_id"},
		),

		// gRPC metrics
		GRPCRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "grpc_request_duration_seconds",
				Help:      "Histogram of gRPC request durations in seconds",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"service", "method", "status"},
		),
		GRPCRequestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "grpc_request_total",
				Help:      "Total number of gRPC requests",
			},
			[]string{"service", "method"},
		),
		GRPCRequestErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "grpc_request_errors_total",
				Help:      "Total number of gRPC request errors",
			},
			[]string{"service", "method", "code"},
		),

		// Cluster metrics
		ShardCount: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "shard_count",
				Help:      "Current number of shards in the cluster",
			},
		),
		ActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "active_connections",
				Help:      "Current number of active connections",
			},
		),
		ReplicationFactor: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "replication_factor",
				Help:      "Configured replication factor",
			},
		),
	}
}

// GetRegistry returns the default Prometheus registry
func GetRegistry() *prometheus.Registry {
	return prometheus.DefaultRegisterer.(*prometheus.Registry)
}
