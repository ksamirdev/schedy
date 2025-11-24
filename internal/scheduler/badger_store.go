package scheduler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type BadgerStore struct {
	db *badger.DB
}

func NewBadgerStore(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &BadgerStore{db: db}, nil
}

func (s *BadgerStore) Save(task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.db.Update(func(txn *badger.Txn) error {
		// key format: "task:<timestamp>:<id>"
		// zero-padded timestamp (%016d) ensuring proper lexicographical ordering.
		k := fmt.Sprintf("task:%016d:%s", task.ExecuteAt.Unix(), task.ID)
		return txn.Set([]byte(k), data)
	})
}

func (s *BadgerStore) Delete(id string, timestamp int64) error {
	return s.db.Update(func(txn *badger.Txn) error {
		k := fmt.Sprintf("task:%016d:%s", timestamp, id)
		return txn.Delete([]byte(k))
	})
}

func (s *BadgerStore) GetDueTasks(start, end time.Time) ([]Task, error) {
	var tasks []Task

	startKey := fmt.Sprintf("task:%016d", start.Unix())
	endKey := fmt.Sprintf("task:%016d", end.Unix())

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek([]byte(startKey)); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			// exits if future record found
			if string(key) > endKey {
				break
			}

			err := item.Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err == nil {
					tasks = append(tasks, t)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return tasks, err
}

func (s *BadgerStore) ListTasks() ([]Task, error) {
	var tasks []Task

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("task:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err == nil {
					tasks = append(tasks, t)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	return tasks, err
}

// GetTask retrieves a single task by ID.
// Returns nil if task doesn't exist.
func (s *BadgerStore) GetTask(id string) (*Task, error) {
	var task *Task

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("task:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err != nil {
					return err
				}
				if t.ID == id {
					task = &t
				}
				return nil
			})
			if err != nil {
				return err
			}
			if task != nil {
				break
			}
		}
		return nil
	})

	return task, err
}

// DeleteTasks deletes tasks based on filters.
// url: exact match on target URL (optional)
// before: delete tasks scheduled before this time (optional)
// after: delete tasks scheduled after this time (optional)
// Returns the number of deleted tasks.
func (s *BadgerStore) DeleteTasks(url string, before, after *time.Time) (int, error) {
	var deleted int

	err := s.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("task:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.Key()

			shouldDelete := false
			err := item.Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err != nil {
					return err
				}

				// Apply filters
				matches := true
				if url != "" && t.URL != url {
					matches = false
				}
				if before != nil && !t.ExecuteAt.Before(*before) {
					matches = false
				}
				if after != nil && !t.ExecuteAt.After(*after) {
					matches = false
				}

				shouldDelete = matches
				return nil
			})
			if err != nil {
				return err
			}

			if shouldDelete {
				if err := txn.Delete(key); err != nil {
					return err
				}
				deleted++
			}
		}
		return nil
	})

	return deleted, err
}
