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
