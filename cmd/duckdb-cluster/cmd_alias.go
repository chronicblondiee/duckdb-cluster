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

func cmdAlias(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster alias <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  list     List all aliases")
		fmt.Fprintln(os.Stderr, "  create   Create an alias")
		fmt.Fprintln(os.Stderr, "  delete   Delete an alias")
		fmt.Fprintln(os.Stderr, "  get      Get alias details")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		cmdAliasList(args[1:])
	case "create":
		cmdAliasCreate(args[1:])
	case "delete":
		cmdAliasDelete(args[1:])
	case "get":
		cmdAliasGet(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown alias subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdAliasList(args []string) {
	fs := flag.NewFlagSet("alias list", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/aliases", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not connect to cluster at %s: %v\n", *addr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Aliases []struct {
			Name    string   `json:"name"`
			Indices []string `json:"indices"`
		} `json:"aliases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: invalid response: %v\n", err)
		os.Exit(1)
	}

	if len(result.Aliases) == 0 {
		fmt.Println("No aliases found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tINDICES")
	for _, a := range result.Aliases {
		fmt.Fprintf(tw, "%s\t%s\n", a.Name, strings.Join(a.Indices, ", "))
	}
	tw.Flush()
}

func cmdAliasCreate(args []string) {
	fs := flag.NewFlagSet("alias create", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	indices := fs.String("indices", "", "comma-separated index names (required)")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster alias create <name> --indices idx1,idx2")
		os.Exit(1)
	}
	if *indices == "" {
		fmt.Fprintln(os.Stderr, "Error: --indices is required")
		os.Exit(1)
	}
	name := fs.Arg(0)

	idxList := strings.Split(*indices, ",")
	body, _ := json.Marshal(map[string]any{"indices": idxList})

	url := fmt.Sprintf("http://localhost%s/aliases/%s", *addr, name)
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

	fmt.Printf("Alias %q created -> %s\n", name, *indices)
}

func cmdAliasDelete(args []string) {
	fs := flag.NewFlagSet("alias delete", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster alias delete <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/aliases/%s", *addr, name)
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

	fmt.Printf("Alias %q deleted\n", name)
}

func cmdAliasGet(args []string) {
	fs := flag.NewFlagSet("alias get", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster alias get <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/aliases/%s", *addr, name)
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

	var alias struct {
		Name    string   `json:"name"`
		Indices []string `json:"indices"`
	}
	json.NewDecoder(resp.Body).Decode(&alias)

	fmt.Printf("Name:    %s\n", alias.Name)
	fmt.Printf("Indices: %s\n", strings.Join(alias.Indices, ", "))
}
