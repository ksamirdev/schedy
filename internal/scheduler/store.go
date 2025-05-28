package scheduler

import "time"

type Store interface {
	Save(task Task) error
	Delete(id string, timestamp int64) error
	GetDueTasks(start, end time.Time) ([]Task, error)
}
