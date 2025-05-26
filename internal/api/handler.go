package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

type Handler struct {
	Store scheduler.Store
}

func New(store scheduler.Store) *Handler {
	return &Handler{Store: store}
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL       string         `json:"url"`
		Payload   map[string]any `json:"payload"`
		ExecuteAt string         `json:"execute_at"` // RFC3339
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

	task := scheduler.Task{
		ID:        uuid.NewString(),
		URL:       req.URL,
		Payload:   req.Payload,
		ExecuteAt: t,
	}
	if err := h.Store.Save(task); err != nil {
		http.Error(w, "could not save task", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}
