package scheduler

import "time"

type Task struct {
	ID        string         `json:"id"`
	URL       string         `json:"url"`
	ExecuteAt time.Time      `json:"execute_at"`
	Payload   map[string]any `json:"payload"` // JSON o
}
