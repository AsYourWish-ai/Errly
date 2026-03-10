package errly

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// captureServer starts an httptest server that records received events.
// Returns the server and a function to retrieve collected events.
func captureServer(t *testing.T) (*httptest.Server, func() []Event) {
	t.Helper()
	var mu sync.Mutex
	var received []Event

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var ev Event
		if err := json.Unmarshal(body, &ev); err == nil {
			mu.Lock()
			received = append(received, ev)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"ok"}`))
	}))

	t.Cleanup(srv.Close)

	return srv, func() []Event {
		mu.Lock()
		defer mu.Unlock()
		out := make([]Event, len(received))
		copy(out, received)
		return out
	}
}

// newClient creates a client pointing at the test server.
func newClient(t *testing.T, srv *httptest.Server, opts ...Option) *Client {
	t.Helper()
	return New(srv.URL, "test-key", opts...)
}

// ── New / defaults ─────────────────────────────────────────────────────────────

func TestNew_Defaults(t *testing.T) {
	srv, _ := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	if c.environment != "production" {
		t.Errorf("expected default environment 'production', got %q", c.environment)
	}
	if cap(c.queue) != 512 {
		t.Errorf("expected queue cap 512, got %d", cap(c.queue))
	}
}

func TestNew_WithOptions(t *testing.T) {
	srv, _ := captureServer(t)
	c := New(srv.URL, "key",
		WithProject("my-svc"),
		WithEnvironment("staging"),
		WithRelease("v2.0.0"),
	)
	defer c.Flush()

	if c.project != "my-svc" {
		t.Errorf("expected project 'my-svc', got %q", c.project)
	}
	if c.environment != "staging" {
		t.Errorf("expected environment 'staging', got %q", c.environment)
	}
	if c.release != "v2.0.0" {
		t.Errorf("expected release 'v2.0.0', got %q", c.release)
	}
}

// ── CaptureError ──────────────────────────────────────────────────────────────

func TestCaptureError_NilReturnsEmpty(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	id := c.CaptureError(context.Background(), nil)
	if id != "" {
		t.Errorf("expected empty id for nil error, got %q", id)
	}
	c.Flush()
	if len(getEvents()) != 0 {
		t.Error("expected no events for nil error")
	}
}

func TestCaptureError_ReturnsNonEmptyID(t *testing.T) {
	srv, _ := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	id := c.CaptureError(context.Background(), errors.New("boom"))
	if id == "" {
		t.Error("expected non-empty event id")
	}
}

func TestCaptureError_EventShape(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv,
		WithProject("my-proj"),
		WithEnvironment("test"),
		WithRelease("v1"),
	)
	c.CaptureError(context.Background(), fmt.Errorf("disk full"))
	c.Flush()

	events := getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]

	if ev.Level != "error" {
		t.Errorf("expected level 'error', got %q", ev.Level)
	}
	if ev.Platform != "go" {
		t.Errorf("expected platform 'go', got %q", ev.Platform)
	}
	if ev.Environment != "test" {
		t.Errorf("expected environment 'test', got %q", ev.Environment)
	}
	if ev.ProjectKey != "my-proj" {
		t.Errorf("expected project 'my-proj', got %q", ev.ProjectKey)
	}
	if ev.Exception == nil {
		t.Fatal("expected exception to be set")
	}
	if ev.Exception.Value != "disk full" {
		t.Errorf("expected exception value 'disk full', got %q", ev.Exception.Value)
	}
	if len(ev.Stacktrace) == 0 {
		t.Error("expected non-empty stacktrace")
	}
}

func TestCaptureError_WithExtra(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	c.CaptureError(context.Background(), errors.New("err"), map[string]any{"key": "val"})
	c.Flush()

	events := getEvents()
	if events[0].Extra["key"] != "val" {
		t.Errorf("expected extra key=val, got %v", events[0].Extra)
	}
}

// ── CaptureMessage ────────────────────────────────────────────────────────────

func TestCaptureMessage_LevelAndMessage(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	c.CaptureMessage(context.Background(), "warning", "slow query detected")
	c.Flush()

	events := getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Level != "warning" {
		t.Errorf("expected level 'warning', got %q", events[0].Level)
	}
	if events[0].Message != "slow query detected" {
		t.Errorf("expected message 'slow query detected', got %q", events[0].Message)
	}
}

// ── SetUser ───────────────────────────────────────────────────────────────────

func TestSetUser_AttachedToEvent(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	c.SetUser(&UserInfo{ID: "u1", Email: "u@example.com"})
	c.CaptureError(context.Background(), errors.New("err"))
	c.Flush()

	events := getEvents()
	if events[0].User == nil {
		t.Fatal("expected user to be set on event")
	}
	if events[0].User.ID != "u1" {
		t.Errorf("expected user id 'u1', got %q", events[0].User.ID)
	}
	if events[0].User.Email != "u@example.com" {
		t.Errorf("expected user email, got %q", events[0].User.Email)
	}
}

func TestSetUser_Nil_DoesNotPanic(t *testing.T) {
	srv, _ := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()
	c.SetUser(nil)
	c.CaptureError(context.Background(), errors.New("err")) // must not panic
}

// ── AddBreadcrumb ─────────────────────────────────────────────────────────────

func TestAddBreadcrumb_AttachedToEvent(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	c.AddBreadcrumb("db", "query executed", "info")
	c.CaptureError(context.Background(), errors.New("err"))
	c.Flush()

	events := getEvents()
	if len(events[0].Breadcrumbs) != 1 {
		t.Fatalf("expected 1 breadcrumb, got %d", len(events[0].Breadcrumbs))
	}
	bc := events[0].Breadcrumbs[0]
	if bc.Category != "db" {
		t.Errorf("expected category 'db', got %q", bc.Category)
	}
	if bc.Message != "query executed" {
		t.Errorf("expected message 'query executed', got %q", bc.Message)
	}
}

func TestAddBreadcrumb_CappedAt50(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)

	for i := 0; i < 55; i++ {
		c.AddBreadcrumb("cat", fmt.Sprintf("crumb-%d", i), "info")
	}
	c.CaptureError(context.Background(), errors.New("err"))
	c.Flush()

	events := getEvents()
	if len(events[0].Breadcrumbs) != 50 {
		t.Errorf("expected 50 breadcrumbs (capped), got %d", len(events[0].Breadcrumbs))
	}
	// Verify we kept the last 50 (crumb-5 through crumb-54)
	if events[0].Breadcrumbs[0].Message != "crumb-5" {
		t.Errorf("expected first kept crumb to be crumb-5, got %q", events[0].Breadcrumbs[0].Message)
	}
}

// ── Middleware ────────────────────────────────────────────────────────────────

func TestMiddleware_PanicCapturedAndHTTP500(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went very wrong")
	})

	wrapped := c.Middleware(panicHandler)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	c.Flush()
	events := getEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event for panic, got %d", len(events))
	}
	if events[0].Level != "fatal" {
		t.Errorf("expected level 'fatal', got %q", events[0].Level)
	}
	if events[0].Exception == nil || events[0].Exception.Type != "panic" {
		t.Errorf("expected panic exception, got %+v", events[0].Exception)
	}
}

func TestMiddleware_PanicWithErrorType(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("typed error panic"))
	})

	wrapped := c.Middleware(panicHandler)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	c.Flush()
	events := getEvents()
	if events[0].Exception.Value != "typed error panic" {
		t.Errorf("expected panic value 'typed error panic', got %q", events[0].Exception.Value)
	}
}

func TestMiddleware_NormalRequestPassesThrough(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	})

	wrapped := c.Middleware(okHandler)
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	c.Flush()
	if len(getEvents()) != 0 {
		t.Error("expected no events for normal request")
	}
}

func TestMiddleware_PanicIncludesRequestInfo(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)
	defer c.Flush()

	wrapped := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest("POST", "/api/process", nil)
	req.Header.Set("User-Agent", "test-agent")
	rr := httptest.NewRecorder()
	wrapped.ServeHTTP(rr, req)

	c.Flush()
	events := getEvents()
	if events[0].Request == nil {
		t.Fatal("expected request info on panic event")
	}
	if events[0].Request.Method != "POST" {
		t.Errorf("expected method POST, got %q", events[0].Request.Method)
	}
}

// ── Flush ─────────────────────────────────────────────────────────────────────

func TestFlush_DrainAllEvents(t *testing.T) {
	srv, getEvents := captureServer(t)
	c := newClient(t, srv)

	const n = 20
	for i := 0; i < n; i++ {
		c.CaptureError(context.Background(), fmt.Errorf("err-%d", i))
	}
	c.Flush()

	events := getEvents()
	if len(events) != n {
		t.Errorf("expected %d events after Flush, got %d", n, len(events))
	}
}

func TestFlush_SafeToCallMultipleTimes(t *testing.T) {
	srv, _ := captureServer(t)
	c := newClient(t, srv)
	c.CaptureError(context.Background(), errors.New("err"))

	// Should not panic on double Flush
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Flush panicked: %v", r)
		}
	}()
	c.Flush()
	c.Flush()
}

func TestFlush_EmptyQueueReturnsQuickly(t *testing.T) {
	srv, _ := captureServer(t)
	c := newClient(t, srv)

	start := time.Now()
	c.Flush()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("Flush on empty queue took too long: %v", elapsed)
	}
}

// ── Queue full / backpressure ─────────────────────────────────────────────────

func TestEnqueue_QueueFullDropsSilently(t *testing.T) {
	// Slow server to keep queue backed up
	var received atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		received.Add(1)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"ok"}`))
	}))
	defer slow.Close()

	c := New(slow.URL, "key") // queue cap = 512

	// Send 600 events — 88 should be silently dropped
	for i := 0; i < 600; i++ {
		c.CaptureError(context.Background(), fmt.Errorf("err-%d", i))
	}

	// Must not panic
	c.Flush()
}

// ── errorType helper ──────────────────────────────────────────────────────────

func TestErrorType_GenericError(t *testing.T) {
	err := errors.New("plain error")
	if errorType(err) != "Error" {
		t.Errorf("expected 'Error', got %q", errorType(err))
	}
}

func TestErrorType_WrappedError(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", errors.New("inner"))
	// fmt.Errorf returns *fmt.wrapError → mapped to "Error"
	if errorType(err) != "Error" {
		t.Errorf("expected 'Error' for wrapped err, got %q", errorType(err))
	}
}

type customErr struct{ msg string }

func (e *customErr) Error() string { return e.msg }

func TestErrorType_CustomType(t *testing.T) {
	err := &customErr{msg: "custom"}
	got := errorType(err)
	if got != "customErr" {
		t.Errorf("expected 'customErr', got %q", got)
	}
}

// ── generateID ────────────────────────────────────────────────────────────────

func TestGenerateID_Unique(t *testing.T) {
	ids := make(map[string]struct{})
	for i := 0; i < 1000; i++ {
		id := generateID()
		if len(id) != 32 {
			t.Errorf("expected id length 32, got %d", len(id))
		}
		if _, seen := ids[id]; seen {
			t.Errorf("duplicate id generated: %s", id)
		}
		ids[id] = struct{}{}
	}
}

// ── isSystemFrame ─────────────────────────────────────────────────────────────

func TestIsSystemFrame(t *testing.T) {
	cases := []struct {
		file   string
		system bool
	}{
		{"runtime/proc.go", true},
		{"net/http/server.go", true},
		{"go/src/something.go", true},
		{"/home/user/myapp/main.go", false},
		{"github.com/myorg/myapp/handler.go", false},
	}
	for _, tc := range cases {
		got := isSystemFrame(tc.file)
		if got != tc.system {
			t.Errorf("isSystemFrame(%q) = %v, want %v", tc.file, got, tc.system)
		}
	}
}

// ── API key sent in header ────────────────────────────────────────────────────

func TestSend_APIKeyInHeader(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Errly-Key")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"ok"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "super-secret")
	c.CaptureError(context.Background(), errors.New("err"))
	c.Flush()

	if gotKey != "super-secret" {
		t.Errorf("expected X-Errly-Key='super-secret', got %q", gotKey)
	}
}
