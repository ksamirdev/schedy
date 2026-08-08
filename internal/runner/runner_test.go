package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// httptest servers bind to loopback, which the executor's new SSRF guard blocks
// by default. Opt out for the package so the runner can reach them.
func TestMain(m *testing.M) {
	os.Setenv("SCHEDY_ALLOW_PRIVATE_TARGETS", "1")
	os.Exit(m.Run())
}

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

func (f *fakeStore) GetDueTasks(start, end time.Time, limit int) ([]scheduler.Task, error) {
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

func (f *fakeStore) ListTasks(status, cursor string, limit int) ([]scheduler.Task, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var tasks []scheduler.Task
	for _, task := range f.tasks {
		if status == "" || string(task.Status) == status {
			tasks = append(tasks, task)
		}
	}
	return tasks, "", nil
}

func (f *fakeStore) RecoverRunning() error { return nil }

func (f *fakeStore) Counts(now time.Time) (scheduler.Counts, error) {
	return scheduler.Counts{}, nil
}

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

// A task that exhausts its retries fires a best-effort callback to
// SCHEDY_ON_FAILURE_URL; a task that succeeds fires nothing.
func TestFailureCallback(t *testing.T) {
	t.Run("fires once on failed with the terminal details", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(target.Close)

		got := make(chan map[string]any, 4)
		hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			got <- b
		}))
		t.Cleanup(hook.Close)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "f1",
			URL:       target.URL,
			ExecuteAt: time.Now().Add(150 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.onFailureURL = hook.URL
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		select {
		case b := <-got:
			assert.Equal(t, "f1", b["id"])
			assert.Equal(t, "failed", b["status"])
			assert.Equal(t, float64(500), b["status_code"])
			assert.Equal(t, float64(1), b["attempts"])
		case <-time.After(2 * time.Second):
			t.Fatal("no failure callback fired")
		}
	})

	t.Run("per-task on_failure_url wins over the global hook", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(target.Close)

		taskHook := make(chan struct{}, 1)
		perTask := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			taskHook <- struct{}{}
		}))
		t.Cleanup(perTask.Close)

		globalHook := make(chan struct{}, 1)
		global := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			globalHook <- struct{}{}
		}))
		t.Cleanup(global.Close)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:           "f2",
			URL:          target.URL,
			OnFailureURL: perTask.URL,
			ExecuteAt:    time.Now().Add(150 * time.Millisecond),
			Status:       scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.onFailureURL = global.URL
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		select {
		case <-taskHook:
		case <-time.After(2 * time.Second):
			t.Fatal("per-task callback did not fire")
		}
		select {
		case <-globalHook:
			t.Fatal("global callback fired despite per-task override")
		case <-time.After(300 * time.Millisecond):
		}
	})

	t.Run("silent on success", func(t *testing.T) {
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		t.Cleanup(target.Close)

		fired := make(chan struct{}, 1)
		hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fired <- struct{}{}
		}))
		t.Cleanup(hook.Close)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "s1",
			URL:       target.URL,
			ExecuteAt: time.Now().Add(150 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.onFailureURL = hook.URL
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		select {
		case <-fired:
			t.Fatal("callback fired on a successful task")
		case <-time.After(600 * time.Millisecond):
		}
	})
}

// A task with a Schedule re-enqueues a fresh one-shot at fire_time+interval
// after it fires; a task without one does not. Cancelling stops the chain,
// which the "cancel wins the race" case already covers (a cancelled task never
// fires, so it never reschedules).
func TestRecurringReschedule(t *testing.T) {
	t.Run("recurring task enqueues the next fire", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "rec1",
			URL:       srv.URL + "/ping",
			ExecuteAt: time.Now().Add(100 * time.Millisecond),
			Status:    scheduler.StatusPending,
			Schedule:  "1h", // far enough out that the successor won't fire mid-test
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))
		<-hits // original delivered

		// The successor is a fresh, distinct, pending one-shot carrying the schedule.
		require.Eventually(t, func() bool {
			pending, _, _ := store.ListTasks(string(scheduler.StatusPending), "", 0)
			return len(pending) == 1
		}, 2*time.Second, 20*time.Millisecond)

		pending, _, _ := store.ListTasks(string(scheduler.StatusPending), "", 0)
		next := pending[0]
		assert.NotEqual(t, "rec1", next.ID, "recurrence is a new task, not a mutation")
		assert.Equal(t, "1h", next.Schedule, "the chain carries the schedule forward")
		assert.Empty(t, next.Attempts, "the successor starts clean")
		assert.True(t, next.ExecuteAt.After(time.Now().Add(30*time.Minute)), "next fires ~1h out")
	})

	t.Run("non-recurring task does not re-enqueue", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "once1",
			URL:       srv.URL + "/ping",
			ExecuteAt: time.Now().Add(100 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))
		<-hits

		require.Eventually(t, func() bool {
			got, _ := store.GetTask("once1")
			return got != nil && got.Status == scheduler.StatusSucceeded
		}, 2*time.Second, 20*time.Millisecond)

		all, _, _ := store.ListTasks("", "", 0)
		assert.Len(t, all, 1, "a one-shot leaves no successor")
	})
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

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

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

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

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

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

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

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

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

// A backlog must not become a thundering herd. Whatever the outage left behind,
// the runner may only hold maxConcurrent deliveries open at once.
func TestConcurrencyIsBounded(t *testing.T) {
	const limit = 3

	var (
		mu      sync.Mutex
		active  int
		peak    int
		release = make(chan struct{})
	)
	// Unblock the handlers however the test ends: a failed assertion would
	// otherwise leave every delivery goroutine parked and hang the package.
	releaseAll := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseAll)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()

		<-release // hold the slot until the test lets go

		mu.Lock()
		active--
		mu.Unlock()
	}))
	t.Cleanup(target.Close)

	store := newFakeStore()
	for i := 0; i < limit*4; i++ {
		require.NoError(t, store.Save(scheduler.Task{
			ID:        fmt.Sprintf("burst%02d", i),
			URL:       target.URL,
			ExecuteAt: time.Now().Add(-time.Minute), // all overdue, all fire at once
			Status:    scheduler.StatusPending,
		}))
	}

	r := New(store, executor.NewExecutor(), time.Second)
	r.sem = make(chan struct{}, limit)
	r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

	// Let the first wave saturate. Give the excess goroutines time to pile in
	// too, so an unbounded runner is caught rather than merely slow.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return active >= limit
	}, 2*time.Second, 10*time.Millisecond, "deliveries never reached the cap")
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, limit, peak, "concurrency exceeded the cap")
	mu.Unlock()

	releaseAll()
	require.Eventually(t, func() bool {
		all, _, _ := store.ListTasks(string(scheduler.StatusSucceeded), "", 0)
		return len(all) == limit*4
	}, 5*time.Second, 20*time.Millisecond, "backlog did not drain")

	mu.Lock()
	assert.LessOrEqual(t, peak, limit, "concurrency exceeded the cap while draining")
	mu.Unlock()
}

// A task waits in the store as pending until it actually fires, so a task still
// queued behind the concurrency cap is visible to the next tick. It must not be
// delivered twice.
func TestNoDoubleDeliveryAcrossTicks(t *testing.T) {
	var hits atomic.Int32
	release := make(chan struct{})
	releaseAll := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseAll) // never leave a delivery parked on a failed assertion

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
	}))
	t.Cleanup(target.Close)

	store := newFakeStore()
	require.NoError(t, store.Save(scheduler.Task{
		ID:        "slow",
		URL:       target.URL,
		ExecuteAt: time.Now().Add(-time.Minute),
		Status:    scheduler.StatusPending,
	}))

	r := New(store, executor.NewExecutor(), time.Second)
	r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))
	require.Eventually(t, func() bool { return hits.Load() == 1 }, 2*time.Second, 10*time.Millisecond)

	// Second tick while the first delivery is still in flight.
	r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))
	time.Sleep(200 * time.Millisecond)
	assert.EqualValues(t, 1, hits.Load(), "an in-flight task was picked up twice")

	releaseAll()
}

// With SCHEDY_MAX_STALENESS set, a task that came due long ago is retired rather
// than delivered - and the retirement is never silent.
func TestMaxStaleness(t *testing.T) {
	t.Run("a stale task is skipped, not delivered", func(t *testing.T) {
		var hits atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
		}))
		t.Cleanup(target.Close)

		got := make(chan map[string]any, 1)
		hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			got <- b
		}))
		t.Cleanup(hook.Close)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "ancient",
			URL:       target.URL,
			ExecuteAt: time.Now().Add(-6 * time.Hour),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.maxStaleness = time.Hour
		r.onFailureURL = hook.URL
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		require.Eventually(t, func() bool {
			task, _ := store.GetTask("ancient")
			return task != nil && task.Status == scheduler.StatusFailed
		}, 2*time.Second, 20*time.Millisecond, "stale task never reached a terminal state")

		assert.EqualValues(t, 0, hits.Load(), "a skipped task must not be delivered")

		task, _ := store.GetTask("ancient")
		require.Len(t, task.Attempts, 1, "the skip must leave a trace in the attempt log")
		assert.Contains(t, task.Attempts[0].Error, "exceeds max staleness")
		assert.NotNil(t, task.FinishedAt)

		select {
		case b := <-got:
			assert.Equal(t, "ancient", b["id"], "a skipped task must still fire the failure callback")
		case <-time.After(2 * time.Second):
			t.Fatal("skipping a task was silent")
		}
	})

	t.Run("a task inside the window still fires", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "recent",
			URL:       srv.URL + "/ping",
			ExecuteAt: time.Now().Add(-time.Minute),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.maxStaleness = time.Hour
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		select {
		case <-hits:
		case <-time.After(2 * time.Second):
			t.Fatal("a task within the staleness window was not delivered")
		}
	})

	t.Run("unset means catch everything up", func(t *testing.T) {
		srv, hits := hitRecorder(t)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "ancient2",
			URL:       srv.URL + "/ping",
			ExecuteAt: time.Now().Add(-30 * 24 * time.Hour),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second) // maxStaleness unset
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		select {
		case <-hits:
		case <-time.After(2 * time.Second):
			t.Fatal("default behaviour must still fire an old task")
		}
	})

	// An outage must interrupt a recurring chain, not end it.
	t.Run("a skipped recurring task still re-enqueues", func(t *testing.T) {
		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "recur-stale",
			URL:       "http://127.0.0.1:1/never",
			ExecuteAt: time.Now().Add(-6 * time.Hour),
			Status:    scheduler.StatusPending,
			Schedule:  "1h",
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.maxStaleness = time.Minute
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		require.Eventually(t, func() bool {
			pending, _, _ := store.ListTasks(string(scheduler.StatusPending), "", 0)
			return len(pending) == 1
		}, 2*time.Second, 20*time.Millisecond, "the recurring chain died on a skip")

		pending, _, _ := store.ListTasks(string(scheduler.StatusPending), "", 0)
		assert.NotEqual(t, "recur-stale", pending[0].ID)
		assert.True(t, pending[0].ExecuteAt.After(time.Now()), "the successor is anchored forward, not replayed")
	})
}

// A replayed task keeps its earlier attempts, so the numbering must continue
// rather than restart - two attempts both called "n: 1" make the log unreadable
// at exactly the moment someone is reading it.
func TestAttemptNumberingContinuesAfterReplay(t *testing.T) {
	srv, hits := hitRecorder(t)

	store := newFakeStore()
	require.NoError(t, store.Save(scheduler.Task{
		ID:        "replayed",
		URL:       srv.URL + "/ping",
		ExecuteAt: time.Now().Add(-time.Minute),
		Status:    scheduler.StatusPending,
		// What a prior failed run left behind, as the replay endpoint preserves it.
		Attempts: []scheduler.Attempt{
			{N: 1, StatusCode: 500, Error: "boom"},
			{N: 2, StatusCode: 500, Error: "boom"},
		},
	}))

	r := New(store, executor.NewExecutor(), time.Second)
	r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))
	<-hits

	require.Eventually(t, func() bool {
		got, _ := store.GetTask("replayed")
		return got != nil && got.Status == scheduler.StatusSucceeded
	}, 2*time.Second, 20*time.Millisecond)

	got, _ := store.GetTask("replayed")
	require.Len(t, got.Attempts, 3, "the replay appends rather than replacing")
	assert.Equal(t, 3, got.Attempts[2].N, "numbering continues across the replay")
}

func TestShutdownDrain(t *testing.T) {
	t.Run("cancelled before fire leaves the task pending", func(t *testing.T) {
		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "d1",
			URL:       "http://example.com",
			ExecuteAt: time.Now().Add(500 * time.Millisecond),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r.runOnce(ctx, time.Now(), time.Now().Add(time.Second))

		start := time.Now()
		r.drain(2 * time.Second)
		assert.Less(t, time.Since(start), time.Second, "drain must not wait out the pre-fire timer")

		task, err := store.GetTask("d1")
		require.NoError(t, err)
		require.NotNil(t, task)
		assert.Equal(t, scheduler.StatusPending, task.Status)
		assert.Empty(t, task.Attempts)
	})

	t.Run("in-flight delivery records its outcome before drain returns", func(t *testing.T) {
		release := make(chan struct{})
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-release
		}))
		t.Cleanup(target.Close)

		store := newFakeStore()
		require.NoError(t, store.Save(scheduler.Task{
			ID:        "d2",
			URL:       target.URL,
			ExecuteAt: time.Now(),
			Status:    scheduler.StatusPending,
		}))

		r := New(store, executor.NewExecutor(), time.Second)
		r.runOnce(context.Background(), time.Now(), time.Now().Add(time.Second))

		go func() {
			time.Sleep(200 * time.Millisecond)
			close(release)
		}()
		r.drain(5 * time.Second)

		task, err := store.GetTask("d2")
		require.NoError(t, err)
		require.NotNil(t, task)
		assert.Equal(t, scheduler.StatusSucceeded, task.Status)
	})
}
