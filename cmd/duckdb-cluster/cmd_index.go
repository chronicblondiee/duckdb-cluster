package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
)

func cmdIndex(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster index <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  list     List all indices")
		fmt.Fprintln(os.Stderr, "  create   Create a new index")
		fmt.Fprintln(os.Stderr, "  delete   Delete an index")
		fmt.Fprintln(os.Stderr, "  get      Get index details")
		fmt.Fprintln(os.Stderr, "  close    Close an index")
		fmt.Fprintln(os.Stderr, "  open     Open a closed index")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		cmdIndexList(args[1:])
	case "create":
		cmdIndexCreate(args[1:])
	case "delete":
		cmdIndexDelete(args[1:])
	case "get":
		cmdIndexGet(args[1:])
	case "close":
		cmdIndexClose(args[1:])
	case "open":
		cmdIndexOpen(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown index subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdIndexList(args []string) {
	fs := flag.NewFlagSet("index list", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/indices", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Indices []struct {
			Name     string `json:"name"`
			State    string `json:"state"`
			Settings struct {
				ShardCount int `json:"shard_count"`
			} `json:"settings"`
		} `json:"indices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATE\tSHARDS")
	for _, idx := range result.Indices {
		fmt.Fprintf(tw, "%s\t%s\t%d\n", idx.Name, idx.State, idx.Settings.ShardCount)
	}
	tw.Flush()
}

func cmdIndexCreate(args []string) {
	fs := flag.NewFlagSet("index create", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	shards := fs.Int("shards", 3, "number of shards")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster index create <name> [--shards N]")
		os.Exit(1)
	}
	name := fs.Arg(0)

	body := fmt.Sprintf(`{"settings":{"shard_count":%d}}`, *shards)
	url := fmt.Sprintf("http://localhost%s/indices/%s", *addr, name)
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintf(os.Stderr, "Error: %v\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Index %q created with %d shards\n", name, *shards)
}

func cmdIndexDelete(args []string) {
	fs := flag.NewFlagSet("index delete", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster index delete <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/indices/%s", *addr, name)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Index %q deleted\n", name)
}

func cmdIndexGet(args []string) {
	fs := flag.NewFlagSet("index get", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster index get <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/indices/%s", *addr, name)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Error: %v\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Name:     %v\n", result["name"])
	fmt.Printf("State:    %v\n", result["state"])
	fmt.Printf("Created:  %v\n", result["created"])
	if settings, ok := result["settings"].(map[string]any); ok {
		fmt.Printf("Shards:   %v\n", formatFloat(settings["shard_count"]))
	}
}

func cmdIndexClose(args []string) {
	fs := flag.NewFlagSet("index close", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster index close <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/indices/%s/_close", *addr, name)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Index %q closed\n", name)
}

func cmdIndexOpen(args []string) {
	fs := flag.NewFlagSet("index open", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster index open <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/indices/%s/_open", *addr, name)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Index %q opened\n", name)
}
