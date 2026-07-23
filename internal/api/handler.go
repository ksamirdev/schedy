package api

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

const DEFAULT_RETRY_INTERVAL = 2000

// validMethods is the whitelist of HTTP verbs a task may deliver.
var validMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
	http.MethodHead:   true,
}

type Handler struct {
	Store  scheduler.Store
	APIKey string
}

func New(store scheduler.Store) *Handler {
	return &Handler{
		Store:  store,
		APIKey: os.Getenv("SCHEDY_API_KEY"),
	}
}

// Middleware to check API key
func (h *Handler) WithAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.APIKey != "" {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				http.Error(w, "missing API key", http.StatusUnauthorized)
				return
			}
			// Constant-time compare: a plain != leaks the key one byte at a
			// time through response timing.
			if subtle.ConstantTimeCompare([]byte(key), []byte(h.APIKey)) != 1 {
				http.Error(w, "invalid API key", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

// taskRequest is the client-owned shape of a task, shared by create and update.
// Server-owned state (id, status, attempts, finished_at) is deliberately absent.
type taskRequest struct {
	URL           string              `json:"url"`
	Method        string              `json:"method"` // HTTP verb, defaults to POST
	Headers       map[string]string   `json:"headers"`
	Payload       any                 `json:"payload"`
	ExecuteAt     string              `json:"execute_at"` // RFC3339
	Retries       int                 `json:"retries"`
	RetryInterval *int                `json:"retry_interval"` // milliseconds
	RetryMode     scheduler.RetryMode `json:"retry_mode"`     // fixed (default) or exponential
}

// decodeTaskRequest reads and validates a task body, applying defaults for the
// optional fields. It writes the error response itself; the bool reports
// whether the caller may continue.
func decodeTaskRequest(w http.ResponseWriter, r *http.Request) (taskRequest, time.Time, bool) {
	var req taskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return req, time.Time{}, false
	}
	if req.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return req, time.Time{}, false
	}
	if req.Method == "" {
		req.Method = http.MethodPost
	}
	req.Method = strings.ToUpper(req.Method)
	if !validMethods[req.Method] {
		http.Error(w, "invalid method", http.StatusBadRequest)
		return req, time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, req.ExecuteAt)
	if err != nil {
		http.Error(w, "invalid time (ISO required)", http.StatusBadRequest)
		return req, time.Time{}, false
	}
	if !t.UTC().After(time.Now().UTC()) {
		http.Error(w, "time must be in the future", http.StatusBadRequest)
		return req, time.Time{}, false
	}
	if req.RetryInterval == nil {
		req.RetryInterval = new(int)
		*req.RetryInterval = DEFAULT_RETRY_INTERVAL
	}
	if req.RetryMode == "" {
		req.RetryMode = scheduler.RetryFixed
	}
	if !req.RetryMode.Valid() {
		http.Error(w, "invalid retry_mode", http.StatusBadRequest)
		return req, time.Time{}, false
	}
	return req, t, true
}

// loadTask resolves the {id} path value to a stored task. It writes the error
// response itself; the bool reports whether the caller may continue.
func (h *Handler) loadTask(w http.ResponseWriter, r *http.Request) (*scheduler.Task, bool) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return nil, false
	}

	task, err := h.Store.GetTask(id)
	if err != nil {
		http.Error(w, "could not get task", http.StatusInternalServerError)
		return nil, false
	}
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return nil, false
	}
	return task, true
}

// findDuplicate returns the pending task a create request would duplicate, or
// nil if there is none.
//
// An Idempotency-Key matches on the key alone: the key is the caller's name for
// the task, so a repeat of a request that has already been accepted returns the
// task it created, whatever the new body says. Without a key, an identical
// schedule - same url, same execute_at to within a second - is what counts as a
// repeat.
//
// Only pending tasks are considered. A task that has already run is history
// rather than a live schedule, and history expires under SCHEDY_HISTORY_TTL,
// which would otherwise make deduplication quietly depend on retention.
func (h *Handler) findDuplicate(key, url string, executeAt time.Time) (*scheduler.Task, error) {
	pending, err := h.Store.ListTasks(string(scheduler.StatusPending))
	if err != nil {
		return nil, err
	}
	for i := range pending {
		task := &pending[i]
		if key != "" {
			if task.IdempotencyKey == key {
				return task, nil
			}
			continue
		}
		if task.URL == url && task.ExecuteAt.Sub(executeAt).Abs() < time.Second {
			return task, nil
		}
	}
	return nil, nil
}

// CreateTask schedules a new task for a future time.
func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	req, t, ok := decodeTaskRequest(w, r)
	if !ok {
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	existing, err := h.findDuplicate(idempotencyKey, req.URL, t)
	if err != nil {
		http.Error(w, "could not check for duplicates", http.StatusInternalServerError)
		return
	}
	if existing != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(existing)
		return
	}

	task := scheduler.Task{
		ID:             uuid.NewString(),
		IdempotencyKey: idempotencyKey,
		URL:            req.URL,
		Method:         req.Method,
		Headers:        req.Headers,
		Payload:        req.Payload,
		ExecuteAt:      t,
		Retries:        req.Retries,
		RetryInterval:  *req.RetryInterval,
		RetryMode:      req.RetryMode,
		Status:         scheduler.StatusPending,
	}
	if err := h.Store.Save(task); err != nil {
		http.Error(w, "could not save task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

// UpdateTask replaces a pending task's client-owned fields, keeping its id.
// Only pending tasks are mutable; anything else is a conflict.
func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	req, execAt, ok := decodeTaskRequest(w, r)
	if !ok {
		return
	}

	task, ok := h.loadTask(w, r)
	if !ok {
		return
	}
	if task.Status != scheduler.StatusPending {
		http.Error(w, "only pending tasks can be updated", http.StatusConflict)
		return
	}

	// Full replace, but of the client-owned fields only. Status, attempts and
	// finished_at stay put: a task re-queued after a crash is pending with
	// attempts already logged, and that delivery record is not the client's to
	// overwrite.
	task.URL = req.URL
	task.Method = req.Method
	task.Headers = req.Headers
	task.Payload = req.Payload
	task.ExecuteAt = execAt
	task.Retries = req.Retries
	task.RetryInterval = *req.RetryInterval
	task.RetryMode = req.RetryMode

	if err := h.Store.Update(*task); err != nil {
		http.Error(w, "could not update task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// ListTasks returns scheduled tasks, optionally filtered by ?status=.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	tasks, err := h.Store.ListTasks(status)
	if err != nil {
		http.Error(w, "could not list tasks", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// GetTask returns a single task by ID
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTask(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// DeleteTask cancels a single task by ID. Non-terminal tasks are soft-cancelled
// (marked cancelled and retained in history); already-terminal tasks are a no-op
// and expire on their own via TTL.
func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTask(w, r)
	if !ok {
		return
	}

	if !task.Status.IsTerminal() {
		now := time.Now().UTC()
		task.Status = scheduler.StatusCancelled
		task.FinishedAt = &now
		if err := h.Store.Update(*task); err != nil {
			http.Error(w, "could not cancel task", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// DeleteTasks deletes tasks based on filters
func (h *Handler) DeleteTasks(w http.ResponseWriter, r *http.Request) {
	// Parse query params
	url := r.URL.Query().Get("url")
	beforeStr := r.URL.Query().Get("before")
	afterStr := r.URL.Query().Get("after")

	var before, after *time.Time

	if beforeStr != "" {
		t, err := time.Parse(time.RFC3339, beforeStr)
		if err != nil {
			http.Error(w, "invalid before timestamp (RFC3339 required)", http.StatusBadRequest)
			return
		}
		before = &t
	}

	if afterStr != "" {
		t, err := time.Parse(time.RFC3339, afterStr)
		if err != nil {
			http.Error(w, "invalid after timestamp (RFC3339 required)", http.StatusBadRequest)
			return
		}
		after = &t
	}

	// Require at least one filter
	if url == "" && before == nil && after == nil {
		http.Error(w, "at least one filter required (url, before, or after)", http.StatusBadRequest)
		return
	}

	deleted, err := h.Store.DeleteTasks(url, before, after)
	if err != nil {
		http.Error(w, "could not delete tasks", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"deleted": deleted})
}

// Health is a liveness probe. Always returns 200 OK.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Ready is a readiness probe. Returns 200 if database is accessible, 503 otherwise.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	_, err := h.Store.ListTasks("")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
