package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStore implements the Store interface for testing
type mockStore struct {
	tasks map[string]scheduler.Task
}

func newMockStore() *mockStore {
	return &mockStore{
		tasks: make(map[string]scheduler.Task),
	}
}

func (m *mockStore) Save(task scheduler.Task) error {
	m.tasks[task.ID] = task
	return nil
}

func (m *mockStore) GetDueTasks(start, end time.Time) ([]scheduler.Task, error) {
	var tasks []scheduler.Task
	for _, task := range m.tasks {
		if task.ExecuteAt.After(start) && task.ExecuteAt.Before(end) {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (m *mockStore) Delete(id string, executeAt int64) error {
	delete(m.tasks, id)
	return nil
}

func (m *mockStore) GetTask(id string) (*scheduler.Task, error) {
	if task, exists := m.tasks[id]; exists {
		return &task, nil
	}
	return nil, nil
}

func (m *mockStore) ListTasks() ([]scheduler.Task, error) {
	var tasks []scheduler.Task
	for _, task := range m.tasks {
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (m *mockStore) DeleteTasks(url string, before, after *time.Time) (int, error) {
	count := 0
	toDelete := []string{}
	
	for id, task := range m.tasks {
		match := true
		
		if url != "" && task.URL != url {
			match = false
		}
		
		if before != nil && !task.ExecuteAt.Before(*before) {
			match = false
		}
		
		if after != nil && !task.ExecuteAt.After(*after) {
			match = false
		}
		
		if match {
			toDelete = append(toDelete, id)
			count++
		}
	}
	
	for _, id := range toDelete {
		delete(m.tasks, id)
	}
	
	return count, nil
}

func TestCreateTaskHandler(t *testing.T) {
	store := newMockStore()
	handler := New(store)
	handler.APIKey = "test-api-key"

	t.Run("successful creation", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"url":        "http://example.com/webhook",
			"execute_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.CreateTask(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)

		var resp scheduler.Task
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ID)
	})

	t.Run("missing API key", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"url":        "http://example.com/webhook",
			"execute_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.WithAuth(handler.CreateTask)(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid API key", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"url":        "http://example.com/webhook",
			"execute_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "wrong-key")
		w := httptest.NewRecorder()

		handler.WithAuth(handler.CreateTask)(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.CreateTask(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid time format", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"url":        "http://example.com/webhook",
			"execute_at": "not-a-time",
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.CreateTask(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("past execution time", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"url":        "http://example.com/webhook",
			"execute_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		}
		body, _ := json.Marshal(reqBody)

		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.CreateTask(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("idempotency - duplicate task", func(t *testing.T) {
		executeAt := time.Now().Add(2 * time.Hour)
		reqBody := map[string]interface{}{
			"url":        "http://example.com/unique-webhook",
			"execute_at": executeAt.Format(time.RFC3339),
		}
		body, _ := json.Marshal(reqBody)

		// Create first task
		req1 := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body))
		req1.Header.Set("Content-Type", "application/json")
		req1.Header.Set("X-API-Key", "test-api-key")
		w1 := httptest.NewRecorder()
		handler.CreateTask(w1, req1)
		assert.Equal(t, http.StatusCreated, w1.Code)

		var firstTask scheduler.Task
		json.Unmarshal(w1.Body.Bytes(), &firstTask)

		// Try to create duplicate task
		body2, _ := json.Marshal(reqBody)
		req2 := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body2))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("X-API-Key", "test-api-key")
		req2.Header.Set("Idempotency-Key", "test-key-123")
		w2 := httptest.NewRecorder()
		handler.CreateTask(w2, req2)

		// Should return 200 (not 201) and return existing task
		assert.Equal(t, http.StatusOK, w2.Code)

		var returnedTask scheduler.Task
		json.Unmarshal(w2.Body.Bytes(), &returnedTask)
		assert.Equal(t, firstTask.ID, returnedTask.ID)
	})
}

func TestListTasksHandler(t *testing.T) {
	store := newMockStore()
	handler := New(store)
	handler.APIKey = "test-api-key"

	// Add some tasks
	now := time.Now()
	task1 := scheduler.Task{ID: "task1", ExecuteAt: now.Add(5 * time.Second), URL: "http://example.com/1"}
	task2 := scheduler.Task{ID: "task2", ExecuteAt: now.Add(10 * time.Second), URL: "http://example.com/2"}
	store.Save(task1)
	store.Save(task2)

	t.Run("successful list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.ListTasks(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp []scheduler.Task
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Len(t, resp, 2)
	})

	t.Run("missing API key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
		w := httptest.NewRecorder()

		handler.WithAuth(handler.ListTasks)(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid API key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
		req.Header.Set("X-API-Key", "wrong-key")
		w := httptest.NewRecorder()

		handler.WithAuth(handler.ListTasks)(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

func TestGetTaskHandler(t *testing.T) {
	store := newMockStore()
	handler := New(store)
	handler.APIKey = "test-api-key"

	now := time.Now()
	task := scheduler.Task{
		ID:        "task123",
		ExecuteAt: now.Add(1 * time.Hour),
		URL:       "http://example.com/webhook",
	}
	store.Save(task)

	t.Run("successful get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tasks/task123", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		req.SetPathValue("id", "task123")
		w := httptest.NewRecorder()

		handler.GetTask(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp scheduler.Task
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "task123", resp.ID)
		assert.Equal(t, "http://example.com/webhook", resp.URL)
	})

	t.Run("task not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tasks/nonexistent", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		handler.GetTask(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("missing API key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/tasks/task123", nil)
		req.SetPathValue("id", "task123")
		w := httptest.NewRecorder()

		handler.WithAuth(handler.GetTask)(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteTaskHandler(t *testing.T) {
	store := newMockStore()
	handler := New(store)
	handler.APIKey = "test-api-key"

	now := time.Now()
	task := scheduler.Task{
		ID:        "task123",
		ExecuteAt: now.Add(1 * time.Hour),
		URL:       "http://example.com/webhook",
	}
	store.Save(task)

	t.Run("successful delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/tasks/task123", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		req.SetPathValue("id", "task123")
		w := httptest.NewRecorder()

		handler.DeleteTask(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify task was deleted
		retrieved, _ := store.GetTask("task123")
		assert.Nil(t, retrieved)
	})

	t.Run("task not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/tasks/nonexistent", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		req.SetPathValue("id", "nonexistent")
		w := httptest.NewRecorder()

		handler.DeleteTask(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("missing API key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/tasks/task123", nil)
		req.SetPathValue("id", "task123")
		w := httptest.NewRecorder()

		handler.WithAuth(handler.DeleteTask)(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDeleteTasksHandler(t *testing.T) {
	t.Run("delete by URL", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		now := time.Now()
		task1 := scheduler.Task{ID: "task1", ExecuteAt: now.Add(1 * time.Hour), URL: "http://example.com/webhook"}
		task2 := scheduler.Task{ID: "task2", ExecuteAt: now.Add(2 * time.Hour), URL: "http://example.com/webhook"}
		task3 := scheduler.Task{ID: "task3", ExecuteAt: now.Add(3 * time.Hour), URL: "http://other.com/webhook"}
		store.Save(task1)
		store.Save(task2)
		store.Save(task3)

		req := httptest.NewRequest(http.MethodDelete, "/tasks?url=http://example.com/webhook", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]int
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 2, resp["deleted"])
	})

	t.Run("delete by before time", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		now := time.Now()
		task1 := scheduler.Task{ID: "task1", ExecuteAt: now.Add(1 * time.Hour), URL: "http://example.com/1"}
		task2 := scheduler.Task{ID: "task2", ExecuteAt: now.Add(2 * time.Hour), URL: "http://example.com/2"}
		task3 := scheduler.Task{ID: "task3", ExecuteAt: now.Add(3 * time.Hour), URL: "http://example.com/3"}
		store.Save(task1)
		store.Save(task2)
		store.Save(task3)

		before := now.Add(90 * time.Minute).Format(time.RFC3339)
		req := httptest.NewRequest(http.MethodDelete, "/tasks?before="+before, nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]int
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 1, resp["deleted"])
	})

	t.Run("delete by after time", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		now := time.Now()
		task1 := scheduler.Task{ID: "task1", ExecuteAt: now.Add(1 * time.Hour), URL: "http://example.com/1"}
		task2 := scheduler.Task{ID: "task2", ExecuteAt: now.Add(2 * time.Hour), URL: "http://example.com/2"}
		task3 := scheduler.Task{ID: "task3", ExecuteAt: now.Add(3 * time.Hour), URL: "http://example.com/3"}
		store.Save(task1)
		store.Save(task2)
		store.Save(task3)

		after := now.Add(90 * time.Minute).Format(time.RFC3339)
		req := httptest.NewRequest(http.MethodDelete, "/tasks?after="+after, nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]int
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 2, resp["deleted"])
	})

	t.Run("delete with combined filters", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		now := time.Now()
		task1 := scheduler.Task{ID: "task1", ExecuteAt: now.Add(1 * time.Hour), URL: "http://example.com/webhook"}
		task2 := scheduler.Task{ID: "task2", ExecuteAt: now.Add(2 * time.Hour), URL: "http://example.com/webhook"}
		task3 := scheduler.Task{ID: "task3", ExecuteAt: now.Add(3 * time.Hour), URL: "http://example.com/webhook"}
		task4 := scheduler.Task{ID: "task4", ExecuteAt: now.Add(4 * time.Hour), URL: "http://other.com/webhook"}
		store.Save(task1)
		store.Save(task2)
		store.Save(task3)
		store.Save(task4)

		before := now.Add(150 * time.Minute).Format(time.RFC3339)
		after := now.Add(30 * time.Minute).Format(time.RFC3339)
		req := httptest.NewRequest(http.MethodDelete, "/tasks?url=http://example.com/webhook&before="+before+"&after="+after, nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]int
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 2, resp["deleted"]) // task1 and task2
	})

	t.Run("no filters provided", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		req := httptest.NewRequest(http.MethodDelete, "/tasks", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid before time", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		req := httptest.NewRequest(http.MethodDelete, "/tasks?before=invalid", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid after time", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		req := httptest.NewRequest(http.MethodDelete, "/tasks?after=invalid", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing API key", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		req := httptest.NewRequest(http.MethodDelete, "/tasks?url=http://example.com", nil)
		w := httptest.NewRecorder()

		handler.WithAuth(handler.DeleteTasks)(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("no matching tasks", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)
		handler.APIKey = "test-api-key"

		now := time.Now()
		task := scheduler.Task{ID: "task1", ExecuteAt: now.Add(1 * time.Hour), URL: "http://example.com/webhook"}
		store.Save(task)

		req := httptest.NewRequest(http.MethodDelete, "/tasks?url=http://nonexistent.com", nil)
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.DeleteTasks(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]int
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, 0, resp["deleted"])
	})
}
