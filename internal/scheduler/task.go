package scheduler

import "time"

// TaskStatus is the lifecycle state of a Task. Exactly one at any time.
type TaskStatus string

const (
	StatusPending   TaskStatus = "pending"   // accepted, no attempt started yet
	StatusRunning   TaskStatus = "running"   // at least one attempt fired, not terminal
	StatusSucceeded TaskStatus = "succeeded" // an attempt got a 2xx response
	StatusFailed    TaskStatus = "failed"    // retries exhausted, last attempt non-2xx/error
	StatusCancelled TaskStatus = "cancelled" // user deleted before terminal
)

// IsTerminal reports whether the status is a final, retained-then-purged state.
func (s TaskStatus) IsTerminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled
}

// RetryMode selects how the delay between retries is computed.
type RetryMode string

const (
	// RetryFixed waits retry_interval between every attempt.
	RetryFixed RetryMode = "fixed"
	// RetryExponential waits min(retry_interval * 2^n, cap) with full jitter,
	// backing off from a struggling endpoint instead of hammering it.
	RetryExponential RetryMode = "exponential"
)

// Valid reports whether m is a recognised retry mode.
func (m RetryMode) Valid() bool {
	return m == RetryFixed || m == RetryExponential
}

// Attempt records one HTTP POST fired at the Task's url.
type Attempt struct {
	N          int       `json:"n"`               // 1-based attempt number
	FiredAt    time.Time `json:"fired_at"`        // when the request went out
	StatusCode int       `json:"status_code"`     // HTTP status, 0 on transport error
	Error      string    `json:"error,omitempty"` // transport or non-2xx description
	DurationMs int64     `json:"duration_ms"`     // round-trip time in milliseconds
	// ResponseBody is the first ~2KB of the response body, captured only on
	// failed (non-2xx) attempts to explain why delivery failed.
	ResponseBody          string `json:"response_body,omitempty"`
	ResponseBodyTruncated bool   `json:"response_body_truncated,omitempty"` // true if body was cut at the cap
}

type Task struct {
	ID string `json:"id"`
	// IdempotencyKey is the caller-supplied Idempotency-Key the Task was
	// created with, if any. Set once at creation and never changed: it is the
	// Task's identity to the caller, independent of what it is scheduled to do.
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	URL            string            `json:"url"`
	Method         string            `json:"method"` // HTTP verb, defaults to POST
	ExecuteAt      time.Time         `json:"execute_at"`
	Headers        map[string]string `json:"headers"` // Custom headers
	Payload        any               `json:"payload"` // Flexible payload
	Retries        int               `json:"retries"`
	RetryInterval  int               `json:"retry_interval"` // milliseconds, base delay between retries
	RetryMode      RetryMode         `json:"retry_mode"`     // fixed (default) or exponential

	Status     TaskStatus `json:"status"`
	Attempts   []Attempt  `json:"attempts,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"` // set when terminal
}
