package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	
	// Check common defaults
	if cfg.Common.DataDir != "./data" {
		t.Errorf("expected DataDir './data', got '%s'", cfg.Common.DataDir)
	}
	if cfg.Common.NumShards != 3 {
		t.Errorf("expected NumShards 3, got %d", cfg.Common.NumShards)
	}
	
	// Check server defaults
	if cfg.Server.HTTPListenAddr != ":8080" {
		t.Errorf("expected HTTPListenAddr ':8080', got '%s'", cfg.Server.HTTPListenAddr)
	}
	if cfg.Server.GRPCListenAddr != ":9095" {
		t.Errorf("expected GRPCListenAddr ':9095', got '%s'", cfg.Server.GRPCListenAddr)
	}
	
	// Check target default
	if cfg.Target != "all" {
		t.Errorf("expected Target 'all', got '%s'", cfg.Target)
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		modifyFn  func(*Config)
		shouldErr bool
		errMsg    string
	}{
		{
			name:      "valid default config",
			modifyFn:  func(c *Config) {},
			shouldErr: false,
		},
		{
			name: "invalid target",
			modifyFn: func(c *Config) {
				c.Target = "invalid"
			},
			shouldErr: true,
			errMsg:    "invalid target",
		},
		{
			name: "zero shards",
			modifyFn: func(c *Config) {
				c.Common.NumShards = 0
			},
			shouldErr: true,
			errMsg:    "num_shards must be at least 1",
		},
		{
			name: "too many shards",
			modifyFn: func(c *Config) {
				c.Common.NumShards = 1001
			},
			shouldErr: true,
			errMsg:    "num_shards too large",
		},
		{
			name: "negative shutdown timeout",
			modifyFn: func(c *Config) {
				c.Server.ShutdownTimeout = -1 * time.Second
			},
			shouldErr: true,
			errMsg:    "shutdown_timeout cannot be negative",
		},
		{
			name: "invalid merge strategy",
			modifyFn: func(c *Config) {
				c.Querier.MergeStrategy = "invalid"
			},
			shouldErr: true,
			errMsg:    "invalid merge_strategy",
		},
		{
			name: "zero max concurrent queries",
			modifyFn: func(c *Config) {
				c.Querier.MaxConcurrentQueries = 0
			},
			shouldErr: true,
			errMsg:    "max_concurrent_queries must be at least 1",
		},
		{
			name: "valid write target",
			modifyFn: func(c *Config) {
				c.Target = "write"
			},
			shouldErr: false,
		},
		{
			name: "valid read target",
			modifyFn: func(c *Config) {
				c.Target = "read"
			},
			shouldErr: false,
		},
		{
			name: "valid backend target",
			modifyFn: func(c *Config) {
				c.Target = "backend"
			},
			shouldErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.modifyFn(cfg)
			
			err := cfg.Validate()
			if tt.shouldErr {
				if err == nil {
					t.Fatal("expected validation error but got none")
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					// Check if error contains expected message
					if len(err.Error()) < len(tt.errMsg) || err.Error()[:len(tt.errMsg)] != tt.errMsg {
						t.Errorf("expected error to start with '%s', got '%s'", tt.errMsg, err.Error())
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected validation error: %v", err)
				}
			}
		})
	}
}

func TestConfigLoadSave(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	// Create and save config
	cfg := Default()
	cfg.Common.NumShards = 5
	cfg.Server.HTTPListenAddr = ":9090"
	cfg.Target = "write"
	
	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	
	// Load config
	loaded, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	
	// Verify values
	if loaded.Common.NumShards != 5 {
		t.Errorf("expected NumShards 5, got %d", loaded.Common.NumShards)
	}
	if loaded.Server.HTTPListenAddr != ":9090" {
		t.Errorf("expected HTTPListenAddr ':9090', got '%s'", loaded.Server.HTTPListenAddr)
	}
	if loaded.Target != "write" {
		t.Errorf("expected Target 'write', got '%s'", loaded.Target)
	}
}

func TestConfigLoadNonExistent(t *testing.T) {
	// Loading non-existent file should return defaults
	cfg, err := Load("/non/existent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for non-existent file, got: %v", err)
	}
	
	// Should have default values
	if cfg.Common.NumShards != 3 {
		t.Errorf("expected default NumShards 3, got %d", cfg.Common.NumShards)
	}
}

func TestConfigYAMLFormat(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-yaml-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	// Write a minimal YAML config
	yamlContent := `
target: read
common:
  data_dir: /var/lib/duckdb
  num_shards: 10
  log_level: debug
server:
  http_listen_addr: :8888
  grpc_listen_addr: :9999
querier:
  merge_strategy: simple
  max_concurrent_queries: 50
`
	
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write YAML: %v", err)
	}
	
	// Load and verify
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load YAML config: %v", err)
	}
	
	if cfg.Target != "read" {
		t.Errorf("expected Target 'read', got '%s'", cfg.Target)
	}
	if cfg.Common.DataDir != "/var/lib/duckdb" {
		t.Errorf("expected DataDir '/var/lib/duckdb', got '%s'", cfg.Common.DataDir)
	}
	if cfg.Common.NumShards != 10 {
		t.Errorf("expected NumShards 10, got %d", cfg.Common.NumShards)
	}
	if cfg.Common.LogLevel != "debug" {
		t.Errorf("expected LogLevel 'debug', got '%s'", cfg.Common.LogLevel)
	}
	if cfg.Server.HTTPListenAddr != ":8888" {
		t.Errorf("expected HTTPListenAddr ':8888', got '%s'", cfg.Server.HTTPListenAddr)
	}
	if cfg.Querier.MergeStrategy != "simple" {
		t.Errorf("expected MergeStrategy 'simple', got '%s'", cfg.Querier.MergeStrategy)
	}
	if cfg.Querier.MaxConcurrentQueries != 50 {
		t.Errorf("expected MaxConcurrentQueries 50, got %d", cfg.Querier.MaxConcurrentQueries)
	}
	
	// Values not in YAML should have defaults
	if cfg.Server.ShutdownTimeout != 30*time.Second {
		t.Errorf("expected default ShutdownTimeout 30s, got %v", cfg.Server.ShutdownTimeout)
	}
}

func TestConfigIsMonolithic(t *testing.T) {
	tests := []struct {
		target   string
		expected bool
	}{
		{"all", true},
		{"write", false},
		{"read", false},
		{"backend", false},
	}
	
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			cfg := Default()
			cfg.Target = tt.target
			
			if cfg.IsMonolithic() != tt.expected {
				t.Errorf("IsMonolithic() = %v, want %v", cfg.IsMonolithic(), tt.expected)
			}
		})
	}
}

func TestConfigIsSingleNode(t *testing.T) {
	cfg := Default()
	
	// Default config with no peers should be single-node
	if !cfg.IsSingleNode() {
		t.Error("expected IsSingleNode() = true for default config")
	}
	
	// Adding peers should make it multi-node
	cfg.Ring.Memberlist.JoinPeers = []string{"peer1:7946", "peer2:7946"}
	if cfg.IsSingleNode() {
		t.Error("expected IsSingleNode() = false when peers are configured")
	}
}

func TestConfigPartialYAML(t *testing.T) {
	// Test that partial YAML configs work (unspecified fields get defaults)
	tmpDir, err := os.MkdirTemp("", "config-partial-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	configPath := filepath.Join(tmpDir, "config.yaml")
	
	// Write minimal config
	yamlContent := `
target: all
common:
  num_shards: 7
`
	
	if err := os.WriteFile(configPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write YAML: %v", err)
	}
	
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	
	// Specified value should be set
	if cfg.Common.NumShards != 7 {
		t.Errorf("expected NumShards 7, got %d", cfg.Common.NumShards)
	}
	
	// Unspecified values should have defaults
	if cfg.Common.DataDir != "./data" {
		t.Errorf("expected default DataDir, got '%s'", cfg.Common.DataDir)
	}
	if cfg.Server.HTTPListenAddr != ":8080" {
		t.Errorf("expected default HTTPListenAddr, got '%s'", cfg.Server.HTTPListenAddr)
	}
}
