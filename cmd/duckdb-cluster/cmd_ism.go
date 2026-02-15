package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

func cmdISM(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism <subcommand>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  list     List all ISM policies")
		fmt.Fprintln(os.Stderr, "  create   Create/update an ISM policy from a YAML file")
		fmt.Fprintln(os.Stderr, "  delete   Delete an ISM policy")
		fmt.Fprintln(os.Stderr, "  get      Get ISM policy details")
		fmt.Fprintln(os.Stderr, "  status   Show ISM status for indices")
		fmt.Fprintln(os.Stderr, "  attach   Attach an ISM policy to an index")
		fmt.Fprintln(os.Stderr, "  detach   Detach ISM policy from an index")
		fmt.Fprintln(os.Stderr, "  retry    Retry a failed ISM action on an index")
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		cmdISMList(args[1:])
	case "create":
		cmdISMCreate(args[1:])
	case "delete":
		cmdISMDelete(args[1:])
	case "get":
		cmdISMGet(args[1:])
	case "status":
		cmdISMStatus(args[1:])
	case "attach":
		cmdISMAttach(args[1:])
	case "detach":
		cmdISMDetach(args[1:])
	case "retry":
		cmdISMRetry(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown ism subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdISMList(args []string) {
	fs := flag.NewFlagSet("ism list", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost%s/ism/policies", *addr)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var result struct {
		Policies []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Version     int    `json:"version"`
		} `json:"policies"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Policies) == 0 {
		fmt.Println("No ISM policies found.")
		return
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tDESCRIPTION")
	for _, p := range result.Policies {
		fmt.Fprintf(tw, "%s\t%d\t%s\n", p.Name, p.Version, p.Description)
	}
	tw.Flush()
}

func cmdISMCreate(args []string) {
	fs := flag.NewFlagSet("ism create", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	file := fs.String("file", "", "YAML policy file (required)")
	fs.Parse(args)

	if *file == "" {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism create --file <policy.yaml>")
		os.Exit(1)
	}

	data, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	// Parse YAML to get name, then convert to JSON for API
	var policy map[string]any
	if err := yaml.Unmarshal(data, &policy); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing YAML: %v\n", err)
		os.Exit(1)
	}

	name, ok := policy["name"].(string)
	if !ok || name == "" {
		fmt.Fprintln(os.Stderr, "Error: policy file must have a 'name' field")
		os.Exit(1)
	}

	jsonData, err := json.Marshal(policy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error converting to JSON: %v\n", err)
		os.Exit(1)
	}

	url := fmt.Sprintf("http://localhost%s/ism/policies/%s", *addr, name)
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	fmt.Printf("ISM policy %q created/updated from %s\n", name, *file)
}

func cmdISMDelete(args []string) {
	fs := flag.NewFlagSet("ism delete", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism delete <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/ism/policies/%s", *addr, name)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}
	fmt.Printf("ISM policy %q deleted\n", name)
}

func cmdISMGet(args []string) {
	fs := flag.NewFlagSet("ism get", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism get <name>")
		os.Exit(1)
	}
	name := fs.Arg(0)

	url := fmt.Sprintf("http://localhost%s/ism/policies/%s", *addr, name)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	var policy json.RawMessage
	json.NewDecoder(resp.Body).Decode(&policy)
	formatted, _ := json.MarshalIndent(policy, "", "  ")
	fmt.Println(string(formatted))
}

func cmdISMStatus(args []string) {
	fs := flag.NewFlagSet("ism status", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	indexName := fs.String("index", "", "specific index name (optional)")
	fs.Parse(args)

	// Also accept positional arg
	if *indexName == "" && fs.NArg() > 0 {
		*indexName = fs.Arg(0)
	}

	var url string
	if *indexName != "" {
		url = fmt.Sprintf("http://localhost%s/ism/status/%s", *addr, *indexName)
	} else {
		url = fmt.Sprintf("http://localhost%s/ism/status", *addr)
	}

	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}

	if *indexName != "" {
		// Single index status
		var status json.RawMessage
		json.NewDecoder(resp.Body).Decode(&status)
		formatted, _ := json.MarshalIndent(status, "", "  ")
		fmt.Println(string(formatted))
	} else {
		// All statuses
		var result struct {
			Statuses []struct {
				Index        string `json:"index"`
				PolicyName   string `json:"policy_name"`
				CurrentState string `json:"current_state"`
				ActionIndex  int    `json:"action_index"`
				Failed       bool   `json:"failed"`
				LastError    string `json:"last_error"`
			} `json:"statuses"`
		}
		json.NewDecoder(resp.Body).Decode(&result)

		if len(result.Statuses) == 0 {
			fmt.Println("No indices managed by ISM.")
			return
		}

		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "INDEX\tPOLICY\tSTATE\tACTION\tFAILED\tERROR")
		for _, s := range result.Statuses {
			failed := ""
			if s.Failed {
				failed = "YES"
			}
			errMsg := s.LastError
			if len(errMsg) > 40 {
				errMsg = errMsg[:40] + "..."
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n",
				s.Index, s.PolicyName, s.CurrentState, s.ActionIndex, failed, errMsg)
		}
		tw.Flush()
	}
}

func cmdISMAttach(args []string) {
	fs := flag.NewFlagSet("ism attach", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	indexName := fs.String("index", "", "index name (required)")
	policy := fs.String("policy", "", "policy name (required)")
	fs.Parse(args)

	if *indexName == "" || *policy == "" {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism attach --index <name> --policy <name>")
		os.Exit(1)
	}

	body, _ := json.Marshal(map[string]string{"policy": *policy})
	url := fmt.Sprintf("http://localhost%s/ism/attach/%s", *addr, *indexName)
	resp, err := http.Post(url, "application/json", strings.NewReader(string(body)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}
	fmt.Printf("Policy %q attached to index %q\n", *policy, *indexName)
}

func cmdISMDetach(args []string) {
	fs := flag.NewFlagSet("ism detach", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	indexName := fs.String("index", "", "index name (required)")
	fs.Parse(args)

	if *indexName == "" {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism detach --index <name>")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://localhost%s/ism/detach/%s", *addr, *indexName)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}
	fmt.Printf("ISM policy detached from index %q\n", *indexName)
}

func cmdISMRetry(args []string) {
	fs := flag.NewFlagSet("ism retry", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "server address")
	indexName := fs.String("index", "", "index name (required)")
	fs.Parse(args)

	// Also accept positional arg
	if *indexName == "" && fs.NArg() > 0 {
		*indexName = fs.Arg(0)
	}

	if *indexName == "" {
		fmt.Fprintln(os.Stderr, "Usage: duckdb-cluster ism retry --index <name>")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://localhost%s/ism/retry/%s", *addr, *indexName)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var result map[string]string
		json.NewDecoder(resp.Body).Decode(&result)
		fmt.Fprintf(os.Stderr, "Error: %s\n", result["error"])
		os.Exit(1)
	}
	fmt.Printf("ISM retry reset for index %q\n", *indexName)
}
