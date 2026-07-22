package scheduler

import "time"

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
	// ListTasks returns Tasks, optionally filtered by status ("" = all).
	ListTasks(status string) ([]Task, error)
	// RecoverRunning re-queues Tasks stuck in running (e.g. after a crash)
	// back to pending. Delivery is at-least-once.
	RecoverRunning() error
}
