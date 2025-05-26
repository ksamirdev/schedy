package executor

import (
	"bytes"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

type Executor struct {
	client *http.Client
}

func NewExecutor() *Executor {
	return &Executor{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (e *Executor) Execute(task scheduler.Task) error {
	j, _ := json.Marshal(task.Payload)
	req, err := http.NewRequest(http.MethodPost, task.URL, bytes.NewBuffer(j))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	_, err = e.client.Do(req)
	return err
}
