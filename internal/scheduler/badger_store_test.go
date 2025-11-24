package scheduler

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBadgerDB(t *testing.T) (*BadgerStore, func()) {
	path := "./testdb_" + uuid.New().String()

	store, err := NewBadgerStore(path)
	require.NoError(t, err)

	cleanup := func() {
		store.db.Close()
		os.RemoveAll(path)
	}

	return store, cleanup
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
	err = store.Delete(task1.ID, task1.ExecuteAt.Unix())
	require.NoError(t, err)
	tasks, err = store.GetDueTasks(now, now.Add(15*time.Second))
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "task2", tasks[0].ID)
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
