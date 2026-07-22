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
	interval time.Duration
}

func New(store scheduler.Store, executor *executor.Executor, interval time.Duration) *Runner {
	return &Runner{
		ticker:   NewTicker(interval),
		store:    store,
		executor: executor,
		interval: interval,
	}
}

func (r *Runner) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			r.ticker.Stop()
			return
		case now := <-r.ticker.C():
			r.runOnce(now, now.Add(r.interval))
		}
	}
}

func (r *Runner) runOnce(start, end time.Time) {
	tasks, err := r.store.GetDueTasks(start, end)
	if err != nil {
		log.Println("Failed to get due tasks:", err)
		return
	}

	for i, task := range tasks {
		go func(t scheduler.Task, idx int) {
			attempt := newAttempt(t.Retries, t.RetryInterval)
			taskTime := time.Until(t.ExecuteAt)
			if max(taskTime, 0) == 0 {
				taskTime = 0
			}

			timer := NewTimer(taskTime)
			defer timer.Stop()

			<-timer.C()

			// The task may have been cancelled after it was picked up but
			// before its timer fired. Re-read its current state and skip if it
			// is no longer pending, so cancel wins the race.
			// ponytail: residual TOCTOU between this read and the Update below
			// (single process, microsecond window); add a CAS status transition
			// in the store if that window ever matters.
			if cur, err := r.store.GetTask(t.ID); err != nil || cur == nil || cur.Status != scheduler.StatusPending {
				return
			}

			log.Printf("#%d | Executing task: %s", idx, t.ID)

			t.Status = scheduler.StatusRunning
			if err := r.store.Update(t); err != nil {
				log.Printf("mark running %s: %v", t.ID, err)
			}

			n := 0
			for {
				n++
				res := r.executor.Execute(t)
				att := scheduler.Attempt{
					N:          n,
					FiredAt:    time.Now().UTC(),
					StatusCode: res.StatusCode,
					DurationMs: res.Duration.Milliseconds(),
				}
				if res.Err != nil {
					att.Error = res.Err.Error()
				}
				t.Attempts = append(t.Attempts, att)

				if res.Err == nil {
					t.Status = scheduler.StatusSucceeded
					break
				}
				if attempt.next() {
					log.Printf("Retrying task: %s (attempt %d/%d)", t.ID, attempt.count, attempt.strategy.retries)
					continue
				}
				t.Status = scheduler.StatusFailed
				break
			}

			now := time.Now().UTC()
			t.FinishedAt = &now
			if err := r.store.Update(t); err != nil {
				log.Printf("finalize %s (%s): %v", t.ID, t.Status, err)
			}
		}(task, i)
	}
}
