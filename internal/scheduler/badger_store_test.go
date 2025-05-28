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
