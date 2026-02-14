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

func cmdTemplate(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster template <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  list     List all templates")
		fmt.Fprintln(os.Stderr, "  create   Create a template")
		fmt.Fprintln(os.Stderr, "  delete   Delete a template")
		fmt.Fprintln(os.Stderr, "  get      Get template details")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		cmdTemplateList(args[1:])
	case "create":
		cmdTemplateCreate(args[1:])
	case "delete":
		cmdTemplateDelete(args[1:])
	case "get":
		cmdTemplateGet(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown template subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdTemplateList(args []string) {
	fs := flag.NewFlagSet("template list", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/templates", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Templates []struct {
			Name     string `json:"name"`
			Pattern  string `json:"pattern"`
			Priority int    `json:"priority"`
			Settings struct {
				ShardCount int `json:"shard_count"`
			} `json:"settings"`
		} `json:"templates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	if len(result.Templates) == 0 {
		fmt.Println("No templates found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPATTERN\tPRIORITY\tSHARDS")
	for _, t := range result.Templates {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", t.Name, t.Pattern, t.Priority, t.Settings.ShardCount)
	}
	tw.Flush()
}

func cmdTemplateCreate(args []string) {
	fs := flag.NewFlagSet("template create", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	pattern := fs.String("pattern", "", "index name pattern (e.g., 'logs-*') (required)")
	shards := fs.Int("shards", 3, "number of shards for matched indices")
	priority := fs.Int("priority", 0, "template priority (higher = matched first)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster template create <name> --pattern 'logs-*' [--shards N] [--priority N]")
		os.Exit(1)
	}
	if *pattern == "" {
		fmt.Fprintln(os.Stderr, "Error: --pattern is required")
		os.Exit(1)
	}
	name := fs.Arg(0)

	body, _ := json.Marshal(map[string]any{
		"pattern":  *pattern,
		"priority": *priority,
		"settings": map[string]any{"shard_count": *shards},
	})

	url := fmt.Sprintf("http://localhost%s/templates/%s", *addr, name)
	req, _ := http.NewRequest(http.MethodPut, url, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	fmt.Printf("Template %q created (pattern: %s, shards: %d, priority: %d)\n", name, *pattern, *shards, *priority)
}

func cmdTemplateDelete(args []string) {
	fs := flag.NewFlagSet("template delete", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster template delete <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/templates/%s", *addr, name)
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

	fmt.Printf("Template %q deleted\n", name)
}

func cmdTemplateGet(args []string) {
	fs := flag.NewFlagSet("template get", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster template get <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/templates/%s", *addr, name)
	resp, err := http.Get(url)
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

	var tmpl struct {
		Name     string `json:"name"`
		Pattern  string `json:"pattern"`
		Priority int    `json:"priority"`
		Settings struct {
			ShardCount int `json:"shard_count"`
		} `json:"settings"`
	}
	json.NewDecoder(resp.Body).Decode(&tmpl)

	fmt.Printf("Name:     %s\n", tmpl.Name)
	fmt.Printf("Pattern:  %s\n", tmpl.Pattern)
	fmt.Printf("Priority: %d\n", tmpl.Priority)
	fmt.Printf("Shards:   %d\n", tmpl.Settings.ShardCount)
}
