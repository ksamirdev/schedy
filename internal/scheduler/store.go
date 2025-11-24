package scheduler

import "time"

type Store interface {
	Save(task Task) error
	Delete(id string, timestamp int64) error
	GetTask(id string) (*Task, error)
	DeleteTasks(url string, before, after *time.Time) (int, error)
	GetDueTasks(start, end time.Time) ([]Task, error)
	ListTasks() ([]Task, error)
}
