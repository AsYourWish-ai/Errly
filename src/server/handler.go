package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Handler struct {
	storage            *Storage
	logger             *slog.Logger
	apiKey             string
	rateLimiter        *RateLimiter
	autoRemoveResolved bool
}

func NewHandler(storage *Storage, logger *slog.Logger, apiKey string) *Handler {
	return NewHandlerWithOptions(storage, logger, apiKey, 100, false)
}

func NewHandlerWithOptions(storage *Storage, logger *slog.Logger, apiKey string, ratePerMin int, autoRemoveResolved bool) *Handler {
	return &Handler{
		storage:            storage,
		logger:             logger,
		apiKey:             apiKey,
		rateLimiter:        NewRateLimiter(ratePerMin),
		autoRemoveResolved: autoRemoveResolved,
	}
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// Ingest endpoint (SDK sends events here)
	mux.HandleFunc("POST /api/v1/events", h.handleIngestEvent)

	// Issues API
	mux.HandleFunc("GET /api/v1/issues", h.auth(h.handleListIssues))
	mux.HandleFunc("GET /api/v1/issues/{id}", h.auth(h.handleGetIssue))
	mux.HandleFunc("PUT /api/v1/issues/{id}/status", h.auth(h.handleUpdateIssueStatus))
	mux.HandleFunc("GET /api/v1/issues/{id}/events", h.auth(h.handleGetIssueEvents))
	mux.HandleFunc("GET /api/v1/issues/search", h.auth(h.handleSearchIssues))

	// Stats
	mux.HandleFunc("GET /api/v1/stats", h.auth(h.handleStats))

	// Projects & Environments
	mux.HandleFunc("GET /api/v1/projects",     h.auth(h.handleListProjects))
	mux.HandleFunc("GET /api/v1/environments", h.auth(h.handleListEnvironments))

	// Server config (public — UI reads this on load)
	mux.HandleFunc("GET /api/v1/config", h.handleConfig)

	// Health
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Dashboard UI
	mux.HandleFunc("GET /", h.handleUI)
	mux.HandleFunc("GET /ui", h.handleUI)

	return h.cors(mux)
}

// handleIngestEvent receives error events from SDKs
func (h *Handler) handleIngestEvent(w http.ResponseWriter, r *http.Request) {
	// Rate limit per client IP
	if !h.rateLimiter.Allow(clientIP(r)) {
		w.Header().Set("Retry-After", retryAfterSeconds(h.rateLimiter.ratePerMin))
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// Auth via X-Errly-Key header or query param
	key := r.Header.Get("X-Errly-Key")
	if key == "" {
		key = r.URL.Query().Get("key")
	}
	if h.apiKey != "" && key != h.apiKey {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var event Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	// Fill defaults
	if event.ID == "" {
		event.ID = generateID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Level == "" {
		event.Level = "error"
	}
	if event.Environment == "" {
		event.Environment = "production"
	}

	if err := h.storage.SaveEvent(&event); err != nil {
		h.logger.Error("save event", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("event received", "id", event.ID, "level", event.Level, "message", event.Message)
	writeJSON(w, http.StatusCreated, map[string]string{"id": event.ID, "fingerprint": event.Fingerprint})
}

func (h *Handler) handleListIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	project := q.Get("project")
	env := q.Get("env")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit == 0 || limit > 100 {
		limit = 25
	}

	issues, total, err := h.storage.ListIssues(status, project, env, limit, offset)
	if err != nil {
		h.logger.Error("list issues", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"issues": issues,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *Handler) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	issue, err := h.storage.GetIssue(id)
	if err != nil {
		h.logger.Error("get issue", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if issue == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

func (h *Handler) handleUpdateIssueStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.Status != "resolved" && body.Status != "unresolved" && body.Status != "ignored" {
		http.Error(w, "status must be resolved, unresolved, or ignored", http.StatusBadRequest)
		return
	}
	if h.autoRemoveResolved && body.Status == "resolved" {
		if err := h.storage.DeleteIssue(id); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "resolved", "removed": "true"})
		return
	}
	if err := h.storage.UpdateIssueStatus(id, body.Status); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": body.Status})
}

func (h *Handler) handleGetIssueEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 || limit > 50 {
		limit = 10
	}
	events, err := h.storage.GetIssueEvents(id, limit)
	if err != nil {
		h.logger.Error("get issue events", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (h *Handler) handleSearchIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 20
	}
	issues, err := h.storage.SearchIssues(q, limit)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": issues})
}

func (h *Handler) handleStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	stats, err := h.storage.GetStats(project)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.storage.ListProjects()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if projects == nil {
		projects = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (h *Handler) handleListEnvironments(w http.ResponseWriter, r *http.Request) {
	envs, err := h.storage.ListEnvironments()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if envs == nil {
		envs = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": envs})
}

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"auto_remove_resolved": h.autoRemoveResolved,
	})
}

// Middleware: API key auth
func (h *Handler) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.apiKey == "" {
			next(w, r)
			return
		}
		key := r.Header.Get("X-Errly-Key")
		if key == "" {
			key = r.URL.Query().Get("key")
		}
		if key != h.apiKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Middleware: CORS
func (h *Handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Errly-Key, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return strings.ReplaceAll(hex.EncodeToString(b), "-", "")
}
