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

// MaxDueBatch bounds how many due Tasks one GetDueTasks call may return. After
// an outage the pending partition can hold an arbitrarily large backlog, and
// Tasks carry their full attempt history - reading all of it into one slice is
// the same unbounded allocation that paging fixed for listing.
const MaxDueBatch = 1000

// Counts is a tally of the store's contents, cheap enough to compute per
// metrics scrape.
type Counts struct {
	// ByStatus counts Tasks per lifecycle status.
	ByStatus map[TaskStatus]int
	// Overdue counts pending Tasks whose ExecuteAt has already passed - the
	// backlog the runner has yet to work through.
	Overdue int
}

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
	// GetDueTasks returns at most limit pending Tasks whose ExecuteAt falls in
	// [start, end], oldest first. limit is clamped to [1, MaxDueBatch],
	// defaulting to MaxDueBatch when <= 0; a backlog larger than one batch is
	// drained over successive calls rather than loaded at once.
	GetDueTasks(start, end time.Time, limit int) ([]Task, error)
	// ListTasks returns one page of Tasks, optionally filtered by status
	// ("" = all), starting after cursor ("" = first page). limit is clamped to
	// [1, MaxPageSize], defaulting to DefaultPageSize when <= 0. The second
	// return is the cursor for the next page, "" when the last page is reached.
	ListTasks(status, cursor string, limit int) ([]Task, string, error)
	// RecoverRunning re-queues Tasks stuck in running (e.g. after a crash)
	// back to pending. Delivery is at-least-once.
	RecoverRunning() error
	// Counts tallies Tasks per status, and how many pending Tasks are already
	// due as of now.
	Counts(now time.Time) (Counts, error)
}
