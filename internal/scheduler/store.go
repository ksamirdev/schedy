package scheduler

import "time"

type Store interface {
	Save(task Task) error
	Delete(id string) error
	GetDueTasks(now time.Time) ([]Task, error)
	GetAll() ([]Task, error)
}
