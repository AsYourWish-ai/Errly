package main

import "time"

type Event struct {
	ID          string            `json:"id"`
	Timestamp   time.Time         `json:"timestamp"`
	Level       string            `json:"level"` // error, warning, info
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

type Issue struct {
	ID          string    `json:"id"`
	Fingerprint string    `json:"fingerprint"`
	Title       string    `json:"title"`
	Level       string    `json:"level"`
	Platform    string    `json:"platform"`
	Environment string    `json:"environment"`
	ProjectKey  string    `json:"project_key"`
	Count       int       `json:"count"`
	Status      string    `json:"status"` // unresolved, resolved, ignored
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	LastEvent   *Event    `json:"last_event,omitempty"`
}

type Stats struct {
	Total         int `json:"total"`
	Unresolved    int `json:"unresolved"`
	Resolved      int `json:"resolved"`
	Ignored       int `json:"ignored"`
	TotalEvents   int `json:"total_events"`
	EventsLast24h int `json:"events_last_24h"`
}
