package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
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
	task.Status = scheduler.StatusPending
	m.tasks[task.ID] = task
	return nil
}

func (m *mockStore) Update(task scheduler.Task) error {
	m.tasks[task.ID] = task
	return nil
}

func (m *mockStore) RecoverRunning() error {
	for id, task := range m.tasks {
		if task.Status == scheduler.StatusRunning {
			task.Status = scheduler.StatusPending
			m.tasks[id] = task
		}
	}
	return nil
}

func (m *mockStore) GetDueTasks(start, end time.Time) ([]scheduler.Task, error) {
	var tasks []scheduler.Task
	for _, task := range m.tasks {
		if task.Status == scheduler.StatusPending && !task.ExecuteAt.After(end) {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func (m *mockStore) Delete(id string) error {
	delete(m.tasks, id)
	return nil
}

func (m *mockStore) GetTask(id string) (*scheduler.Task, error) {
	if task, exists := m.tasks[id]; exists {
		return &task, nil
	}
	return nil, nil
}

func (m *mockStore) ListTasks(status string) ([]scheduler.Task, error) {
	var tasks []scheduler.Task
	for _, task := range m.tasks {
		if status != "" && string(task.Status) != status {
			continue
		}
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

		// Try to create duplicate task. No Idempotency-Key: this is the
		// implicit same-url-same-time match. Key behaviour has its own test.
		body2, _ := json.Marshal(reqBody)
		req2 := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(body2))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("X-API-Key", "test-api-key")
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

func TestCreateTaskDeduplication(t *testing.T) {
	postReq := func(key string, body any) *http.Request {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key")
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}
		return req
	}

	post := func(t *testing.T, handler *Handler, key string, body any) (int, scheduler.Task) {
		t.Helper()
		w := httptest.NewRecorder()
		handler.CreateTask(w, postReq(key, body))
		var task scheduler.Task
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &task))
		return w.Code, task
	}

	newHandler := func() *Handler {
		handler := New(newMockStore())
		handler.APIKey = "test-api-key"
		return handler
	}

	t.Run("an idempotency key matches on the key alone", func(t *testing.T) {
		handler := newHandler()

		code, first := post(t, handler, "key-123", map[string]any{
			"url":        "http://example.com/a",
			"execute_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
		require.Equal(t, http.StatusCreated, code)
		assert.Equal(t, "key-123", first.IdempotencyKey)

		// Same key, completely different schedule: still the same task.
		code, second := post(t, handler, "key-123", map[string]any{
			"url":        "http://example.com/totally-different",
			"execute_at": time.Now().Add(9 * time.Hour).Format(time.RFC3339),
		})
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, first.ID, second.ID)
		assert.Equal(t, "http://example.com/a", second.URL, "the original schedule is returned untouched")
	})

	t.Run("a different idempotency key creates a new task", func(t *testing.T) {
		handler := newHandler()
		body := map[string]any{
			"url":        "http://example.com/a",
			"execute_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		}

		code, first := post(t, handler, "key-a", body)
		require.Equal(t, http.StatusCreated, code)

		code, second := post(t, handler, "key-b", body)
		assert.Equal(t, http.StatusCreated, code, "the key is the identity, so an identical schedule is not a duplicate")
		assert.NotEqual(t, first.ID, second.ID)
	})

	t.Run("without a key, near-identical schedules deduplicate", func(t *testing.T) {
		handler := newHandler()
		base := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

		// 100ms apart, but either side of a second boundary.
		code, first := post(t, handler, "", map[string]any{
			"url":        "http://example.com/a",
			"execute_at": base.Add(900 * time.Millisecond).Format(time.RFC3339Nano),
		})
		require.Equal(t, http.StatusCreated, code)

		code, second := post(t, handler, "", map[string]any{
			"url":        "http://example.com/a",
			"execute_at": base.Add(1 * time.Second).Format(time.RFC3339Nano),
		})
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, first.ID, second.ID)
	})

	t.Run("without a key, schedules a second or more apart are distinct", func(t *testing.T) {
		handler := newHandler()
		base := time.Now().Add(1 * time.Hour).UTC().Truncate(time.Second)

		code, first := post(t, handler, "", map[string]any{
			"url":        "http://example.com/a",
			"execute_at": base.Format(time.RFC3339),
		})
		require.Equal(t, http.StatusCreated, code)

		code, second := post(t, handler, "", map[string]any{
			"url":        "http://example.com/a",
			"execute_at": base.Add(2 * time.Second).Format(time.RFC3339),
		})
		assert.Equal(t, http.StatusCreated, code)
		assert.NotEqual(t, first.ID, second.ID)
	})

	t.Run("without a key, a different url is not a duplicate", func(t *testing.T) {
		handler := newHandler()
		executeAt := time.Now().Add(1 * time.Hour).Format(time.RFC3339)

		code, first := post(t, handler, "", map[string]any{"url": "http://example.com/a", "execute_at": executeAt})
		require.Equal(t, http.StatusCreated, code)

		code, second := post(t, handler, "", map[string]any{"url": "http://example.com/b", "execute_at": executeAt})
		assert.Equal(t, http.StatusCreated, code)
		assert.NotEqual(t, first.ID, second.ID)
	})

	t.Run("only pending tasks are deduplicated against", func(t *testing.T) {
		handler := newHandler()
		store := handler.Store.(*mockStore)
		executeAt := time.Now().Add(1 * time.Hour)

		store.tasks["done"] = scheduler.Task{
			ID:             "done",
			URL:            "http://example.com/a",
			ExecuteAt:      executeAt,
			IdempotencyKey: "key-123",
			Status:         scheduler.StatusSucceeded,
		}

		code, task := post(t, handler, "key-123", map[string]any{
			"url":        "http://example.com/a",
			"execute_at": executeAt.Format(time.RFC3339),
		})
		assert.Equal(t, http.StatusCreated, code)
		assert.NotEqual(t, "done", task.ID)
	})
}

// A burst of concurrent creates carrying the same Idempotency-Key must collapse
// to a single task: exactly one 201, the rest 200, all pointing at one id. Run
// with -race to also catch the unlocked findDuplicate+Save the mutex closes.
func TestCreateTaskConcurrentIdempotency(t *testing.T) {
	handler := New(newMockStore())

	raw, _ := json.Marshal(map[string]any{
		"url":        "http://example.com/a",
		"execute_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	})

	const n = 32
	var wg sync.WaitGroup
	ids := make([]string, n)
	codes := make([]int, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewReader(raw))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "same-key")
			w := httptest.NewRecorder()
			<-start // release all goroutines at once to maximize contention
			handler.CreateTask(w, req)
			var task scheduler.Task
			json.Unmarshal(w.Body.Bytes(), &task)
			ids[i] = task.ID
			codes[i] = w.Code
		}(i)
	}
	close(start)
	wg.Wait()

	all, err := handler.Store.ListTasks("")
	require.NoError(t, err)
	assert.Len(t, all, 1, "concurrent same-key creates must persist exactly one task")

	created := 0
	for i := range ids {
		assert.Equal(t, ids[0], ids[i], "every response must reference the same task id")
		if codes[i] == http.StatusCreated {
			created++
		}
	}
	assert.Equal(t, 1, created, "exactly one request gets 201, the rest 200")
}

func TestUpdateTaskHandler(t *testing.T) {
	// Seeds go in directly rather than through Save, which forces pending.
	newHandler := func(seed ...scheduler.Task) (*mockStore, *Handler) {
		store := newMockStore()
		for _, task := range seed {
			store.tasks[task.ID] = task
		}
		handler := New(store)
		handler.APIKey = "test-api-key"
		return store, handler
	}

	updateReq := func(id string, body any) *http.Request {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/tasks/"+id, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "test-api-key")
		req.SetPathValue("id", id)
		return req
	}

	pendingTask := func() scheduler.Task {
		return scheduler.Task{
			ID:             "task123",
			IdempotencyKey: "key-123",
			URL:            "http://example.com/old",
			ExecuteAt:      time.Now().Add(1 * time.Hour),
			Headers:        map[string]string{"X-Old": "1"},
			Payload:        map[string]any{"old": true},
			Retries:        1,
			RetryInterval:  1000,
			Status:         scheduler.StatusPending,
			// A task recovered from a crash is pending with attempts already
			// logged; that history is not the client's to overwrite.
			Attempts: []scheduler.Attempt{{N: 1, StatusCode: 500, Error: "unexpected status code: 500"}},
		}
	}

	t.Run("replaces the client-owned fields", func(t *testing.T) {
		store, handler := newHandler(pendingTask())
		executeAt := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("task123", map[string]any{
			"url":            "http://example.com/new",
			"execute_at":     executeAt.Format(time.RFC3339),
			"headers":        map[string]string{"X-New": "2"},
			"payload":        map[string]any{"new": true},
			"retries":        5,
			"retry_interval": 7000,
		}))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var resp scheduler.Task
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "task123", resp.ID)
		assert.Equal(t, "http://example.com/new", resp.URL)
		assert.True(t, executeAt.Equal(resp.ExecuteAt))
		assert.Equal(t, map[string]string{"X-New": "2"}, resp.Headers)
		assert.Equal(t, map[string]any{"new": true}, resp.Payload)
		assert.Equal(t, 5, resp.Retries)
		assert.Equal(t, 7000, resp.RetryInterval)

		// Server-owned state survives untouched.
		assert.Equal(t, scheduler.StatusPending, resp.Status)
		assert.Equal(t, "key-123", resp.IdempotencyKey, "the creation-time key is not settable via PUT")
		assert.Len(t, resp.Attempts, 1)
		assert.Nil(t, resp.FinishedAt)

		stored, err := store.GetTask("task123")
		require.NoError(t, err)
		assert.Equal(t, "http://example.com/new", stored.URL)
		assert.Len(t, stored.Attempts, 1)
	})

	t.Run("omitted optional fields are reset", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("task123", map[string]any{
			"url":        "http://example.com/new",
			"execute_at": time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		}))

		assert.Equal(t, http.StatusOK, w.Code)

		var resp scheduler.Task
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Nil(t, resp.Headers)
		assert.Nil(t, resp.Payload)
		assert.Equal(t, 0, resp.Retries)
		assert.Equal(t, DEFAULT_RETRY_INTERVAL, resp.RetryInterval)
	})

	t.Run("rejects non-pending tasks", func(t *testing.T) {
		for _, status := range []scheduler.TaskStatus{
			scheduler.StatusRunning,
			scheduler.StatusSucceeded,
			scheduler.StatusFailed,
			scheduler.StatusCancelled,
		} {
			t.Run(string(status), func(t *testing.T) {
				task := pendingTask()
				task.Status = status
				_, handler := newHandler(task)

				w := httptest.NewRecorder()
				handler.UpdateTask(w, updateReq("task123", map[string]any{
					"url":        "http://example.com/new",
					"execute_at": time.Now().Add(3 * time.Hour).Format(time.RFC3339),
				}))

				assert.Equal(t, http.StatusConflict, w.Code)
			})
		}
	})

	t.Run("task not found", func(t *testing.T) {
		_, handler := newHandler()

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("nonexistent", map[string]any{
			"url":        "http://example.com/new",
			"execute_at": time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		}))

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("missing task id", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		req := httptest.NewRequest(http.MethodPut, "/tasks/", bytes.NewReader([]byte("{}")))
		req.Header.Set("X-API-Key", "test-api-key")
		w := httptest.NewRecorder()

		handler.UpdateTask(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		req := httptest.NewRequest(http.MethodPut, "/tasks/task123", bytes.NewReader([]byte("invalid json")))
		req.Header.Set("X-API-Key", "test-api-key")
		req.SetPathValue("id", "task123")
		w := httptest.NewRecorder()

		handler.UpdateTask(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid time format", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("task123", map[string]any{
			"url":        "http://example.com/new",
			"execute_at": "not-a-time",
		}))

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("past execution time", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("task123", map[string]any{
			"url":        "http://example.com/new",
			"execute_at": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
		}))

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing url", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("task123", map[string]any{
			"execute_at": time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		}))

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("store failure", func(t *testing.T) {
		handler := New(&updateFailingStore{})
		handler.APIKey = "test-api-key"

		w := httptest.NewRecorder()
		handler.UpdateTask(w, updateReq("task123", map[string]any{
			"url":        "http://example.com/new",
			"execute_at": time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		}))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("missing API key", func(t *testing.T) {
		_, handler := newHandler(pendingTask())

		req := updateReq("task123", map[string]any{
			"url":        "http://example.com/new",
			"execute_at": time.Now().Add(3 * time.Hour).Format(time.RFC3339),
		})
		req.Header.Del("X-API-Key")
		w := httptest.NewRecorder()

		handler.WithAuth(handler.UpdateTask)(w, req)

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

		// Soft-cancel: task retained in history, marked cancelled
		retrieved, _ := store.GetTask("task123")
		require.NotNil(t, retrieved)
		assert.Equal(t, scheduler.StatusCancelled, retrieved.Status)
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
		req := httptest.NewRequest(http.MethodDelete, "/tasks?before="+url.QueryEscape(before), nil)
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
		req := httptest.NewRequest(http.MethodDelete, "/tasks?after="+url.QueryEscape(after), nil)
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
		req := httptest.NewRequest(http.MethodDelete, "/tasks?url=http://example.com/webhook&before="+url.QueryEscape(before)+"&after="+url.QueryEscape(after), nil)
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

func TestHealthHandler(t *testing.T) {
	store := newMockStore()
	handler := New(store)

	t.Run("always returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()

		handler.Health(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
	})

	t.Run("no auth required", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		// Intentionally not setting API key header
		w := httptest.NewRecorder()

		handler.Health(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

type failingStore struct{}

func (f *failingStore) Save(task scheduler.Task) error {
	return nil
}

func (f *failingStore) Update(task scheduler.Task) error {
	return nil
}

func (f *failingStore) RecoverRunning() error {
	return nil
}

func (f *failingStore) GetDueTasks(start, end time.Time) ([]scheduler.Task, error) {
	return nil, nil
}

func (f *failingStore) Delete(id string) error {
	return nil
}

func (f *failingStore) GetTask(id string) (*scheduler.Task, error) {
	return nil, nil
}

func (f *failingStore) ListTasks(status string) ([]scheduler.Task, error) {
	return nil, errors.New("database connection failed")
}

func (f *failingStore) DeleteTasks(url string, before, after *time.Time) (int, error) {
	return 0, nil
}

// updateFailingStore hands back a pending task but fails to persist the update.
type updateFailingStore struct{ failingStore }

func (s *updateFailingStore) GetTask(id string) (*scheduler.Task, error) {
	return &scheduler.Task{ID: id, Status: scheduler.StatusPending}, nil
}

func (s *updateFailingStore) Update(task scheduler.Task) error {
	return errors.New("database connection failed")
}

func TestReadyHandler(t *testing.T) {
	t.Run("returns 200 when database accessible", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		handler.Ready(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, w.Body.String())
	})

	t.Run("returns 503 when database fails", func(t *testing.T) {
		handler := New(&failingStore{})

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		handler.Ready(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("no auth required", func(t *testing.T) {
		store := newMockStore()
		handler := New(store)

		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()

		handler.Ready(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
