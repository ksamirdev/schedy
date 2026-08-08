package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBadgerDB(t *testing.T) (*BadgerStore, func()) {
	path := "./testdb_" + uuid.New().String()

	store, err := NewBadgerStore(path, 72*time.Hour)
	require.NoError(t, err)

	cleanup := func() {
		store.db.Close()
		os.RemoveAll(path)
	}

	return store, cleanup
}

// RunGC must fire real value-log GC passes against a live DB without erroring
// the caller, stop promptly when its context is cancelled, and leave the store
// cleanly closeable.
func TestRunGCAndClose(t *testing.T) {
	path := "./testdb_" + uuid.New().String()
	store, err := NewBadgerStore(path, time.Hour)
	require.NoError(t, err)
	defer os.RemoveAll(path)

	require.NoError(t, store.Save(Task{ID: "g1", ExecuteAt: time.Now().Add(time.Hour)}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		store.RunGC(ctx, 10*time.Millisecond) // tight interval so passes actually run
		close(done)
	}()

	time.Sleep(60 * time.Millisecond) // let several GC ticks fire
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunGC did not stop after context cancel")
	}

	require.NoError(t, store.Close(), "store must close cleanly after GC")
}

// Backup then Restore into a fresh directory must round-trip every task, and
// Restore must refuse a directory that already holds data.
func TestBackupAndRestore(t *testing.T) {
	src, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	require.NoError(t, src.Save(Task{ID: "b1", ExecuteAt: now.Add(time.Hour), URL: "http://x/1"}))
	require.NoError(t, src.Save(Task{ID: "b2", ExecuteAt: now.Add(2 * time.Hour), URL: "http://x/2"}))

	// Online backup to a file while src is open.
	tmp := t.TempDir()
	backupFile := filepath.Join(tmp, "backup.badger")
	f, err := os.Create(backupFile)
	require.NoError(t, err)
	require.NoError(t, src.Backup(f))
	require.NoError(t, f.Close())

	t.Run("restores every task into a fresh dir", func(t *testing.T) {
		restoreDir := filepath.Join(tmp, "restored")
		require.NoError(t, Restore(restoreDir, backupFile))

		dst, err := NewBadgerStore(restoreDir, time.Hour)
		require.NoError(t, err)
		defer dst.db.Close()

		all, _, err := dst.ListTasks(ListFilter{}, "", 0)
		require.NoError(t, err)
		assert.Len(t, all, 2)

		got, err := dst.GetTask("b1")
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, "http://x/1", got.URL)
	})

	t.Run("refuses a non-empty data dir", func(t *testing.T) {
		occupied := filepath.Join(tmp, "occupied")
		busy, err := NewBadgerStore(occupied, time.Hour)
		require.NoError(t, err)
		require.NoError(t, busy.Save(Task{ID: "keep", ExecuteAt: now.Add(time.Hour)}))
		require.NoError(t, busy.db.Close())

		err = Restore(occupied, backupFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not empty")
	})
}

func TestSaveAndGetTasks(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task1 := Task{
		ID:        "task1",
		ExecuteAt: now.Add(5 * time.Second),
		Payload:   "payload1",
	}
	task2 := Task{
		ID:        "task2",
		ExecuteAt: now.Add(10 * time.Second),
		Payload:   "payload2",
	}

	// Test Save
	err := store.Save(task1)
	require.NoError(t, err)
	err = store.Save(task2)
	require.NoError(t, err)

	// Test GetDueTasks
	tasks, err := store.GetDueTasks(now, now.Add(15*time.Second))
	require.NoError(t, err)
	assert.Len(t, tasks, 2)

	// Test time range filtering
	tasks, err = store.GetDueTasks(now, now.Add(7*time.Second))
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task1", tasks[0].ID)

	// Test Delete
	err = store.Delete(task1.ID)
	require.NoError(t, err)
	tasks, err = store.GetDueTasks(now, now.Add(15*time.Second))
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task2", tasks[0].ID)
}

func TestStatusLifecycle(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task := Task{ID: "t1", ExecuteAt: now.Add(5 * time.Second), URL: "http://x/1"}
	require.NoError(t, store.Save(task))

	// Save forces pending, and pending tasks show up as due.
	got, err := store.GetTask("t1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, StatusPending, got.Status)

	due, err := store.GetDueTasks(now, now.Add(15*time.Second))
	require.NoError(t, err)
	assert.Len(t, due, 1)

	// Transition to a terminal status: it must leave the pending keyspace so
	// the due-query never re-fires it.
	got.Status = StatusSucceeded
	got.Attempts = []Attempt{{N: 1, StatusCode: 200}}
	require.NoError(t, store.Update(*got))

	due, err = store.GetDueTasks(now, now.Add(15*time.Second))
	require.NoError(t, err)
	assert.Empty(t, due, "terminal task must not be due")

	// Still queryable by id and by status filter.
	got, err = store.GetTask("t1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, StatusSucceeded, got.Status)
	assert.Len(t, got.Attempts, 1)

	succeeded, _, err := store.ListTasks(ListFilter{Status: string(StatusSucceeded)}, "", 0)
	require.NoError(t, err)
	assert.Len(t, succeeded, 1)

	pending, _, err := store.ListTasks(ListFilter{Status: string(StatusPending)}, "", 0)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestRecoverRunning(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	// A recovered task is always past-due (it was already due when it ran).
	task := Task{ID: "t1", ExecuteAt: now.Add(-5 * time.Second)}
	require.NoError(t, store.Save(task))

	// Simulate a crash mid-run: task stuck in running.
	task.Status = StatusRunning
	require.NoError(t, store.Update(task))
	due, _ := store.GetDueTasks(now, now.Add(15*time.Second))
	require.Empty(t, due)

	// Recovery re-queues it to pending, and the due-scan must catch it up
	// despite its ExecuteAt being in the past.
	require.NoError(t, store.RecoverRunning())
	due, err := store.GetDueTasks(now, now.Add(15*time.Second))
	require.NoError(t, err)
	require.Len(t, due, 1)
	assert.Equal(t, StatusPending, due[0].Status)
}

func TestKeyOrdering(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	tasks := []Task{
		{ID: "task3", ExecuteAt: now.Add(30 * time.Second)},
		{ID: "task1", ExecuteAt: now.Add(10 * time.Second)},
		{ID: "task2", ExecuteAt: now.Add(20 * time.Second)},
	}

	// Save out of order
	for _, task := range tasks {
		require.NoError(t, store.Save(task))
	}

	// Should come back in chronological order
	result, err := store.GetDueTasks(now, now.Add(1*time.Minute))
	require.NoError(t, err)
	require.Len(t, result, 3)
	assert.Equal(t, "task1", result[0].ID)
	assert.Equal(t, "task2", result[1].ID)
	assert.Equal(t, "task3", result[2].ID)
}

func TestEmptyResults(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	tasks, err := store.GetDueTasks(time.Now(), time.Now().Add(1*time.Hour))
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestGetTask(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task := Task{
		ID:        "task1",
		ExecuteAt: now.Add(5 * time.Second),
		Payload:   "payload1",
	}

	// Save task
	err := store.Save(task)
	require.NoError(t, err)

	// Test GetTask - found
	retrieved, err := store.GetTask(task.ID)
	require.NoError(t, err)
	require.NotNil(t, retrieved)
	assert.Equal(t, task.ID, retrieved.ID)
	assert.Equal(t, task.Payload, retrieved.Payload)
	assert.Equal(t, task.ExecuteAt.Unix(), retrieved.ExecuteAt.Unix())

	// Test GetTask - not found
	notFound, err := store.GetTask("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, notFound)
}

func TestDeleteTasks(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task1 := Task{
		ID:        "task1",
		ExecuteAt: now.Add(5 * time.Second),
		URL:       "http://example.com/webhook1",
	}
	task2 := Task{
		ID:        "task2",
		ExecuteAt: now.Add(10 * time.Second),
		URL:       "http://example.com/webhook2",
	}
	task3 := Task{
		ID:        "task3",
		ExecuteAt: now.Add(15 * time.Second),
		URL:       "http://example.com/webhook1",
	}
	task4 := Task{
		ID:        "task4",
		ExecuteAt: now.Add(20 * time.Second),
		URL:       "http://different.com/webhook",
	}

	// Save all tasks
	for _, task := range []Task{task1, task2, task3, task4} {
		require.NoError(t, store.Save(task))
	}

	t.Run("delete by URL", func(t *testing.T) {
		count, err := store.DeleteTasks("http://example.com/webhook1", nil, nil)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// Verify task1 and task3 are deleted
		retrieved, _ := store.GetTask(task1.ID)
		assert.Nil(t, retrieved)
		retrieved, _ = store.GetTask(task3.ID)
		assert.Nil(t, retrieved)

		// Verify task2 and task4 still exist
		retrieved, _ = store.GetTask(task2.ID)
		assert.NotNil(t, retrieved)
		retrieved, _ = store.GetTask(task4.ID)
		assert.NotNil(t, retrieved)
	})
}

func TestDeleteTasksByTimeRange(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task1 := Task{ID: "task1", ExecuteAt: now.Add(5 * time.Second), URL: "http://example.com/1"}
	task2 := Task{ID: "task2", ExecuteAt: now.Add(10 * time.Second), URL: "http://example.com/2"}
	task3 := Task{ID: "task3", ExecuteAt: now.Add(15 * time.Second), URL: "http://example.com/3"}
	task4 := Task{ID: "task4", ExecuteAt: now.Add(20 * time.Second), URL: "http://example.com/4"}

	for _, task := range []Task{task1, task2, task3, task4} {
		require.NoError(t, store.Save(task))
	}

	t.Run("delete before time", func(t *testing.T) {
		before := now.Add(12 * time.Second)
		count, err := store.DeleteTasks("", &before, nil)
		require.NoError(t, err)
		assert.Equal(t, 2, count) // task1 and task2

		// Verify deleted
		retrieved, _ := store.GetTask(task1.ID)
		assert.Nil(t, retrieved)
		retrieved, _ = store.GetTask(task2.ID)
		assert.Nil(t, retrieved)

		// Verify still exist
		retrieved, _ = store.GetTask(task3.ID)
		assert.NotNil(t, retrieved)
		retrieved, _ = store.GetTask(task4.ID)
		assert.NotNil(t, retrieved)
	})
}

func TestDeleteTasksAfterTime(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task1 := Task{ID: "task1", ExecuteAt: now.Add(5 * time.Second), URL: "http://example.com/1"}
	task2 := Task{ID: "task2", ExecuteAt: now.Add(10 * time.Second), URL: "http://example.com/2"}
	task3 := Task{ID: "task3", ExecuteAt: now.Add(15 * time.Second), URL: "http://example.com/3"}

	for _, task := range []Task{task1, task2, task3} {
		require.NoError(t, store.Save(task))
	}

	after := now.Add(12 * time.Second)
	count, err := store.DeleteTasks("", nil, &after)
	require.NoError(t, err)
	assert.Equal(t, 1, count) // only task3

	// Verify task3 deleted
	retrieved, _ := store.GetTask(task3.ID)
	assert.Nil(t, retrieved)

	// Verify task1 and task2 still exist
	retrieved, _ = store.GetTask(task1.ID)
	assert.NotNil(t, retrieved)
	retrieved, _ = store.GetTask(task2.ID)
	assert.NotNil(t, retrieved)
}

func TestDeleteTasksCombinedFilters(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task1 := Task{ID: "task1", ExecuteAt: now.Add(5 * time.Second), URL: "http://example.com/webhook"}
	task2 := Task{ID: "task2", ExecuteAt: now.Add(10 * time.Second), URL: "http://example.com/webhook"}
	task3 := Task{ID: "task3", ExecuteAt: now.Add(15 * time.Second), URL: "http://example.com/webhook"}
	task4 := Task{ID: "task4", ExecuteAt: now.Add(20 * time.Second), URL: "http://other.com/webhook"}

	for _, task := range []Task{task1, task2, task3, task4} {
		require.NoError(t, store.Save(task))
	}

	// Delete tasks with specific URL and in time range [8s, 18s]
	before := now.Add(18 * time.Second)
	after := now.Add(8 * time.Second)
	count, err := store.DeleteTasks("http://example.com/webhook", &before, &after)
	require.NoError(t, err)
	assert.Equal(t, 2, count) // task2 and task3

	// Verify task2 and task3 deleted
	retrieved, _ := store.GetTask(task2.ID)
	assert.Nil(t, retrieved)
	retrieved, _ = store.GetTask(task3.ID)
	assert.Nil(t, retrieved)

	// Verify task1 (wrong time) and task4 (wrong URL) still exist
	retrieved, _ = store.GetTask(task1.ID)
	assert.NotNil(t, retrieved)
	retrieved, _ = store.GetTask(task4.ID)
	assert.NotNil(t, retrieved)
}

func TestDeleteTasksNoMatches(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	task := Task{ID: "task1", ExecuteAt: now.Add(10 * time.Second), URL: "http://example.com/webhook"}
	require.NoError(t, store.Save(task))

	// Delete with non-matching URL
	count, err := store.DeleteTasks("http://nonexistent.com/webhook", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Verify task still exists
	retrieved, _ := store.GetTask(task.ID)
	assert.NotNil(t, retrieved)
}

// Paging must walk the whole store exactly once - no row returned twice, none
// skipped - and it must stay correct across the seams: a page boundary that
// lands exactly on the last row, a cursor whose row was deleted mid-walk, and a
// cursor pointed at a different status partition than the query asks for.
func TestListTasksPagination(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	const total = 25
	for i := 0; i < total; i++ {
		require.NoError(t, store.Save(Task{
			ID:        fmt.Sprintf("t%02d", i),
			URL:       "http://example.com",
			ExecuteAt: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	// walk pages the whole store at the given size and returns the ids in order.
	walk := func(t *testing.T, limit int) []string {
		t.Helper()
		var ids []string
		cursor := ""
		for pages := 0; ; pages++ {
			require.Less(t, pages, total+2, "paging failed to terminate")

			page, next, err := store.ListTasks(ListFilter{}, cursor, limit)
			require.NoError(t, err)
			for _, task := range page {
				ids = append(ids, task.ID)
			}
			if next == "" {
				break
			}
			require.NotEmpty(t, page, "a cursor was handed back for an empty page")
			cursor = next
		}
		return ids
	}

	var want []string
	for i := 0; i < total; i++ {
		want = append(want, fmt.Sprintf("t%02d", i))
	}

	// 25 rows over sizes that divide evenly (5), leave a remainder (7), take one
	// at a time, and exceed the total in a single page.
	for _, limit := range []int{1, 5, 7, total, total + 10} {
		t.Run(fmt.Sprintf("limit %d covers every task once", limit), func(t *testing.T) {
			assert.Equal(t, want, walk(t, limit))
		})
	}

	t.Run("limit defaults and clamps", func(t *testing.T) {
		page, _, err := store.ListTasks(ListFilter{}, "", 0)
		require.NoError(t, err)
		assert.Len(t, page, total, "<=0 means the default page size, not zero rows")

		page, _, err = store.ListTasks(ListFilter{}, "", MaxPageSize+1)
		require.NoError(t, err)
		assert.Len(t, page, total, "an over-large limit clamps rather than erroring")
	})

	t.Run("a cursor survives its own row being deleted", func(t *testing.T) {
		first, next, err := store.ListTasks(ListFilter{}, "", 5)
		require.NoError(t, err)
		require.NotEmpty(t, next)
		require.NoError(t, store.Delete(first[len(first)-1].ID)) // the cursor row itself

		page, _, err := store.ListTasks(ListFilter{}, next, 5)
		require.NoError(t, err)
		require.NotEmpty(t, page)
		assert.Equal(t, "t05", page[0].ID, "the page resumes after the deleted cursor")

		require.NoError(t, store.Save(first[len(first)-1])) // restore for later subtests
	})

	t.Run("rejects a malformed cursor", func(t *testing.T) {
		_, _, err := store.ListTasks(ListFilter{}, "not-base64!!", 5)
		assert.ErrorIs(t, err, ErrInvalidCursor)
	})

	t.Run("rejects a cursor from another status partition", func(t *testing.T) {
		_, next, err := store.ListTasks(ListFilter{Status: string(StatusPending)}, "", 5)
		require.NoError(t, err)
		require.NotEmpty(t, next)

		// Same cursor, different status: paging on would walk the wrong keyspace.
		_, _, err = store.ListTasks(ListFilter{Status: string(StatusSucceeded)}, next, 5)
		assert.ErrorIs(t, err, ErrInvalidCursor)
	})

	t.Run("a status filter pages only its own partition", func(t *testing.T) {
		done, err := store.GetTask("t00")
		require.NoError(t, err)
		done.Status = StatusSucceeded
		require.NoError(t, store.Update(*done))

		succeeded, next, err := store.ListTasks(ListFilter{Status: string(StatusSucceeded)}, "", 1)
		require.NoError(t, err)
		assert.Empty(t, next, "one row, one page")
		require.Len(t, succeeded, 1)
		assert.Equal(t, "t00", succeeded[0].ID)
	})
}

// Counts must tally from the key index alone and must agree with what the store
// actually holds, including after tasks move between status partitions.
func TestCounts(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	require.NoError(t, store.Save(Task{ID: "late1", ExecuteAt: now.Add(-2 * time.Minute)}))
	require.NoError(t, store.Save(Task{ID: "late2", ExecuteAt: now.Add(-1 * time.Minute)}))
	require.NoError(t, store.Save(Task{ID: "soon", ExecuteAt: now.Add(time.Hour)}))

	counts, err := store.Counts(now)
	require.NoError(t, err)
	assert.Equal(t, 3, counts.ByStatus[StatusPending])
	assert.Equal(t, 2, counts.Overdue, "only past-due pending tasks are backlog")

	// A task that finishes leaves the pending partition, and with it the backlog.
	done, err := store.GetTask("late1")
	require.NoError(t, err)
	done.Status = StatusSucceeded
	require.NoError(t, store.Update(*done))

	counts, err = store.Counts(now)
	require.NoError(t, err)
	assert.Equal(t, 2, counts.ByStatus[StatusPending])
	assert.Equal(t, 1, counts.ByStatus[StatusSucceeded])
	assert.Equal(t, 1, counts.Overdue)

	// A future-dated task is never backlog, however many there are.
	counts, err = store.Counts(now.Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, counts.Overdue, "nothing is overdue before anything is due")
}

func TestCountsEmptyStore(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	counts, err := store.Counts(time.Now())
	require.NoError(t, err)
	assert.Empty(t, counts.ByStatus)
	assert.Zero(t, counts.Overdue)
}

// A URL filter must return only matching rows, count the page limit against
// matches (not scanned rows), and keep the cursor walk exhaustive.
func TestListTasksURLFilter(t *testing.T) {
	store, cleanup := setupBadgerDB(t)
	defer cleanup()

	now := time.Now()
	const total = 20
	for i := 0; i < total; i++ {
		url := "http://a.example.com"
		if i%2 == 1 {
			url = "http://b.example.com"
		}
		require.NoError(t, store.Save(Task{
			ID:        fmt.Sprintf("u%02d", i),
			URL:       url,
			ExecuteAt: now.Add(time.Duration(i) * time.Minute),
		}))
	}

	t.Run("returns only matching rows", func(t *testing.T) {
		page, next, err := store.ListTasks(ListFilter{URL: "http://a.example.com"}, "", 0)
		require.NoError(t, err)
		assert.Empty(t, next)
		assert.Len(t, page, total/2)
		for _, task := range page {
			assert.Equal(t, "http://a.example.com", task.URL)
		}
	})

	t.Run("limit counts matches and cursor continues the walk", func(t *testing.T) {
		var ids []string
		cursor := ""
		for {
			page, next, err := store.ListTasks(ListFilter{URL: "http://b.example.com"}, cursor, 3)
			require.NoError(t, err)
			for _, task := range page {
				assert.Equal(t, "http://b.example.com", task.URL)
				ids = append(ids, task.ID)
			}
			if next == "" {
				break
			}
			cursor = next
		}
		assert.Len(t, ids, total/2)
	})

	t.Run("no match is an empty page, not an error", func(t *testing.T) {
		page, next, err := store.ListTasks(ListFilter{URL: "http://nope.example.com"}, "", 0)
		require.NoError(t, err)
		assert.Empty(t, page)
		assert.Empty(t, next)
	})
}
