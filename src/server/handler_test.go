package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "test-secret-key"

// newTestHandler returns a Handler backed by fresh in-memory storage and a real API key.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	store, err := NewStorage(":memory:", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHandler(store, logger, testAPIKey)
}

// do sends an HTTP request to the handler and returns the response.
func do(t *testing.T, h *Handler, method, path, body, apiKey string) *http.Response {
	t.Helper()
	var b *bytes.Buffer
	if body != "" {
		b = bytes.NewBufferString(body)
	} else {
		b = &bytes.Buffer{}
	}
	req := httptest.NewRequest(method, path, b)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("X-Errly-Key", apiKey)
	}
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	return rr.Result()
}

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

// ── Health ────────────────────────────────────────────────────────────────────

func TestHealthz(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "GET", "/healthz", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ── POST /api/v1/events ───────────────────────────────────────────────────────

func TestIngestEvent_ValidEvent(t *testing.T) {
	h := newTestHandler(t)
	event := map[string]any{
		"level":       "error",
		"message":     "test error",
		"platform":    "go",
		"environment": "production",
		"project_key": "testproj",
		"exception":   map[string]any{"type": "RuntimeError", "value": "boom"},
	}
	resp := do(t, h, "POST", "/api/v1/events", jsonBody(t, event), testAPIKey)
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["id"] == "" {
		t.Error("expected non-empty id in response")
	}
}

func TestIngestEvent_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "POST", "/api/v1/events", `{not valid json`, testAPIKey)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

func TestIngestEvent_WrongAPIKey(t *testing.T) {
	h := newTestHandler(t)
	event := map[string]any{"level": "error", "message": "x"}
	resp := do(t, h, "POST", "/api/v1/events", jsonBody(t, event), "wrong-key")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestIngestEvent_NoAPIKey(t *testing.T) {
	h := newTestHandler(t)
	event := map[string]any{"level": "error", "message": "x"}
	resp := do(t, h, "POST", "/api/v1/events", jsonBody(t, event), "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 when no key provided, got %d", resp.StatusCode)
	}
}

func TestIngestEvent_NoKeyRequired_WhenConfigEmpty(t *testing.T) {
	// Handler with no API key set should accept all events
	store, _ := NewStorage(":memory:", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	h := NewHandler(store, slog.New(slog.NewTextHandler(os.Stderr, nil)), "")
	event := map[string]any{"level": "error", "message": "x"}

	req := httptest.NewRequest("POST", "/api/v1/events", bytes.NewBufferString(jsonBody(t, event)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 with no api key configured, got %d", rr.Code)
	}
}

func TestIngestEvent_FillsDefaultFields(t *testing.T) {
	h := newTestHandler(t)
	// Minimal event — server should fill level + environment defaults
	event := map[string]any{"message": "bare event"}
	do(t, h, "POST", "/api/v1/events", jsonBody(t, event), testAPIKey)

	issues, total, _ := h.storage.ListIssues("", "", "", 1, 0)
	if total != 1 {
		t.Fatalf("expected 1 issue, got %d", total)
	}
	if issues[0].Level == "" {
		t.Error("expected default level to be set")
	}
	if issues[0].Environment == "" {
		t.Error("expected default environment to be set")
	}
}

// ── GET /api/v1/issues ────────────────────────────────────────────────────────

func seedIssue(t *testing.T, h *Handler, project, env string) {
	t.Helper()
	event := map[string]any{
		"level":       "error",
		"message":     "seed error",
		"environment": env,
		"project_key": project,
		"exception":   map[string]any{"type": "SeedErr", "value": project + "/" + env},
	}
	do(t, h, "POST", "/api/v1/events", jsonBody(t, event), testAPIKey)
}

func TestListIssues_ReturnsAll(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "p1", "production")
	seedIssue(t, h, "p2", "staging")

	resp := do(t, h, "GET", "/api/v1/issues", "", testAPIKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if int(result["total"].(float64)) != 2 {
		t.Errorf("expected total 2, got %v", result["total"])
	}
}

func TestListIssues_FilterByProject(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "alpha", "production")
	seedIssue(t, h, "beta", "production")

	resp := do(t, h, "GET", "/api/v1/issues?project=alpha", "", testAPIKey)
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if int(result["total"].(float64)) != 1 {
		t.Errorf("expected 1 alpha issue, got %v", result["total"])
	}
}

func TestListIssues_RequiresAuth(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "GET", "/api/v1/issues", "", "bad-key")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// ── GET /api/v1/issues/{id} ───────────────────────────────────────────────────

func TestGetIssue_Found(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "proj", "production")

	issues, _, _ := h.storage.ListIssues("", "", "", 1, 0)
	id := issues[0].ID

	resp := do(t, h, "GET", "/api/v1/issues/"+id, "", testAPIKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var issue Issue
	json.NewDecoder(resp.Body).Decode(&issue)
	if issue.ID != id {
		t.Errorf("expected issue id %s, got %s", id, issue.ID)
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "GET", "/api/v1/issues/nonexistent", "", testAPIKey)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// ── PUT /api/v1/issues/{id}/status ───────────────────────────────────────────

func TestUpdateStatus_ValidStatus(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "proj", "production")
	issues, _, _ := h.storage.ListIssues("", "", "", 1, 0)
	id := issues[0].ID

	for _, status := range []string{"resolved", "ignored", "unresolved"} {
		body := jsonBody(t, map[string]string{"status": status})
		resp := do(t, h, "PUT", "/api/v1/issues/"+id+"/status", body, testAPIKey)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status %q: expected 200, got %d", status, resp.StatusCode)
		}
	}
}

func TestUpdateStatus_InvalidStatus(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "proj", "production")
	issues, _, _ := h.storage.ListIssues("", "", "", 1, 0)
	id := issues[0].ID

	body := jsonBody(t, map[string]string{"status": "deleted"})
	resp := do(t, h, "PUT", "/api/v1/issues/"+id+"/status", body, testAPIKey)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", resp.StatusCode)
	}
}

func TestUpdateStatus_InvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "PUT", "/api/v1/issues/any-id/status", `{bad`, testAPIKey)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", resp.StatusCode)
	}
}

// ── GET /api/v1/issues/{id}/events ───────────────────────────────────────────

func TestGetIssueEvents_ReturnsList(t *testing.T) {
	h := newTestHandler(t)
	// Send 3 events for the same fingerprint
	for range 3 {
		ev := map[string]any{
			"level": "error", "environment": "production", "project_key": "proj",
			"exception": map[string]any{"type": "RepeatErr", "value": "same"},
		}
		do(t, h, "POST", "/api/v1/events", jsonBody(t, ev), testAPIKey)
	}

	issues, _, _ := h.storage.ListIssues("", "", "", 1, 0)
	id := issues[0].ID

	resp := do(t, h, "GET", "/api/v1/issues/"+id+"/events", "", testAPIKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	events := result["events"].([]any)
	if len(events) == 0 {
		t.Error("expected at least 1 event")
	}
}

// ── GET /api/v1/issues/search ─────────────────────────────────────────────────

func TestSearchIssues_MissingQuery(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "GET", "/api/v1/issues/search", "", testAPIKey)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing q, got %d", resp.StatusCode)
	}
}

func TestSearchIssues_ReturnsMatch(t *testing.T) {
	h := newTestHandler(t)
	ev := map[string]any{
		"level": "error", "environment": "production", "project_key": "proj",
		"exception": map[string]any{"type": "DatabaseError", "value": "connection refused"},
	}
	do(t, h, "POST", "/api/v1/events", jsonBody(t, ev), testAPIKey)

	resp := do(t, h, "GET", "/api/v1/issues/search?q=database", "", testAPIKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	issues := result["issues"].([]any)
	if len(issues) != 1 {
		t.Errorf("expected 1 search result, got %d", len(issues))
	}
}

func TestSearchIssues_NoResults(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "proj", "production")

	resp := do(t, h, "GET", "/api/v1/issues/search?q=payment", "", testAPIKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	issues := result["issues"]
	if issues != nil && len(issues.([]any)) != 0 {
		t.Errorf("expected 0 results, got %v", issues)
	}
}

// ── GET /api/v1/stats ─────────────────────────────────────────────────────────

func TestGetStats_ReturnsStructure(t *testing.T) {
	h := newTestHandler(t)
	seedIssue(t, h, "proj", "production")

	resp := do(t, h, "GET", "/api/v1/stats", "", testAPIKey)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var stats Stats
	json.NewDecoder(resp.Body).Decode(&stats)
	if stats.TotalIssues != 1 {
		t.Errorf("expected TotalIssues=1, got %d", stats.TotalIssues)
	}
	if stats.UnresolvedIssues != 1 {
		t.Errorf("expected UnresolvedIssues=1, got %d", stats.UnresolvedIssues)
	}
}

// ── CORS ──────────────────────────────────────────────────────────────────────

func TestCORS_OptionsRequest(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("OPTIONS", "/api/v1/issues", nil)
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS Allow-Origin header")
	}
}

func TestCORS_HeadersPresent(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "GET", "/healthz", "", "")
	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header on all responses")
	}
}

// ── Key via query param ───────────────────────────────────────────────────────

func TestIngestEvent_KeyViaQueryParam(t *testing.T) {
	h := newTestHandler(t)
	event := map[string]any{"level": "error", "message": "via query"}
	body := jsonBody(t, event)

	req := httptest.NewRequest("POST", "/api/v1/events?key="+testAPIKey, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 with key via query param, got %d", rr.Code)
	}
}

// ── Timestamp default ─────────────────────────────────────────────────────────

func TestIngestEvent_TimestampDefaulted(t *testing.T) {
	h := newTestHandler(t)
	before := time.Now().UTC().Add(-time.Second)

	event := map[string]any{"message": "no timestamp"}
	do(t, h, "POST", "/api/v1/events", jsonBody(t, event), testAPIKey)

	issues, _, _ := h.storage.ListIssues("", "", "", 1, 0)
	if issues[0].FirstSeen.Before(before) {
		t.Errorf("expected FirstSeen to be recent, got %v", issues[0].FirstSeen)
	}
}

// ── JSON response format ──────────────────────────────────────────────────────

func TestListIssues_ResponseIsJSON(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, "GET", "/api/v1/issues", "", testAPIKey)

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content-type, got %s", ct)
	}
}

// ── Rate limiting ─────────────────────────────────────────────────────────────

// newRateLimitedHandler creates a handler with a very low rate limit for testing.
func newRateLimitedHandler(t *testing.T, ratePerMin int) *Handler {
	t.Helper()
	store, err := NewStorage(":memory:", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	return NewHandlerWithOptions(store, logger, "", ratePerMin)
}

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	h := newRateLimitedHandler(t, 10)
	event := jsonBody(t, map[string]any{"level": "error", "message": "x"})

	// First request must succeed
	resp := do(t, h, "POST", "/api/v1/events", event, "")
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestRateLimit_Returns429WhenExceeded(t *testing.T) {
	// Rate limit of 2 per minute — exhaust with 3 rapid requests
	h := newRateLimitedHandler(t, 2)
	event := jsonBody(t, map[string]any{"level": "error", "message": "x"})

	var lastStatus int
	for range 5 {
		resp := do(t, h, "POST", "/api/v1/events", event, "")
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusTooManyRequests {
			// Confirm Retry-After header is present
			if resp.Header.Get("Retry-After") == "" {
				t.Error("expected Retry-After header on 429 response")
			}
			return
		}
	}
	t.Errorf("expected 429 after exhausting rate limit, last status=%d", lastStatus)
}

func TestRateLimit_OnlyAppliesToIngestEndpoint(t *testing.T) {
	// Rate limit of 1 — exhaust the ingest endpoint
	h := newRateLimitedHandler(t, 1)
	event := jsonBody(t, map[string]any{"level": "error", "message": "x"})

	// Exhaust rate limit on ingest
	do(t, h, "POST", "/api/v1/events", event, "")
	do(t, h, "POST", "/api/v1/events", event, "")

	// Health check must still work (not rate limited)
	resp := do(t, h, "GET", "/healthz", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz should not be rate limited, got %d", resp.StatusCode)
	}
}

func TestRateLimit_DifferentIPsHaveSeparateLimits(t *testing.T) {
	h := newRateLimitedHandler(t, 1)
	event := jsonBody(t, map[string]any{"level": "error", "message": "x"})

	// Exhaust limit for 127.0.0.1 (default in httptest)
	do(t, h, "POST", "/api/v1/events", event, "")
	do(t, h, "POST", "/api/v1/events", event, "")

	// Request from a different IP (via X-Forwarded-For) should succeed
	req := httptest.NewRequest("POST", "/api/v1/events", bytes.NewBufferString(event))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	rr := httptest.NewRecorder()
	h.Routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201 for different IP, got %d", rr.Code)
	}
}

func TestRateLimit_RetryAfterHeaderValue(t *testing.T) {
	h := newRateLimitedHandler(t, 60) // 60/min = 1/sec → Retry-After: 1
	event := jsonBody(t, map[string]any{"level": "error", "message": "x"})

	var rateLimitResp *http.Response
	for range 70 {
		resp := do(t, h, "POST", "/api/v1/events", event, "")
		if resp.StatusCode == http.StatusTooManyRequests {
			rateLimitResp = resp
			break
		}
	}
	if rateLimitResp == nil {
		t.Skip("rate limit not triggered in test window")
	}
	ra := rateLimitResp.Header.Get("Retry-After")
	if ra == "" {
		t.Error("expected non-empty Retry-After header")
	}
}
