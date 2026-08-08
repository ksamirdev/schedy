package scheduler

import (
	"errors"
	"time"
)

// ErrInvalidCursor is returned by ListTasks when the supplied pagination cursor
// is malformed or does not belong to the requested status partition.
var ErrInvalidCursor = errors.New("invalid cursor")

// Page size bounds for ListTasks. A task carries its full attempt history, so
// an unbounded page is an unbounded response body.
const (
	DefaultPageSize = 100
	MaxPageSize     = 1000
)

type Store interface {
	// Save creates a new Task in the pending keyspace.
	Save(task Task) error
	// Update relocates a Task to match its current Status, applying the
	// history TTL when the status is terminal.
	Update(task Task) error
	// Delete hard-removes a Task by id regardless of status.
	Delete(id string) error
	GetTask(id string) (*Task, error)
	DeleteTasks(url string, before, after *time.Time) (int, error)
	// GetDueTasks returns pending Tasks whose ExecuteAt falls in [start, end].
	GetDueTasks(start, end time.Time) ([]Task, error)
	// ListTasks returns one page of Tasks, optionally filtered by status
	// ("" = all), starting after cursor ("" = first page). limit is clamped to
	// [1, MaxPageSize], defaulting to DefaultPageSize when <= 0. The second
	// return is the cursor for the next page, "" when the last page is reached.
	ListTasks(status, cursor string, limit int) ([]Task, string, error)
	// RecoverRunning re-queues Tasks stuck in running (e.g. after a crash)
	// back to pending. Delivery is at-least-once.
	RecoverRunning() error
}
