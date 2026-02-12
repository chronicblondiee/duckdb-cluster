package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/brown/duckdb-cluster/internal/api"
	"github.com/brown/duckdb-cluster/internal/cluster"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "init":
		cmdInit(os.Args[2:])
	case "start":
		cmdStart(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster <command> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  init    Initialize a new cluster")
	fmt.Fprintln(os.Stderr, "  start   Start the cluster server")
	fmt.Fprintln(os.Stderr, "  status  Check cluster status")
}

func cmdInit(args []string) {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	shards := fs.Int("shards", 3, "number of shards")
	dataDir := fs.String("data-dir", "./data", "data directory")
	fs.Parse(args)

	cfg := &cluster.Config{
		DataDir:    *dataDir,
		NumShards:  *shards,
		ListenAddr: ":8080",
	}

	c, err := cluster.NewCluster(cfg)
	if err != nil {
		slog.Error("failed to create cluster", "error", err)
		os.Exit(1)
	}

	if err := c.Init(); err != nil {
		slog.Error("failed to init cluster", "error", err)
		os.Exit(1)
	}

	fmt.Printf("Cluster initialized with %d shards in %s\n", *shards, *dataDir)
}

func cmdStart(args []string) {
	fs := flag.NewFlagSet("start", flag.ExitOnError)
	addr := fs.String("addr", "", "listen address (overrides config)")
	dataDir := fs.String("data-dir", "", "data directory (overrides config)")
	fs.Parse(args)

	cfg, err := cluster.LoadConfig("cluster.json")
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if *addr != "" {
		cfg.ListenAddr = *addr
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
	}

	c, err := cluster.NewCluster(cfg)
	if err != nil {
		slog.Error("failed to create cluster", "error", err)
		os.Exit(1)
	}

	if err := c.Start(); err != nil {
		slog.Error("failed to start cluster", "error", err)
		os.Exit(1)
	}

	srv := api.NewServer(c)
	if err := srv.Start(cfg.ListenAddr); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/health", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status cluster.ClusterStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Status: %s\nShards: %d\n", status.Status, status.ShardCount)
}
