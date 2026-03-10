// Package errly provides a lightweight error monitoring SDK for Go.
//
// Usage:
//
//	client := errly.New("http://localhost:5080", "your-api-key",
//	    errly.WithProject("my-service"),
//	    errly.WithEnvironment("production"),
//	    errly.WithRelease("v1.2.3"),
//	)
//	defer client.Flush()
//
//	// Capture an error
//	client.CaptureError(ctx, err)
//
//	// Use as HTTP middleware
//	http.ListenAndServe(":5080", client.Middleware(myHandler))
package errly

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Client is the Errly error monitoring client.
type Client struct {
	endpoint    string
	apiKey      string
	project     string
	environment string
	release     string
	queue       chan *Event
	wg          sync.WaitGroup
	mu          sync.RWMutex
	breadcrumbs []Breadcrumb
	user        *UserInfo
	closeOnce   sync.Once
}

// Option configures the client.
type Option func(*Client)

func WithProject(project string) Option {
	return func(c *Client) { c.project = project }
}

func WithEnvironment(env string) Option {
	return func(c *Client) { c.environment = env }
}

func WithRelease(release string) Option {
	return func(c *Client) { c.release = release }
}

// New creates a new Errly client. Call Flush() or defer it to ensure all events are sent.
func New(endpoint, apiKey string, opts ...Option) *Client {
	c := &Client{
		endpoint:    strings.TrimRight(endpoint, "/"),
		apiKey:      apiKey,
		environment: "production",
		queue:       make(chan *Event, 512),
	}
	for _, o := range opts {
		o(c)
	}
	go c.worker()
	return c
}

// CaptureError captures an error with optional context keys.
func (c *Client) CaptureError(ctx context.Context, err error, extra ...map[string]any) string {
	if err == nil {
		return ""
	}
	event := c.newEvent("error")
	event.Exception = &Exception{
		Type:  errorType(err),
		Value: err.Error(),
	}
	event.Stacktrace = captureStack(2)
	if len(extra) > 0 {
		event.Extra = extra[0]
	}
	c.enqueue(event)
	return event.ID
}

// CaptureMessage captures a plain message at the given level.
func (c *Client) CaptureMessage(ctx context.Context, level, message string, extra ...map[string]any) string {
	event := c.newEvent(level)
	event.Message = message
	if len(extra) > 0 {
		event.Extra = extra[0]
	}
	c.enqueue(event)
	return event.ID
}

// SetUser sets the current user on future events.
func (c *Client) SetUser(user *UserInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.user = user
}

// AddBreadcrumb adds a breadcrumb to the trail.
func (c *Client) AddBreadcrumb(category, message, level string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.breadcrumbs = append(c.breadcrumbs, Breadcrumb{
		Timestamp: time.Now().UTC(),
		Category:  category,
		Message:   message,
		Level:     level,
		Type:      "default",
	})
	if len(c.breadcrumbs) > 50 {
		c.breadcrumbs = c.breadcrumbs[1:]
	}
}

// Middleware wraps an http.Handler to automatically capture panics and errors.
func (c *Client) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				var err error
				switch v := rec.(type) {
				case error:
					err = v
				default:
					err = fmt.Errorf("%v", v)
				}
				event := c.newEvent("fatal")
				event.Exception = &Exception{Type: "panic", Value: err.Error()}
				event.Stacktrace = captureStack(3)
				event.Request = &RequestInfo{
					URL:    r.URL.String(),
					Method: r.Method,
					Headers: map[string]string{
						"User-Agent": r.Header.Get("User-Agent"),
					},
				}
				c.enqueue(event)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Flush waits for all queued events to be sent (max 5s). Safe to call multiple times.
func (c *Client) Flush() {
	c.closeOnce.Do(func() { close(c.queue) })
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

// --- internals ---

func (c *Client) newEvent(level string) *Event {
	c.mu.RLock()
	crumbs := make([]Breadcrumb, len(c.breadcrumbs))
	copy(crumbs, c.breadcrumbs)
	user := c.user
	c.mu.RUnlock()

	return &Event{
		ID:          generateID(),
		Timestamp:   time.Now().UTC(),
		Level:       level,
		Platform:    "go",
		Environment: c.environment,
		Release:     c.release,
		ProjectKey:  c.project,
		Breadcrumbs: crumbs,
		User:        user,
	}
}

// enqueue adds an event to the send queue. wg.Add is called before enqueuing
// so that Flush()'s wg.Wait() never races with the worker's wg.Add.
func (c *Client) enqueue(event *Event) {
	c.wg.Add(1)
	select {
	case c.queue <- event:
	default:
		// Queue full — cancel the Add we just registered and drop the event.
		c.wg.Done()
	}
}

func (c *Client) worker() {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	for event := range c.queue {
		c.send(httpClient, event)
		c.wg.Done()
	}
}

func (c *Client) send(httpClient *http.Client, event *Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	req, err := http.NewRequest("POST", c.endpoint+"/api/v1/events", bytes.NewReader(data))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Errly-Key", c.apiKey)
	}
	httpClient.Do(req) //nolint:errcheck
}

func captureStack(skip int) []StackFrame {
	var frames []StackFrame
	for i := skip; i < skip+20; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		fn := runtime.FuncForPC(pc)
		name := "unknown"
		if fn != nil {
			name = fn.Name()
		}
		frames = append(frames, StackFrame{
			Filename: file,
			Function: name,
			Lineno:   line,
			InApp:    !isSystemFrame(file),
		})
	}
	return frames
}

func isSystemFrame(file string) bool {
	return strings.Contains(file, "runtime/") ||
		strings.Contains(file, "net/http") ||
		strings.Contains(file, "go/src")
}

func errorType(err error) string {
	t := fmt.Sprintf("%T", err)
	if t == "*errors.errorString" || t == "*fmt.wrapError" {
		return "Error"
	}
	parts := strings.Split(t, ".")
	return parts[len(parts)-1]
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Re-export types so callers don't need to use internal package
type Event struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	Level       string            `json:"level"`
	Message     string            `json:"message"`
	Exception   *Exception        `json:"exception,omitempty"`
	Stacktrace  []StackFrame      `json:"stacktrace,omitempty"`
	User        *UserInfo         `json:"user,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Extra       map[string]any    `json:"extra,omitempty"`
	Breadcrumbs []Breadcrumb      `json:"breadcrumbs,omitempty"`
	Environment string            `json:"environment"`
	Release     string            `json:"release"`
	Platform    string            `json:"platform"`
	ProjectKey  string            `json:"project_key"`
	Fingerprint string            `json:"fingerprint"`
	Request     *RequestInfo      `json:"request,omitempty"`
}

type Exception struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Module string `json:"module,omitempty"`
}

type StackFrame struct {
	Filename string `json:"filename"`
	Function string `json:"function"`
	Lineno   int    `json:"lineno"`
	Colno    int    `json:"colno,omitempty"`
	InApp    bool   `json:"in_app"`
}

type UserInfo struct {
	ID       string `json:"id,omitempty"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
}

type Breadcrumb struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Category  string    `json:"category"`
	Message   string    `json:"message"`
	Level     string    `json:"level"`
}

type RequestInfo struct {
	URL     string            `json:"url,omitempty"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}
