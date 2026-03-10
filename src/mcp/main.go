package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// MCP JSON-RPC types
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type MCPResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      any     `json:"id"`
	Result  any     `json:"result,omitempty"`
	Error   *RPCErr `json:"error,omitempty"`
}

type RPCErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// Errly API client
type ErrlyClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewErrlyClient(baseURL, apiKey string) *ErrlyClient {
	return &ErrlyClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

func (c *ErrlyClient) get(path string, params url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, _ := http.NewRequest("GET", u, nil)
	if c.apiKey != "" {
		req.Header.Set("X-Errly-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *ErrlyClient) put(path string, body map[string]any) ([]byte, error) {
	data, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", c.baseURL+path, strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Errly-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// MCP Server
type MCPServer struct {
	client *ErrlyClient
}

func (s *MCPServer) handle(req MCPRequest) MCPResponse {
	switch req.Method {
	case "initialize":
		// Mirror the client's protocol version if it's newer than our minimum
		protoVersion := "2025-03-26"
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if req.Params != nil {
			json.Unmarshal(req.Params, &initParams)
			if initParams.ProtocolVersion != "" {
				protoVersion = initParams.ProtocolVersion
			}
		}
		return MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protoVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{"name": "errly-mcp", "version": "1.0.0"},
		}}

	case "ping":
		return MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	case "tools/list":
		return MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"tools": tools,
		}}

	case "tools/call":
		var params ToolCallParams
		json.Unmarshal(req.Params, &params)
		result, err := s.callTool(params.Name, params.Arguments)
		if err != nil {
			return MCPResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCErr{Code: -32603, Message: err.Error()}}
		}
		return MCPResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": result}},
		}}

	case "notifications/initialized", "notifications/cancelled":
		return MCPResponse{} // notifications — no response
	}

	return MCPResponse{JSONRPC: "2.0", ID: req.ID, Error: &RPCErr{Code: -32601, Message: "method not found"}}
}

func (s *MCPServer) callTool(name string, args map[string]any) (string, error) {
	str := func(k string) string {
		v, _ := args[k].(string)
		return v
	}

	switch name {
	case "list_issues":
		params := url.Values{}
		if v := str("status"); v != "" {
			params.Set("status", v)
		}
		if v := str("project"); v != "" {
			params.Set("project", v)
		}
		if v := str("env"); v != "" {
			params.Set("env", v)
		}
		params.Set("limit", "20")
		data, err := s.client.get("/api/v1/issues", params)
		if err != nil {
			return "", fmt.Errorf("fetch issues: %w", err)
		}
		return prettify(data), nil

	case "get_issue":
		id := str("issue_id")
		if id == "" {
			return "", fmt.Errorf("issue_id is required")
		}
		data, err := s.client.get("/api/v1/issues/"+id, nil)
		if err != nil {
			return "", err
		}
		return prettify(data), nil

	case "get_issue_events":
		id := str("issue_id")
		if id == "" {
			return "", fmt.Errorf("issue_id is required")
		}
		data, err := s.client.get("/api/v1/issues/"+id+"/events", url.Values{"limit": {"5"}})
		if err != nil {
			return "", err
		}
		return prettify(data), nil

	case "search_issues":
		q := str("query")
		if q == "" {
			return "", fmt.Errorf("query is required")
		}
		data, err := s.client.get("/api/v1/issues/search", url.Values{"q": {q}})
		if err != nil {
			return "", err
		}
		return prettify(data), nil

	case "resolve_issue":
		id := str("issue_id")
		status := str("status")
		if id == "" {
			return "", fmt.Errorf("issue_id is required")
		}
		if status == "" {
			status = "resolved"
		}
		data, err := s.client.put("/api/v1/issues/"+id+"/status", map[string]any{"status": status})
		if err != nil {
			return "", err
		}
		return string(data), nil

	case "get_stats":
		params := url.Values{}
		if v := str("project"); v != "" {
			params.Set("project", v)
		}
		data, err := s.client.get("/api/v1/stats", params)
		if err != nil {
			return "", err
		}
		return prettify(data), nil
	}

	return "", fmt.Errorf("unknown tool: %s", name)
}

func prettify(data []byte) string {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return string(data)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

var tools = []map[string]any{
	{
		"name":        "list_issues",
		"description": "List error issues from Errly. Filter by status (unresolved/resolved/ignored), project, or environment.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":  map[string]any{"type": "string", "description": "Filter by status: unresolved, resolved, ignored"},
				"project": map[string]any{"type": "string", "description": "Filter by project key"},
				"env":     map[string]any{"type": "string", "description": "Filter by environment (e.g. production, staging)"},
			},
		},
	},
	{
		"name":        "get_issue",
		"description": "Get detailed info about a specific issue including stack trace.",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string", "description": "The issue ID"},
			},
		},
	},
	{
		"name":        "get_issue_events",
		"description": "Get recent events (occurrences) for a specific issue with full stack traces.",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string", "description": "The issue ID"},
			},
		},
	},
	{
		"name":        "search_issues",
		"description": "Search issues by keyword in the error title or message.",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "Search keyword"},
			},
		},
	},
	{
		"name":        "resolve_issue",
		"description": "Update the status of an issue (resolve, ignore, or re-open).",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"issue_id"},
			"properties": map[string]any{
				"issue_id": map[string]any{"type": "string", "description": "The issue ID"},
				"status":   map[string]any{"type": "string", "description": "New status: resolved, unresolved, ignored (default: resolved)"},
			},
		},
	},
	{
		"name":        "get_stats",
		"description": "Get error monitoring statistics: total issues, unresolved count, events in last 24h.",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string", "description": "Filter by project key (optional)"},
			},
		},
	},
}

func main() {
	baseURL := getEnv("ERRLY_URL", "http://localhost:5080")
	apiKey := getEnv("ERRLY_API_KEY", "")

	server := &MCPServer{
		client: NewErrlyClient(baseURL, apiKey),
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	encoder := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		resp := server.handle(req)

		// Don't respond to notifications
		if resp.JSONRPC == "" {
			continue
		}

		encoder.Encode(resp)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
