package runner

import (
	"context"
	"log"
	"time"

	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

type Runner struct {
	ticker   Ticker
	store    scheduler.Store
	executor *executor.Executor
}

func New(store scheduler.Store, executor *executor.Executor, interval time.Duration) *Runner {
	return &Runner{
		ticker:   NewTicker(interval),
		store:    store,
		executor: executor,
	}
}

func (r *Runner) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.ticker.Stop()
			return
		case now := <-r.ticker.C():
			r.runOnce(now)
		}
	}
}

func (r *Runner) runOnce(now time.Time) {
	tasks, err := r.store.GetDueTasks(now)
	if err != nil {
		log.Println("Failed to get due tasks:", err)
		return
	}

	for i, task := range tasks {
		go func(t scheduler.Task, idx int) {
			taskTime := time.Until(t.ExecuteAt)
			if max(taskTime, 0) == 0 {
				taskTime = 0
			}

			timer := NewTimer(taskTime)
			defer timer.Stop()

			<-timer.C()

			log.Printf("#%d | Executing task: %s", i, t.ID)
			if err := r.executor.Execute(t); err == nil {
				_ = r.store.Delete(t.ID)
			}

		}(task, i)
	}
}
