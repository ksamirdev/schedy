package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

// Result is the outcome of a single delivery attempt.
type Result struct {
	StatusCode int           // HTTP status, 0 on transport error
	Err        error         // nil on 2xx, otherwise transport error or non-2xx
	Duration   time.Duration // round-trip time
}

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

// Execute delivers one HTTP POST for the task and reports the attempt outcome
// (status code, error, duration). A 2xx yields a nil Err.
func (e *Executor) Execute(task scheduler.Task) Result {
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
		return Result{Err: err}
	}

	// Set custom headers
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}
	// If no Content-Type header is set, default to application/json
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	res, err := e.client.Do(req)
	dur := time.Since(start)
	if err != nil {
		// transport failure (DNS, timeout, connection refused): res is nil.
		return Result{Err: err, Duration: dur}
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Result{
			StatusCode: res.StatusCode,
			Err:        fmt.Errorf("unexpected status code: %d", res.StatusCode),
			Duration:   dur,
		}
	}

	return Result{StatusCode: res.StatusCode, Duration: dur}
}
