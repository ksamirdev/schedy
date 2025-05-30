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
