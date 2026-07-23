package runner

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStore is a concurrency-safe in-memory Store. The runner reads it from a
// worker goroutine while the test writes to it, which is the whole point of
// these tests, so the mutex is not optional.
type fakeStore struct {
	mu    sync.Mutex
	tasks map[string]scheduler.Task
}

func newFakeStore() *fakeStore {
	return &fakeStore{tasks: make(map[string]scheduler.Task)}
}

func (f *fakeStore) Save(task scheduler.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	task.Status = scheduler.StatusPending
	f.tasks[task.ID] = task
	return nil
}

func (f *fakeStore) Update(task scheduler.Task) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[task.ID] = task
	return nil
}

func (f *fakeStore) Delete(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tasks, id)
	return nil
}

func (f *fakeStore) GetTask(id string) (*scheduler.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[id]
	if !ok {
		return nil, nil
	}
	return &task, nil
}

func (f *fakeStore) DeleteTasks(url string, before, after *time.Time) (int, error) {
	return 0, nil
}

func (f *fakeStore) GetDueTasks(start, end time.Time) ([]scheduler.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var tasks []scheduler.Task
	for _, task := range f.tasks {
		if task.Status == scheduler.StatusPending && !task.ExecuteAt.After(end) {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (f *fakeStore) ListTasks(status string) ([]scheduler.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var tasks []scheduler.Task
	for _, task := range f.tasks {
		if status == "" || string(task.Status) == status {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (f *fakeStore) RecoverRunning() error { return nil }

// hitRecorder is a target server that reports the path of every delivery.
func hitRecorder(t *testing.T) (*httptest.Server, chan string) {
	t.Helper()
	hits := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits <- r.URL.Path
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

// The runner pre-fetches everything due in the next interval and holds an
// in-memory copy until each task's timer fires. These tests cover what happens
// when the task is edited inside that window.
func TestRunOnceRereadsTaskBeforeFiring(t *testing.T) {
	t.Run("reschedule drops the run", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		task := scheduler.Task{
			ID:        "t1",
			URL:       srv.URL + "/old",
			ExecuteAt: time.Now().Add(150 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}
		require.NoError(t, store.Save(task))

		r := &Runner{store: store, executor: executor.NewExecutor(), interval: time.Second}
		r.runOnce(time.Now(), time.Now().Add(time.Second))

		// Push the task an hour out while the runner still holds the stale copy.
		moved := task
		moved.ExecuteAt = time.Now().Add(time.Hour)
		require.NoError(t, store.Update(moved))

		select {
		case path := <-hits:
			t.Fatalf("fired %s despite the reschedule", path)
		case <-time.After(600 * time.Millisecond):
		}

		got, err := store.GetTask("t1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, scheduler.StatusPending, got.Status, "task must stay pending for the next tick")
	})

	t.Run("edit fires the fresh url, not the stale copy", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		task := scheduler.Task{
			ID:        "t2",
			URL:       srv.URL + "/old",
			ExecuteAt: time.Now().Add(150 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}
		require.NoError(t, store.Save(task))

		r := &Runner{store: store, executor: executor.NewExecutor(), interval: time.Second}
		r.runOnce(time.Now(), time.Now().Add(time.Second))

		// Same execute_at, different target.
		edited := task
		edited.URL = srv.URL + "/new"
		require.NoError(t, store.Update(edited))

		select {
		case path := <-hits:
			assert.Equal(t, "/new", path)
		case <-time.After(2 * time.Second):
			t.Fatal("task never fired")
		}

		require.Eventually(t, func() bool {
			got, err := store.GetTask("t2")
			return err == nil && got != nil && got.Status == scheduler.StatusSucceeded
		}, 2*time.Second, 20*time.Millisecond)

		got, err := store.GetTask("t2")
		require.NoError(t, err)
		assert.Equal(t, srv.URL+"/new", got.URL, "the edit must survive the write-back")
	})

	t.Run("edit fires the fresh retry settings", func(t *testing.T) {
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		store := newFakeStore()
		task := scheduler.Task{
			ID:        "t4",
			URL:       srv.URL,
			ExecuteAt: time.Now().Add(150 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}
		require.NoError(t, store.Save(task))

		r := &Runner{store: store, executor: executor.NewExecutor(), interval: time.Second}
		r.runOnce(time.Now(), time.Now().Add(time.Second))

		// Arm retries that the stale copy does not have.
		edited := task
		edited.Retries = 2
		edited.RetryInterval = 10
		require.NoError(t, store.Update(edited))

		require.Eventually(t, func() bool {
			got, err := store.GetTask("t4")
			return err == nil && got != nil && got.Status == scheduler.StatusFailed
		}, 2*time.Second, 20*time.Millisecond)

		assert.Equal(t, int32(3), atomic.LoadInt32(&hits), "one delivery plus the two updated retries")
	})

	t.Run("cancel wins the race", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		task := scheduler.Task{
			ID:        "t3",
			URL:       srv.URL + "/old",
			ExecuteAt: time.Now().Add(150 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}
		require.NoError(t, store.Save(task))

		r := &Runner{store: store, executor: executor.NewExecutor(), interval: time.Second}
		r.runOnce(time.Now(), time.Now().Add(time.Second))

		cancelled := task
		cancelled.Status = scheduler.StatusCancelled
		require.NoError(t, store.Update(cancelled))

		select {
		case path := <-hits:
			t.Fatalf("fired %s despite the cancel", path)
		case <-time.After(600 * time.Millisecond):
		}
	})
}
