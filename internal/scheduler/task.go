package scheduler

import "time"

type Task struct {
	ID        string            `json:"id"`
	URL       string            `json:"url"`
	ExecuteAt time.Time         `json:"execute_at"`
	Headers   map[string]string `json:"headers"` // Custom headers
	Payload   any               `json:"payload"` // Flexible payload
}
