package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type client struct {
	baseURL string
	http    *http.Client
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	c := client{
		baseURL: strings.TrimRight(env("TYPE_REGISTRY_URL", "http://127.0.0.1:8080"), "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}

	var err error
	switch os.Args[1] {
	case "namespaces":
		err = c.get("/api/v1/namespaces")
	case "status":
		err = status(c, os.Args[2:])
	case "resolve":
		err = resolve(c, os.Args[2:])
	case "search":
		err = search(c, os.Args[2:])
	case "allocate":
		err = allocate(c, os.Args[2:])
	case "revoke":
		err = revoke(c, os.Args[2:])
	case "validate":
		err = validate(c, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func status(c client, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: type-registry status <namespace>")
	}
	return c.get("/api/v1/namespaces/" + url.PathEscape(args[0]))
}

func resolve(c client, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: type-registry resolve <namespace-or-alias>")
	}
	values := url.Values{}
	values.Set("q", strings.Join(args, " "))
	return c.get("/api/v1/namespaces/resolve?" + values.Encode())
}

func search(c client, args []string) error {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	namespace := fs.String("namespace", "", "namespace filter")
	project := fs.String("project", "", "project filter")
	limit := fs.Int("limit", 50, "max results")
	if err := fs.Parse(args); err != nil {
		return err
	}
	query := strings.Join(fs.Args(), " ")
	values := url.Values{}
	values.Set("q", query)
	values.Set("limit", strconv.Itoa(*limit))
	if *namespace != "" {
		values.Set("namespace", *namespace)
	}
	if *project != "" {
		values.Set("project", *project)
	}
	return c.get("/api/v1/types/search?" + values.Encode())
}

func allocate(c client, args []string) error {
	fs := flag.NewFlagSet("allocate", flag.ContinueOnError)
	namespace := fs.String("namespace", "", "namespace")
	project := fs.String("project", "", "project")
	symbol := fs.String("symbol", "", "constant symbol")
	description := fs.String("description", "", "description")
	requirement := fs.String("requirement", "", "requirement/ticket reference")
	requester := fs.String("requester", os.Getenv("USER"), "requester")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return c.post("/api/v1/types/allocate", map[string]any{
		"namespace": *namespace, "project": *project, "symbol": *symbol,
		"description": *description, "requirement": *requirement, "requester": *requester,
	})
}

func revoke(c client, args []string) error {
	fs := flag.NewFlagSet("revoke", flag.ContinueOnError)
	entryID := fs.Int64("id", 0, "type entry id")
	allocationID := fs.String("allocation", "", "allocation id")
	reason := fs.String("reason", "", "revoke reason")
	requester := fs.String("requester", os.Getenv("USER"), "requester")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*entryID > 0) == (*allocationID != "") {
		return errors.New("exactly one of --id or --allocation is required")
	}
	if strings.TrimSpace(*reason) == "" {
		return errors.New("--reason is required")
	}
	payload := map[string]any{
		"requester": *requester,
		"reason":    *reason,
	}
	if *entryID > 0 {
		return c.post("/api/v1/types/"+strconv.FormatInt(*entryID, 10)+"/revoke", payload)
	}
	return c.post("/api/v1/allocations/"+url.PathEscape(*allocationID)+"/revoke", payload)
}

func validate(c client, args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	namespace := fs.String("namespace", "", "namespace")
	value := fs.Int64("value", 0, "type value")
	symbol := fs.String("symbol", "", "constant symbol")
	project := fs.String("project", "", "project")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return c.post("/api/v1/types/validate", map[string]any{
		"namespace": *namespace, "value": *value, "symbol": *symbol, "project": *project,
	})
}

func (c client) get(path string) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req)
}

func (c client) post(path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

func (c client) do(req *http.Request) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(content)))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, content, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(content))
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func usage() {
	fmt.Fprintln(os.Stderr, `type-registry commands:
  namespaces
  status <namespace>
  resolve <namespace-or-alias>
  search [--namespace N] [--project P] <keyword>
  allocate --namespace N [--project P] [--symbol SYMBOL] [--description TEXT] [--requirement REF] [--requester USER]
  revoke (--id ID | --allocation ALLOCATION_ID) --reason TEXT [--requester USER]
  validate --namespace N --value V [--symbol SYMBOL] [--project P]`)
}
