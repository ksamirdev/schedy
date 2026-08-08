package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ksamirdev/schedy/internal/metrics"
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
	// createMu serializes the findDuplicate + Save pair so two concurrent
	// creates carrying the same Idempotency-Key can't both miss the duplicate
	// check and both persist. Schedy is single-process, so one mutex is enough.
	createMu sync.Mutex
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
	Schedule      string              `json:"schedule"`       // optional Go duration ("15m"); recurring re-enqueue
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
	// Interval-only recurrence: a plain Go duration, never cron. ParseDuration
	// rejects cron expressions and calendar syntax for free.
	if req.Schedule != "" {
		if d, err := time.ParseDuration(req.Schedule); err != nil || d <= 0 {
			http.Error(w, `invalid schedule (positive Go duration like "15m" required)`, http.StatusBadRequest)
			return req, time.Time{}, false
		}
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
//
// ponytail: pages the whole pending partition on every create - O(pending) per
// request. Add an idempotency-key index if create throughput makes it hot.
func (h *Handler) findDuplicate(key, url string, executeAt time.Time) (*scheduler.Task, error) {
	// Without an idempotency key the duplicate is same-url by definition, so let
	// the store skip every other URL instead of paging them all back here.
	filter := scheduler.ListFilter{Status: string(scheduler.StatusPending)}
	if key == "" {
		filter.URL = url
	}
	for cursor := ""; ; {
		pending, next, err := h.Store.ListTasks(filter, cursor, scheduler.MaxPageSize)
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
		if next == "" {
			return nil, nil
		}
		cursor = next
	}
}

// CreateTask schedules a new task for a future time.
func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	req, t, ok := decodeTaskRequest(w, r)
	if !ok {
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")

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
		Schedule:       req.Schedule,
		Status:         scheduler.StatusPending,
	}

	// findDuplicate scans then Save writes; without serialization two same-key
	// creates can both miss the scan and both persist, defeating idempotency.
	// Unlock before the response encode so a slow client can't stall every
	// other create.
	h.createMu.Lock()
	existing, err := h.findDuplicate(idempotencyKey, req.URL, t)
	if err == nil && existing == nil {
		err = h.Store.Save(task)
	}
	h.createMu.Unlock()
	if err != nil {
		http.Error(w, "could not save task", http.StatusInternalServerError)
		return
	}

	status := http.StatusCreated
	if existing != nil {
		task = *existing
		status = http.StatusOK
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
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
	task.Schedule = req.Schedule

	if err := h.Store.Update(*task); err != nil {
		http.Error(w, "could not update task", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// taskPage is one page of a task listing. The listing is an envelope rather than
// a bare array so a client can tell a full store from an exhausted one.
type taskPage struct {
	Tasks      []scheduler.Task `json:"tasks"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

// ListTasks returns one page of scheduled tasks, optionally filtered by
// ?status=, exact ?url=, and the ?due_before=/?due_after= time window
// (RFC3339, strict bounds on execute_at). Paging is by ?cursor= (opaque, from
// next_cursor) and ?limit=.
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	status := q.Get("status")
	if status != "" && !scheduler.TaskStatus(status).Valid() {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	limit := 0 // store applies the default
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > scheduler.MaxPageSize {
			http.Error(w, fmt.Sprintf("invalid limit (1-%d)", scheduler.MaxPageSize), http.StatusBadRequest)
			return
		}
		limit = n
	}

	filter := scheduler.ListFilter{Status: status, URL: q.Get("url")}
	if v := q.Get("due_before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid due_before (RFC3339 required)", http.StatusBadRequest)
			return
		}
		filter.DueBefore = &t
	}
	if v := q.Get("due_after"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid due_after (RFC3339 required)", http.StatusBadRequest)
			return
		}
		filter.DueAfter = &t
	}

	tasks, next, err := h.Store.ListTasks(filter, q.Get("cursor"), limit)
	if errors.Is(err, scheduler.ErrInvalidCursor) {
		http.Error(w, "invalid cursor", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "could not list tasks", http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []scheduler.Task{} // encode as [], never null
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(taskPage{Tasks: tasks, NextCursor: next, HasMore: next != ""})
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

// Metrics renders Prometheus metrics. The task gauges are read from the store
// per scrape so they can't drift from it; everything else is in-process.
func (h *Handler) Metrics(w http.ResponseWriter, r *http.Request) {
	counts, err := h.Store.Counts(time.Now().UTC())
	if err != nil {
		http.Error(w, "could not read task counts", http.StatusInternalServerError)
		return
	}

	byStatus := make(map[string]int, len(counts.ByStatus))
	for status, n := range counts.ByStatus {
		byStatus[string(status)] = n
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if err := metrics.Write(w, metrics.Snapshot{ByStatus: byStatus, Overdue: counts.Overdue}); err != nil {
		// Headers are already out; the scrape fails on a truncated body.
		log.Printf("write metrics: %v", err)
	}
}

// Health is a liveness probe. Always returns 200 OK.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Ready is a readiness probe. Returns 200 if database is accessible, 503
// otherwise. It reads a single row: a probe that runs every few seconds must not
// scan the store.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	_, _, err := h.Store.ListTasks(scheduler.ListFilter{}, "", 1)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
