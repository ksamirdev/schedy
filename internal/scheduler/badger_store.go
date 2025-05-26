package scheduler

import (
	"encoding/json"
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
		return txn.Set([]byte("task:"+task.ID), data)
	})
}

func (s *BadgerStore) Delete(id string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte("task:" + id))
	})
}

func (s *BadgerStore) GetAll() ([]Task, error) {
	var tasks []Task
	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()
			if string(key[:5]) != "task:" {
				continue
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

func (s *BadgerStore) GetDueTasks(now time.Time) ([]Task, error) {
	all, err := s.GetAll()
	if err != nil {
		return nil, err
	}
	var due []Task
	for _, t := range all {
		if now.After(t.ExecuteAt) {
			due = append(due, t)
		}
	}
	return due, nil
}
