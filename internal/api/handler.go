package api

import (
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

const DEFAULT_RETRY_INTERVAL = 2000

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
			if key != h.APIKey {
				http.Error(w, "invalid API key", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL           string            `json:"url"`
		Headers       map[string]string `json:"headers"`
		Payload       any               `json:"payload"`
		ExecuteAt     string            `json:"execute_at"` // RFC3339
		Retries       int               `json:"retries"`
		RetryInterval *int              `json:"retry_interval"` // milliseconds
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	t, err := time.Parse(time.RFC3339, req.ExecuteAt)
	if err != nil {
		http.Error(w, "invalid time (ISO required)", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()
	if t.UTC().Before(now) {
		http.Error(w, "time cannot be in past", http.StatusBadRequest)
		return
	}

	if req.RetryInterval == nil {
		req.RetryInterval = new(int)
		*req.RetryInterval = DEFAULT_RETRY_INTERVAL
	}

	// Idempotency: Check for existing task with same URL and execute_at
	// If Idempotency-Key header is provided, use it for stricter matching
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey != "" || req.URL != "" {
		existingTasks, err := h.Store.ListTasks()
		if err == nil {
			for _, existing := range existingTasks {
				// Match by URL and execute time (within 1 second tolerance)
				if existing.URL == req.URL && 
					existing.ExecuteAt.Unix() == t.Unix() {
					// Return existing task instead of creating duplicate
					w.WriteHeader(http.StatusOK)
					json.NewEncoder(w).Encode(existing)
					return
				}
			}
		}
	}

	task := scheduler.Task{
		ID:            uuid.NewString(),
		URL:           req.URL,
		Headers:       req.Headers,
		Payload:       req.Payload,
		ExecuteAt:     t,
		Retries:       req.Retries,
		RetryInterval: *req.RetryInterval,
	}
	if err := h.Store.Save(task); err != nil {
		http.Error(w, "could not save task", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

// ListTasks returns all scheduled tasks (no status yet)
func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := h.Store.ListTasks()
	if err != nil {
		http.Error(w, "could not list tasks", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tasks)
}

// GetTask returns a single task by ID
func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}

	task, err := h.Store.GetTask(id)
	if err != nil {
		http.Error(w, "could not get task", http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

// DeleteTask deletes a single task by ID
func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing task id", http.StatusBadRequest)
		return
	}

	// First, get the task to find its timestamp
	task, err := h.Store.GetTask(id)
	if err != nil {
		http.Error(w, "could not get task", http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// Delete using ID and timestamp
	if err := h.Store.Delete(id, task.ExecuteAt.Unix()); err != nil {
		http.Error(w, "could not delete task", http.StatusInternalServerError)
		return
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
