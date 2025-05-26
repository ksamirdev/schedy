package executor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

type Executor struct {
	client *http.Client
}

func NewExecutor() *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (e *Executor) Execute(task scheduler.Task) error {
	var bodyBytes []byte
	switch v := task.Payload.(type) {
	case string:
		bodyBytes = []byte(v)
	case []byte:
		bodyBytes = v
	default:
		// fallback to JSON
		bodyBytes, _ = json.Marshal(task.Payload)
	}

	req, err := http.NewRequest(http.MethodPost, task.URL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return err
	}

	// Set custom headers
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}
	// If no Content-Type header is set, default to application/json
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	_, err = e.client.Do(req)
	return err
}
