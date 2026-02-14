package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete application configuration
type Config struct {
	// Target specifies which modules to run: "all", "write", "read", "backend"
	Target string `yaml:"target"`
	
	// Common configuration shared by all components
	Common CommonConfig `yaml:"common"`
	
	// Server configuration
	Server ServerConfig `yaml:"server"`
	
	// Distributor configuration
	Distributor DistributorConfig `yaml:"distributor"`
	
	// Ingester configuration
	Ingester IngesterConfig `yaml:"ingester"`
	
	// Querier configuration
	Querier QuerierConfig `yaml:"querier"`
	
	// QueryFrontend configuration
	QueryFrontend QueryFrontendConfig `yaml:"query_frontend"`
	
	// Admin configuration
	Admin AdminConfig `yaml:"admin"`
	
	// Compactor configuration
	Compactor CompactorConfig `yaml:"compactor"`
	
	// Ring configuration
	Ring RingConfig `yaml:"ring"`
	
	// Observability configuration
	Observability ObservabilityConfig `yaml:"observability"`
	
	// Security configuration
	Security SecurityConfig `yaml:"security"`
	
	// Reliability configuration
	Reliability ReliabilityConfig `yaml:"reliability"`
	
	// Backup configuration
	Backup BackupConfig `yaml:"backup"`
}

// CommonConfig contains settings shared across components
type CommonConfig struct {
	// DataDir is the directory where shard files are stored
	DataDir string `yaml:"data_dir"`

	// NumShards is the default number of shards for new indices
	NumShards int `yaml:"num_shards"`

	// DefaultIndex is the name of the default index (defaults to "_default")
	DefaultIndex string `yaml:"default_index"`

	// LogLevel controls logging verbosity (debug, info, warn, error)
	LogLevel string `yaml:"log_level"`
}

// ServerConfig configures the HTTP/gRPC server
type ServerConfig struct {
	// HTTPListenAddr is the address to listen on for HTTP (e.g., ":8080")
	HTTPListenAddr string `yaml:"http_listen_addr"`
	
	// GRPCListenAddr is the address to listen on for gRPC (e.g., ":9095")
	GRPCListenAddr string `yaml:"grpc_listen_addr"`
	
	// ShutdownTimeout is how long to wait for graceful shutdown
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	
	// ReadTimeout is the HTTP read timeout
	ReadTimeout time.Duration `yaml:"read_timeout"`
	
	// WriteTimeout is the HTTP write timeout
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

// DistributorConfig configures the distributor component
type DistributorConfig struct {
	// MaxQueryLength is the maximum allowed SQL query length in bytes
	MaxQueryLength int `yaml:"max_query_length"`
	
	// UseLocalIngester if true, calls ingester directly without gRPC
	UseLocalIngester bool `yaml:"use_local_ingester"`
	
	// ReplicationFactor is the number of replicas for each write (1 = no replication)
	ReplicationFactor int `yaml:"replication_factor"`
}

// IngesterConfig configures the ingester component
type IngesterConfig struct {
	// MaxShardsPerInstance limits how many shards one ingester can own
	MaxShardsPerInstance int `yaml:"max_shards_per_instance"`
	
	// FlushInterval is how often to flush writes to disk (0 = immediate)
	FlushInterval time.Duration `yaml:"flush_interval"`
}

// QuerierConfig configures the querier component
type QuerierConfig struct {
	// MergeStrategy determines how to merge shard results ("duckdb" or "simple")
	MergeStrategy string `yaml:"merge_strategy"`
	
	// MaxConcurrentQueries limits parallel query execution
	MaxConcurrentQueries int `yaml:"max_concurrent_queries"`
	
	// QueryTimeout is the maximum time a query can run
	QueryTimeout time.Duration `yaml:"query_timeout"`
	
	// ReadConsistency determines read consistency level ("one", "quorum", "all")
	ReadConsistency string `yaml:"read_consistency"`
}

// QueryFrontendConfig configures the query frontend component
type QueryFrontendConfig struct {
	// QueryTimeout is the timeout for queries
	QueryTimeout time.Duration `yaml:"query_timeout"`
	
	// MaxRetries is how many times to retry failed queries
	MaxRetries int `yaml:"max_retries"`
}

// AdminConfig configures the admin component
type AdminConfig struct {
	// Enabled controls whether admin endpoints are exposed
	Enabled bool `yaml:"enabled"`
}

// CompactorConfig configures the compactor component
type CompactorConfig struct {
	// Enabled controls whether compaction runs
	Enabled bool `yaml:"enabled"`
	
	// CompactionInterval is how often to run compaction
	CompactionInterval time.Duration `yaml:"compaction_interval"`
}

// RingConfig configures the hash ring and memberlist
type RingConfig struct {
	// InstanceID is a unique identifier for this instance
	InstanceID string `yaml:"instance_id"`
	
	// InstanceAddr is the address this instance advertises to peers
	InstanceAddr string `yaml:"instance_addr"`
	
	// Memberlist configuration
	Memberlist MemberlistConfig `yaml:"memberlist"`
}

// MemberlistConfig configures gossip-based peer discovery
type MemberlistConfig struct {
	// JoinPeers is a list of peer addresses to join (empty = single-node mode)
	JoinPeers []string `yaml:"join_peers"`
	
	// BindAddr is the address to bind the gossip listener to
	BindAddr string `yaml:"bind_addr"`
	
	// BindPort is the port for gossip traffic
	BindPort int `yaml:"bind_port"`
}

// ObservabilityConfig configures metrics, tracing, and logging
type ObservabilityConfig struct {
	// Metrics configuration
	Metrics MetricsConfig `yaml:"metrics"`
	
	// Tracing configuration
	Tracing TracingConfig `yaml:"tracing"`
	
	// Logging configuration
	Logging LoggingConfig `yaml:"logging"`
}

// MetricsConfig configures Prometheus metrics
type MetricsConfig struct {
	// Enabled controls whether metrics are collected
	Enabled bool `yaml:"enabled"`
	
	// Path is the HTTP endpoint path for metrics (e.g., "/metrics")
	Path string `yaml:"path"`
	
	// Namespace is the metrics namespace prefix
	Namespace string `yaml:"namespace"`
}

// TracingConfig configures distributed tracing
type TracingConfig struct {
	// Enabled controls whether tracing is enabled
	Enabled bool `yaml:"enabled"`
	
	// OTLPEndpoint is the OTLP collector endpoint (e.g., "localhost:4317")
	OTLPEndpoint string `yaml:"otlp_endpoint"`
	
	// ServiceName overrides the default service name
	ServiceName string `yaml:"service_name"`
	
	// Environment identifies the deployment environment (e.g., "production")
	Environment string `yaml:"environment"`
	
	// SampleRate is the sampling rate (0.0 to 1.0, 1.0 = trace everything)
	SampleRate float64 `yaml:"sample_rate"`
}

// LoggingConfig configures structured logging
type LoggingConfig struct {
	// Level is the log level (debug, info, warn, error)
	Level string `yaml:"level"`
	
	// Format is the log format (text or json)
	Format string `yaml:"format"`
}

// SecurityConfig configures security features
type SecurityConfig struct {
	// TLS configuration for gRPC
	TLS TLSConfig `yaml:"tls"`
	
	// Authentication configuration
	Authentication AuthenticationConfig `yaml:"authentication"`
	
	// RateLimit configuration
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

// TLSConfig configures TLS/mTLS
type TLSConfig struct {
	// Enabled controls whether TLS is enabled
	Enabled bool `yaml:"enabled"`
	
	// CertFile is the path to the TLS certificate
	CertFile string `yaml:"cert_file"`
	
	// KeyFile is the path to the TLS private key
	KeyFile string `yaml:"key_file"`
	
	// CAFile is the path to the CA certificate (for mTLS)
	CAFile string `yaml:"ca_file"`
	
	// ClientAuth controls client authentication ("none", "require", "verify")
	ClientAuth string `yaml:"client_auth"`
	
	// ServerName is the expected server name for certificate validation
	ServerName string `yaml:"server_name"`
}

// AuthenticationConfig configures authentication
type AuthenticationConfig struct {
	// Enabled controls whether authentication is enforced
	Enabled bool `yaml:"enabled"`
	
	// JWTSecret is the secret for signing JWT tokens
	JWTSecret string `yaml:"jwt_secret"`
	
	// TokenExpiration is how long tokens are valid
	TokenExpiration time.Duration `yaml:"token_expiration"`
	
	// AllowAnonymous allows unauthenticated requests
	AllowAnonymous bool `yaml:"allow_anonymous"`
}

// RateLimitConfig configures rate limiting
type RateLimitConfig struct {
	// Enabled controls whether rate limiting is enforced
	Enabled bool `yaml:"enabled"`
	
	// RequestsPerSecond is the max requests per second
	RequestsPerSecond int `yaml:"requests_per_second"`
	
	// Burst is the maximum burst size
	Burst int `yaml:"burst"`
	
	// PerTenant enables per-tenant rate limiting
	PerTenant bool `yaml:"per_tenant"`
	
	// PerAPIKey enables per-API-key rate limiting
	PerAPIKey bool `yaml:"per_api_key"`
}

// ReliabilityConfig configures reliability features
type ReliabilityConfig struct {
	// Timeouts configuration
	Timeouts TimeoutConfig `yaml:"timeouts"`
	
	// Backpressure configuration
	Backpressure BackpressureConfig `yaml:"backpressure"`
	
	// AdmissionControl configuration
	AdmissionControl AdmissionControlConfig `yaml:"admission_control"`
	
	// Degradation configuration
	Degradation DegradationConfig `yaml:"degradation"`
}

// TimeoutConfig configures operation timeouts
type TimeoutConfig struct {
	// QueryTimeout is the maximum time a query can run
	QueryTimeout time.Duration `yaml:"query_timeout"`
	
	// WriteTimeout is the maximum time a write can take
	WriteTimeout time.Duration `yaml:"write_timeout"`
	
	// ReplicationTimeout is the maximum time for replication
	ReplicationTimeout time.Duration `yaml:"replication_timeout"`
	
	// HealthCheckTimeout is the timeout for health checks
	HealthCheckTimeout time.Duration `yaml:"health_check_timeout"`
}

// BackpressureConfig configures backpressure
type BackpressureConfig struct {
	// Enabled controls whether backpressure is enabled
	Enabled bool `yaml:"enabled"`
	
	// MaxConcurrentWrites is the maximum concurrent writes allowed
	MaxConcurrentWrites int `yaml:"max_concurrent_writes"`
	
	// MaxConcurrentReads is the maximum concurrent reads allowed
	MaxConcurrentReads int `yaml:"max_concurrent_reads"`
	
	// MaxQueueSize is the maximum size of the request queue
	MaxQueueSize int `yaml:"max_queue_size"`
	
	// QueueTimeout is how long to wait in queue before rejecting
	QueueTimeout time.Duration `yaml:"queue_timeout"`
}

// AdmissionControlConfig configures admission control
type AdmissionControlConfig struct {
	// Enabled controls whether admission control is enabled
	Enabled bool `yaml:"enabled"`
	
	// MaxMemoryMB is the maximum memory usage in MB
	MaxMemoryMB int64 `yaml:"max_memory_mb"`
	
	// MaxCPUPercent is the maximum CPU usage percentage
	MaxCPUPercent float64 `yaml:"max_cpu_percent"`
}

// DegradationConfig configures graceful degradation
type DegradationConfig struct {
	// Enabled controls whether graceful degradation is enabled
	Enabled bool `yaml:"enabled"`
	
	// AutoDegrade enables automatic degradation on errors
	AutoDegrade bool `yaml:"auto_degrade"`
	
	// ErrorThreshold is the number of errors before degrading
	ErrorThreshold int `yaml:"error_threshold"`
}

// BackupConfig configures backup and restore
type BackupConfig struct {
	// StorageType specifies where backups are stored: "local", "s3", "gcs"
	StorageType string `yaml:"storage_type"`
	
	// LocalPath is the directory for local backups
	LocalPath string `yaml:"local_path"`
	
	// Compression enables gzip compression of backups
	Compression bool `yaml:"compression"`
	
	// Retention is the number of backups to retain (0 = unlimited)
	Retention int `yaml:"retention"`
	
	// ScheduleInterval is the interval for automatic backups (0 = disabled)
	ScheduleInterval time.Duration `yaml:"schedule_interval"`
}

// Default returns a config with sensible defaults
func Default() *Config {
	return &Config{
		Target: "all",
		Common: CommonConfig{
			DataDir:      "./data",
			NumShards:    3,
			DefaultIndex: "_default",
			LogLevel:     "info",
		},
		Server: ServerConfig{
			HTTPListenAddr:  ":8080",
			GRPCListenAddr:  ":9095",
			ShutdownTimeout: 30 * time.Second,
			ReadTimeout:     30 * time.Second,
			WriteTimeout:    30 * time.Second,
		},
		Distributor: DistributorConfig{
			MaxQueryLength:    1048576, // 1MB
			UseLocalIngester:  true,
			ReplicationFactor: 1, // No replication by default
		},
		Ingester: IngesterConfig{
			MaxShardsPerInstance: 10,
			FlushInterval:        0, // Immediate
		},
		Querier: QuerierConfig{
			MergeStrategy:        "duckdb",
			MaxConcurrentQueries: 100,
			QueryTimeout:         60 * time.Second,
			ReadConsistency:      "one", // Read from one replica by default
		},
		QueryFrontend: QueryFrontendConfig{
			QueryTimeout: 60 * time.Second,
			MaxRetries:   3,
		},
		Admin: AdminConfig{
			Enabled: true,
		},
		Compactor: CompactorConfig{
			Enabled:            false,
			CompactionInterval: 1 * time.Hour,
		},
		Ring: RingConfig{
			InstanceID:   "instance-0",
			InstanceAddr: "localhost:9095",
			Memberlist: MemberlistConfig{
				JoinPeers: []string{},
				BindAddr:  "0.0.0.0",
				BindPort:  7946,
			},
		},
		Observability: ObservabilityConfig{
			Metrics: MetricsConfig{
				Enabled:   true,
				Path:      "/metrics",
				Namespace: "duckdb_cluster",
			},
			Tracing: TracingConfig{
				Enabled:      false,
				OTLPEndpoint: "localhost:4317",
				ServiceName:  "duckdb-cluster",
				Environment:  "development",
				SampleRate:   1.0,
			},
			Logging: LoggingConfig{
				Level:  "info",
				Format: "text",
			},
		},
		Security: SecurityConfig{
			TLS: TLSConfig{
				Enabled:    false,
				ClientAuth: "none",
			},
			Authentication: AuthenticationConfig{
				Enabled:         false,
				TokenExpiration: 24 * time.Hour,
				AllowAnonymous:  true,
			},
			RateLimit: RateLimitConfig{
				Enabled:           false,
				RequestsPerSecond: 100,
				Burst:             200,
				PerTenant:         true,
				PerAPIKey:         false,
			},
		},
		Reliability: ReliabilityConfig{
			Timeouts: TimeoutConfig{
				QueryTimeout:       60 * time.Second,
				WriteTimeout:       30 * time.Second,
				ReplicationTimeout: 10 * time.Second,
				HealthCheckTimeout: 5 * time.Second,
			},
			Backpressure: BackpressureConfig{
				Enabled:             true,
				MaxConcurrentWrites: 100,
				MaxConcurrentReads:  1000,
				MaxQueueSize:        1000,
				QueueTimeout:        10 * time.Second,
			},
			AdmissionControl: AdmissionControlConfig{
				Enabled:       false,
				MaxMemoryMB:   8192,
				MaxCPUPercent: 90.0,
			},
			Degradation: DegradationConfig{
				Enabled:        true,
				AutoDegrade:    false,
				ErrorThreshold: 10,
			},
		},
		Backup: BackupConfig{
			StorageType:      "local",
			LocalPath:        "./data/backups",
			Compression:      true,
			Retention:        7, // Keep last 7 backups
			ScheduleInterval: 0, // Disabled by default
		},
	}
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return nil, fmt.Errorf("read config file: %w", err)
	}
	
	// Start with defaults
	cfg := Default()
	
	// Unmarshal YAML, overriding defaults
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config YAML: %w", err)
	}
	
	// Apply common config to components if they weren't explicitly set
	if err := cfg.applyCommon(); err != nil {
		return nil, fmt.Errorf("apply common config: %w", err)
	}
	
	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	
	return cfg, nil
}

// Save writes configuration to a YAML file
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	
	return nil
}

// applyCommon applies common config values to component configs where appropriate
func (c *Config) applyCommon() error {
	// Currently, common config is mostly informational
	// Components can access it directly via cfg.Common
	// In the future, we might add more sophisticated inheritance
	return nil
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Validate target
	validTargets := map[string]bool{
		"all": true, "write": true, "read": true, "backend": true,
	}
	if !validTargets[c.Target] {
		return fmt.Errorf("invalid target: %s (must be: all, write, read, backend)", c.Target)
	}
	
	// Validate common config
	if c.Common.NumShards < 1 {
		return fmt.Errorf("num_shards must be at least 1, got %d", c.Common.NumShards)
	}
	if c.Common.NumShards > 1000 {
		return fmt.Errorf("num_shards too large: %d (max 1000)", c.Common.NumShards)
	}
	
	// Validate server config
	if c.Server.ShutdownTimeout < 0 {
		return fmt.Errorf("shutdown_timeout cannot be negative")
	}
	
	// Validate distributor config
	if c.Distributor.ReplicationFactor < 1 {
		return fmt.Errorf("replication_factor must be at least 1")
	}
	if c.Distributor.ReplicationFactor > 10 {
		return fmt.Errorf("replication_factor too large: %d (max 10)", c.Distributor.ReplicationFactor)
	}
	
	// Validate querier config
	if c.Querier.MergeStrategy != "duckdb" && c.Querier.MergeStrategy != "simple" {
		return fmt.Errorf("invalid merge_strategy: %s (must be 'duckdb' or 'simple')", c.Querier.MergeStrategy)
	}
	if c.Querier.MaxConcurrentQueries < 1 {
		return fmt.Errorf("max_concurrent_queries must be at least 1")
	}
	
	validConsistency := map[string]bool{
		"one": true, "quorum": true, "all": true,
	}
	if !validConsistency[c.Querier.ReadConsistency] {
		return fmt.Errorf("invalid read_consistency: %s (must be: one, quorum, all)", c.Querier.ReadConsistency)
	}
	
	// Validate ingester config
	if c.Ingester.MaxShardsPerInstance < 1 {
		return fmt.Errorf("max_shards_per_instance must be at least 1")
	}
	
	return nil
}

// IsMonolithic returns true if running in monolithic mode (target == "all")
func (c *Config) IsMonolithic() bool {
	return c.Target == "all"
}

// IsSingleNode returns true if this is a single-node deployment (no join peers)
func (c *Config) IsSingleNode() bool {
	return len(c.Ring.Memberlist.JoinPeers) == 0
}
