package runner

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

type Runner struct {
	ticker   Ticker
	store    scheduler.Store
	executor *executor.Executor
	interval time.Duration
	// onFailureURL, if set (SCHEDY_ON_FAILURE_URL), receives a best-effort POST
	// whenever a task exhausts its retries and reaches the failed state.
	onFailureURL string
}

func New(store scheduler.Store, executor *executor.Executor, interval time.Duration) *Runner {
	return &Runner{
		ticker:       NewTicker(interval),
		store:        store,
		executor:     executor,
		interval:     interval,
		onFailureURL: os.Getenv("SCHEDY_ON_FAILURE_URL"),
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
			taskTime := time.Until(t.ExecuteAt)
			if max(taskTime, 0) == 0 {
				taskTime = 0
			}

			timer := NewTimer(taskTime)
			defer timer.Stop()

			<-timer.C()

			// The task may have been cancelled or updated after it was picked up
			// but before its timer fired. Re-read its current state: a cancel
			// (or any non-pending status) wins the race, a reschedule drops this
			// run so the next tick picks the task up at its new time, and any
			// other edit fires the fresh field values instead of the stale copy.
			// ponytail: residual TOCTOU between this read and the Update below
			// (single process, microsecond window); add a CAS status transition
			// in the store if that window ever matters.
			cur, err := r.store.GetTask(t.ID)
			if err != nil {
				log.Printf("re-read task %s before firing: %v", t.ID, err)
				return
			}
			// Cancelled or deleted mid-flight: expected, not an error.
			if cur == nil || cur.Status != scheduler.StatusPending {
				return
			}
			if !cur.ExecuteAt.Equal(t.ExecuteAt) {
				log.Printf("Task %s rescheduled to %s, skipping this run", t.ID, cur.ExecuteAt.Format(time.RFC3339))
				return
			}
			t = *cur

			// Built from the re-read copy so an update to the retry settings
			// takes effect on this run rather than the next one.
			attempt := newAttempt(t.Retries, t.RetryInterval, t.RetryMode)

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
					att.ResponseBody = res.ResponseBody
					att.ResponseBodyTruncated = res.ResponseBodyTruncated
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

			if t.Status == scheduler.StatusFailed {
				r.notifyFailure(t)
			}
		}(task, i)
	}
}

// notifyFailure fires a single best-effort POST to SCHEDY_ON_FAILURE_URL when a
// task exhausts its retries, so a permanent failure is not silent. Fire-and-
// forget: the callback is never retried, and a failing callback never triggers
// a callback about itself.
func (r *Runner) notifyFailure(t scheduler.Task) {
	if r.onFailureURL == "" || len(t.Attempts) == 0 {
		return
	}
	last := t.Attempts[len(t.Attempts)-1]
	res := r.executor.Execute(scheduler.Task{
		URL:    r.onFailureURL,
		Method: http.MethodPost,
		Payload: map[string]any{
			"id":          t.ID,
			"status":      t.Status,
			"attempts":    len(t.Attempts),
			"last_error":  last.Error,
			"status_code": last.StatusCode,
		},
	})
	if res.Err != nil {
		log.Printf("failure callback for %s: %v", t.ID, res.Err)
	}
}
