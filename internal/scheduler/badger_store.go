package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// Key layout: "task:<status>:<zero-padded-unix-ts>:<id>"
//
// Partitioning by status keeps the hot path (find pending due tasks) scanning
// only live work, and lets terminal tasks carry an independent TTL. The
// zero-padded timestamp preserves chronological ordering within a status.
const keyPrefix = "task:"

func taskKey(t Task) string {
	return fmt.Sprintf("task:%s:%016d:%s", t.Status, t.ExecuteAt.Unix(), t.ID)
}

func statusPrefix(status TaskStatus) string {
	return fmt.Sprintf("task:%s:", status)
}

type BadgerStore struct {
	db  *badger.DB
	ttl time.Duration // retention for terminal tasks
}

// NewBadgerStore opens the store. historyTTL bounds how long terminal
// (succeeded/failed/cancelled) tasks are retained for history.
func NewBadgerStore(path string, historyTTL time.Duration) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	return &BadgerStore{db: db, ttl: historyTTL}, nil
}

// Close flushes and releases the underlying BadgerDB. Call once on shutdown so
// the LSM tree and value log are left in a clean, reopenable state.
func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// RunGC reclaims value-log space on a ticker until ctx is cancelled. BadgerDB
// never garbage-collects its value log on its own, so without this the single
// binary silently accumulates on-disk garbage and eventually eats its own disk.
// Each tick runs GC passes back-to-back until Badger reports nothing left to
// rewrite (any non-nil error, typically badger.ErrNoRewrite), then waits for
// the next tick.
func (s *BadgerStore) RunGC(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// A successful pass (nil) may leave more to reclaim, so loop until
			// RunValueLogGC declines with an error.
			for s.db.RunValueLogGC(0.5) == nil {
			}
		}
	}
}

// Backup streams a consistent snapshot of the entire store to w using
// BadgerDB's native online backup. It is safe to call while the store is
// serving traffic - unlike hot-copying the live data directory, which can
// capture a torn LSM tree. A full (not incremental) snapshot is written.
func (s *BadgerStore) Backup(w io.Writer) error {
	_, err := s.db.Backup(w, 0)
	return err
}

// Restore loads a Backup snapshot into a fresh data directory using BadgerDB's
// native Load. It refuses to run against a non-empty directory so a restore can
// never half-overwrite a live store; restore into an empty dir, then start the
// server against it. This is an offline operation - nothing else may hold the
// directory open.
func Restore(dir, backupFile string) error {
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("refusing to restore: data directory %q is not empty", dir)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	f, err := os.Open(backupFile)
	if err != nil {
		return err
	}
	defer f.Close()

	opts := badger.DefaultOptions(dir).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	defer db.Close()

	return db.Load(f, 256)
}

func (s *BadgerStore) put(txn *badger.Txn, task Task) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	e := badger.NewEntry([]byte(taskKey(task)), data)
	if task.Status.IsTerminal() && s.ttl > 0 {
		e = e.WithTTL(s.ttl)
	}
	return txn.SetEntry(e)
}

// findKey returns the current storage key for a task id, or nil if absent.
// ponytail: O(n) scan across all partitions; add an id->key index if task
// volume makes per-write lookups (Update/Delete) hot.
func findKey(txn *badger.Txn, id string) []byte {
	it := txn.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	prefix := []byte(keyPrefix)
	suffix := ":" + id
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		k := it.Item().KeyCopy(nil)
		if strings.HasSuffix(string(k), suffix) {
			return k
		}
	}
	return nil
}

// Save creates a new task in the pending keyspace.
func (s *BadgerStore) Save(task Task) error {
	task.Status = StatusPending
	return s.db.Update(func(txn *badger.Txn) error {
		return s.put(txn, task)
	})
}

// Update relocates a task to match its current status. The old key (which may
// live in a different status partition) is removed and the task re-written.
func (s *BadgerStore) Update(task Task) error {
	return s.db.Update(func(txn *badger.Txn) error {
		if old := findKey(txn, task.ID); old != nil {
			if err := txn.Delete(old); err != nil {
				return err
			}
		}
		return s.put(txn, task)
	})
}

// Delete hard-removes a task by id regardless of status.
func (s *BadgerStore) Delete(id string) error {
	return s.db.Update(func(txn *badger.Txn) error {
		if k := findKey(txn, id); k != nil {
			return txn.Delete(k)
		}
		return nil
	})
}

// GetDueTasks returns pending tasks due at or before end.
//
// The scan starts at the beginning of the pending partition, not at `start`, so
// tasks that came due while the server was down - and tasks re-queued by
// RecoverRunning, whose ExecuteAt is always in the past - are caught up rather
// than skipped. `start` is retained for interface symmetry.
func (s *BadgerStore) GetDueTasks(start, end time.Time) ([]Task, error) {
	var tasks []Task

	pfx := statusPrefix(StatusPending)
	endKey := fmt.Sprintf("%s%016d", pfx, end.Unix())

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek([]byte(pfx)); it.ValidForPrefix([]byte(pfx)); it.Next() {
			key := it.Item().Key()

			// exit once past the due window (keys are zero-padded, ordered)
			if string(key) > endKey {
				break
			}

			err := it.Item().Value(func(val []byte) error {
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

// ListTasks returns tasks, optionally filtered by status ("" = all).
func (s *BadgerStore) ListTasks(status string) ([]Task, error) {
	var tasks []Task

	prefix := []byte(keyPrefix)
	if status != "" {
		prefix = []byte(statusPrefix(TaskStatus(status)))
	}

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
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

// GetTask retrieves a single task by id. Returns nil if it doesn't exist.
func (s *BadgerStore) GetTask(id string) (*Task, error) {
	var task *Task

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte(keyPrefix)
		suffix := ":" + id
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			if !strings.HasSuffix(string(item.Key()), suffix) {
				continue
			}
			return item.Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err != nil {
					return err
				}
				task = &t
				return nil
			})
		}
		return nil
	})

	return task, err
}

// RecoverRunning re-queues tasks stuck in running back to pending.
func (s *BadgerStore) RecoverRunning() error {
	return s.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)

		prefix := []byte(statusPrefix(StatusRunning))
		var stuck []Task
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err == nil {
					stuck = append(stuck, t)
				}
				return nil
			})
			if err != nil {
				it.Close()
				return err
			}
		}
		it.Close()

		for _, t := range stuck {
			if err := txn.Delete([]byte(taskKey(t))); err != nil {
				return err
			}
			t.Status = StatusPending
			if err := s.put(txn, t); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteTasks hard-deletes tasks across all statuses matching the filters.
// url: exact match on target URL (optional)
// before: delete tasks scheduled before this time (optional)
// after: delete tasks scheduled after this time (optional)
// Returns the number of deleted tasks.
func (s *BadgerStore) DeleteTasks(url string, before, after *time.Time) (int, error) {
	var deleted int

	err := s.db.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte(keyPrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)

			shouldDelete := false
			err := item.Value(func(val []byte) error {
				var t Task
				if err := json.Unmarshal(val, &t); err != nil {
					return err
				}

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
