package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockErrlyServer creates a test HTTP server that returns canned JSON for Errly API paths.
func mockErrlyServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// defaultMock returns a mock server with reasonable responses for all endpoints.
func defaultMock(t *testing.T) *httptest.Server {
	return mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/issues/search"):
			fmt.Fprintf(w, `{"issues":[{"id":"abc","title":"Test error"}]}`)
		case strings.HasSuffix(r.URL.Path, "/events"):
			fmt.Fprintf(w, `{"events":[{"id":"evt1","level":"error"}]}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprintf(w, `{"status":"resolved"}`)
		case r.URL.Path == "/api/v1/issues/abc":
			fmt.Fprintf(w, `{"id":"abc","title":"Test error","status":"unresolved"}`)
		case r.URL.Path == "/api/v1/issues":
			fmt.Fprintf(w, `{"issues":[{"id":"abc","title":"Test error"}],"total":1}`)
		case r.URL.Path == "/api/v1/stats":
			fmt.Fprintf(w, `{"total_issues":5,"unresolved_issues":3,"total_events":42,"events_last_24h":7}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, `{"error":"not found"}`)
		}
	}))
}

// newMCPServer creates an MCPServer pointing at the given test server.
func newMCPServer(srv *httptest.Server) *MCPServer {
	return &MCPServer{client: NewErrlyClient(srv.URL, "test-key")}
}

// callTool is a helper that invokes a tool and unmarshals text content.
func callTool(t *testing.T, s *MCPServer, name string, args map[string]any) (string, *RPCErr) {
	t.Helper()
	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
	}
	params := ToolCallParams{Name: name, Arguments: args}
	raw, _ := json.Marshal(params)
	req.Params = raw

	resp := s.handle(req)
	if resp.Error != nil {
		return "", resp.Error
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	content, ok := result["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("expected content array in result")
	}
	text, _ := content[0]["text"].(string)
	return text, nil
}

// ── initialize ────────────────────────────────────────────────────────────────

func TestHandle_Initialize(t *testing.T) {
	s := newMCPServer(defaultMock(t))
	resp := s.handle(MCPRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatal("expected result map")
	}
	if result["protocolVersion"] != "2024-11-05" {
		t.Errorf("unexpected protocolVersion: %v", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "errly-mcp" {
		t.Errorf("expected serverInfo.name=errly-mcp, got %v", info["name"])
	}
}

// ── tools/list ────────────────────────────────────────────────────────────────

func TestHandle_ToolsList(t *testing.T) {
	s := newMCPServer(defaultMock(t))
	resp := s.handle(MCPRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list"})

	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}
	result, _ := resp.Result.(map[string]any)
	toolList, _ := result["tools"].([]map[string]any)
	if len(toolList) != 6 {
		t.Errorf("expected 6 tools, got %d", len(toolList))
	}
}

// ── list_issues ───────────────────────────────────────────────────────────────

func TestListIssues_NoFilters(t *testing.T) {
	var gotPath string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		w.Write([]byte(`{"issues":[],"total":0}`))
	}))
	s := newMCPServer(srv)

	text, rpcErr := callTool(t, s, "list_issues", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	if !strings.Contains(gotPath, "/api/v1/issues") {
		t.Errorf("expected call to /api/v1/issues, got %q", gotPath)
	}
	if text == "" {
		t.Error("expected non-empty text response")
	}
}

func TestListIssues_WithStatusFilter(t *testing.T) {
	var gotQuery string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"issues":[],"total":0}`))
	}))
	s := newMCPServer(srv)

	callTool(t, s, "list_issues", map[string]any{"status": "unresolved"})
	if !strings.Contains(gotQuery, "status=unresolved") {
		t.Errorf("expected status=unresolved in query, got %q", gotQuery)
	}
}

func TestListIssues_WithProjectAndEnvFilter(t *testing.T) {
	var gotQuery string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"issues":[],"total":0}`))
	}))
	s := newMCPServer(srv)

	callTool(t, s, "list_issues", map[string]any{
		"project": "my-api",
		"env":     "production",
	})
	if !strings.Contains(gotQuery, "project=my-api") {
		t.Errorf("expected project=my-api, got %q", gotQuery)
	}
	if !strings.Contains(gotQuery, "env=production") {
		t.Errorf("expected env=production, got %q", gotQuery)
	}
}

// ── get_issue ─────────────────────────────────────────────────────────────────

func TestGetIssue_Success(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	text, rpcErr := callTool(t, s, "get_issue", map[string]any{"issue_id": "abc"})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	if !strings.Contains(text, "abc") {
		t.Errorf("expected issue id in response, got %q", text)
	}
}

func TestGetIssue_MissingIssueID(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	_, rpcErr := callTool(t, s, "get_issue", map[string]any{})
	if rpcErr == nil {
		t.Error("expected rpc error for missing issue_id")
	}
	if rpcErr.Code != -32603 {
		t.Errorf("expected code -32603, got %d", rpcErr.Code)
	}
	if !strings.Contains(rpcErr.Message, "issue_id") {
		t.Errorf("expected 'issue_id' in error message, got %q", rpcErr.Message)
	}
}

// ── get_issue_events ──────────────────────────────────────────────────────────

func TestGetIssueEvents_Success(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	text, rpcErr := callTool(t, s, "get_issue_events", map[string]any{"issue_id": "abc"})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	if !strings.Contains(text, "events") {
		t.Errorf("expected events in response, got %q", text)
	}
}

func TestGetIssueEvents_MissingIssueID(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	_, rpcErr := callTool(t, s, "get_issue_events", map[string]any{})
	if rpcErr == nil {
		t.Error("expected rpc error for missing issue_id")
	}
}

// ── search_issues ─────────────────────────────────────────────────────────────

func TestSearchIssues_Success(t *testing.T) {
	var gotQuery string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Write([]byte(`{"issues":[{"id":"abc","title":"payment error"}]}`))
	}))
	s := newMCPServer(srv)

	text, rpcErr := callTool(t, s, "search_issues", map[string]any{"query": "payment"})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	if gotQuery != "payment" {
		t.Errorf("expected q=payment, got %q", gotQuery)
	}
	if !strings.Contains(text, "payment") {
		t.Errorf("expected 'payment' in response, got %q", text)
	}
}

func TestSearchIssues_MissingQuery(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	_, rpcErr := callTool(t, s, "search_issues", map[string]any{})
	if rpcErr == nil {
		t.Error("expected rpc error for missing query")
	}
	if !strings.Contains(rpcErr.Message, "query") {
		t.Errorf("expected 'query' in error message, got %q", rpcErr.Message)
	}
}

// ── resolve_issue ─────────────────────────────────────────────────────────────

func TestResolveIssue_DefaultsToResolved(t *testing.T) {
	var gotBody string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			buf := make([]byte, 512)
			n, _ := r.Body.Read(buf)
			gotBody = string(buf[:n])
		}
		w.Write([]byte(`{"status":"resolved"}`))
	}))
	s := newMCPServer(srv)

	_, rpcErr := callTool(t, s, "resolve_issue", map[string]any{"issue_id": "abc"})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	if !strings.Contains(gotBody, `"resolved"`) {
		t.Errorf("expected default status 'resolved' in request body, got %q", gotBody)
	}
}

func TestResolveIssue_CustomStatus(t *testing.T) {
	var gotBody string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			buf := make([]byte, 512)
			n, _ := r.Body.Read(buf)
			gotBody = string(buf[:n])
		}
		w.Write([]byte(`{"status":"ignored"}`))
	}))
	s := newMCPServer(srv)

	callTool(t, s, "resolve_issue", map[string]any{"issue_id": "abc", "status": "ignored"})
	if !strings.Contains(gotBody, `"ignored"`) {
		t.Errorf("expected status 'ignored' in request body, got %q", gotBody)
	}
}

func TestResolveIssue_MissingIssueID(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	_, rpcErr := callTool(t, s, "resolve_issue", map[string]any{})
	if rpcErr == nil {
		t.Error("expected rpc error for missing issue_id")
	}
}

// ── get_stats ─────────────────────────────────────────────────────────────────

func TestGetStats_NoProject(t *testing.T) {
	var gotQuery string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"total_issues":5,"unresolved_issues":3,"total_events":42,"events_last_24h":7}`))
	}))
	s := newMCPServer(srv)

	text, rpcErr := callTool(t, s, "get_stats", map[string]any{})
	if rpcErr != nil {
		t.Fatalf("unexpected rpc error: %v", rpcErr)
	}
	if gotQuery != "" {
		t.Errorf("expected no query params when no project, got %q", gotQuery)
	}
	if !strings.Contains(text, "total_issues") {
		t.Errorf("expected stats in response, got %q", text)
	}
}

func TestGetStats_WithProjectFilter(t *testing.T) {
	var gotQuery string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"total_issues":1}`))
	}))
	s := newMCPServer(srv)

	callTool(t, s, "get_stats", map[string]any{"project": "payments"})
	if !strings.Contains(gotQuery, "project=payments") {
		t.Errorf("expected project=payments in query, got %q", gotQuery)
	}
}

// ── unknown tool ──────────────────────────────────────────────────────────────

func TestUnknownTool_ReturnsError(t *testing.T) {
	s := newMCPServer(defaultMock(t))

	_, rpcErr := callTool(t, s, "delete_everything", map[string]any{})
	if rpcErr == nil {
		t.Error("expected rpc error for unknown tool")
	}
	if rpcErr.Code != -32603 {
		t.Errorf("expected code -32603, got %d", rpcErr.Code)
	}
}

// ── notifications/initialized ─────────────────────────────────────────────────

func TestHandle_NotificationsInitialized_NoResponse(t *testing.T) {
	s := newMCPServer(defaultMock(t))
	resp := s.handle(MCPRequest{JSONRPC: "2.0", Method: "notifications/initialized"})

	// Notifications must not generate a response (empty JSONRPC)
	if resp.JSONRPC != "" {
		t.Errorf("expected empty response for notification, got JSONRPC=%q", resp.JSONRPC)
	}
}

// ── unknown method ────────────────────────────────────────────────────────────

func TestHandle_UnknownMethod(t *testing.T) {
	s := newMCPServer(defaultMock(t))
	resp := s.handle(MCPRequest{JSONRPC: "2.0", ID: 1, Method: "unknown/method"})

	if resp.Error == nil {
		t.Error("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("expected code -32601 (method not found), got %d", resp.Error.Code)
	}
}

// ── API key forwarded ─────────────────────────────────────────────────────────

func TestAPIKey_ForwardedToErrlyServer(t *testing.T) {
	var gotKey string
	srv := mockErrlyServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Errly-Key")
		w.Write([]byte(`{"issues":[],"total":0}`))
	}))

	s := &MCPServer{client: NewErrlyClient(srv.URL, "mcp-api-key")}
	callTool(t, s, "list_issues", map[string]any{})

	if gotKey != "mcp-api-key" {
		t.Errorf("expected X-Errly-Key='mcp-api-key', got %q", gotKey)
	}
}
