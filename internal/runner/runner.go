package runner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ksamirdev/schedy/internal/executor"
	"github.com/ksamirdev/schedy/internal/metrics"
	"github.com/ksamirdev/schedy/internal/scheduler"
)

// DefaultMaxConcurrent bounds how many deliveries may be in flight at once.
// Without a bound, every task due in the same moment fires simultaneously -
// after an outage that is thousands of concurrent requests aimed at the user's
// own API, turning Schedy's recovery into their incident.
const DefaultMaxConcurrent = 50

type Runner struct {
	ticker   Ticker
	store    scheduler.Store
	executor *executor.Executor
	interval time.Duration
	// onFailureURL, if set (SCHEDY_ON_FAILURE_URL), receives a best-effort POST
	// whenever a task exhausts its retries and reaches the failed state.
	onFailureURL string
	// sem bounds concurrent deliveries (SCHEDY_MAX_CONCURRENT_DELIVERIES).
	sem chan struct{}
	// maxStaleness, if set (SCHEDY_MAX_STALENESS), is how far past its
	// execute_at a task may fire. Zero means no limit: catch up everything.
	maxStaleness time.Duration

	// inflight holds the ids currently claimed by a delivery goroutine. A task
	// stays pending in the store until it actually fires, so without this a
	// tick could pick up a task that an earlier tick is already waiting to
	// deliver - a real risk now that a task can sit on the semaphore for a
	// while. Schedy is single-process, so an in-memory claim is enough.
	mu       sync.Mutex
	inflight map[string]struct{}
}

func New(store scheduler.Store, executor *executor.Executor, interval time.Duration) *Runner {
	maxConcurrent := DefaultMaxConcurrent
	if v := os.Getenv("SCHEDY_MAX_CONCURRENT_DELIVERIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			slog.Error("invalid SCHEDY_MAX_CONCURRENT_DELIVERIES", "value", v, "want", "a positive integer")
			os.Exit(1)
		}
		maxConcurrent = n
	}

	var maxStaleness time.Duration
	if v := os.Getenv("SCHEDY_MAX_STALENESS"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			slog.Error("invalid SCHEDY_MAX_STALENESS", "value", v, "want", `a positive Go duration like "1h"`)
			os.Exit(1)
		}
		maxStaleness = d
	}

	return &Runner{
		ticker:       NewTicker(interval),
		store:        store,
		executor:     executor,
		interval:     interval,
		onFailureURL: os.Getenv("SCHEDY_ON_FAILURE_URL"),
		sem:          make(chan struct{}, maxConcurrent),
		maxStaleness: maxStaleness,
		inflight:     make(map[string]struct{}),
	}
}

// claim reserves a task for delivery. It reports false if another goroutine
// already holds it, in which case the caller must not touch the task.
func (r *Runner) claim(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, held := r.inflight[id]; held {
		return false
	}
	r.inflight[id] = struct{}{}
	return true
}

func (r *Runner) release(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, id)
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
	// One bounded batch per tick. A backlog larger than the batch is drained
	// over successive ticks, oldest first, rather than read in at once.
	tasks, err := r.store.GetDueTasks(start, end, scheduler.MaxDueBatch)
	if err != nil {
		slog.Error("get due tasks", "error", err)
		return
	}

	for i, task := range tasks {
		// Already being delivered by an earlier tick: leave it alone.
		if !r.claim(task.ID) {
			continue
		}
		go func(t scheduler.Task, idx int) {
			defer r.release(t.ID)

			taskTime := time.Until(t.ExecuteAt)
			if max(taskTime, 0) == 0 {
				taskTime = 0
			}

			timer := NewTimer(taskTime)
			defer timer.Stop()

			<-timer.C()

			// Wait for a delivery slot. Taken before the fire timestamp on
			// purpose: time spent queued here is time the task is late, and
			// hiding that would make saturation invisible in the one metric
			// meant to show it.
			r.sem <- struct{}{}
			metrics.InflightAdd(1)
			defer func() {
				metrics.InflightAdd(-1)
				<-r.sem
			}()

			fireTime := time.Now().UTC()

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
				slog.Error("re-read task before firing", "task_id", t.ID, "error", err)
				return
			}
			// Cancelled or deleted mid-flight: expected, not an error.
			if cur == nil || cur.Status != scheduler.StatusPending {
				return
			}
			if !cur.ExecuteAt.Equal(t.ExecuteAt) {
				slog.Info("task rescheduled, skipping this run", "task_id", t.ID, "execute_at", cur.ExecuteAt)
				return
			}
			t = *cur

			// Too late to be worth firing: skip rather than deliver. Checked
			// against the real fire time, so a task delayed by a queue of its
			// peers is judged by when it would actually go out.
			late := fireTime.Sub(t.ExecuteAt)
			if r.maxStaleness > 0 && late > r.maxStaleness {
				r.skipStale(t, late, fireTime)
				return
			}

			// Recorded only once the task is committed to firing: a cancelled or
			// rescheduled task never ran, so its wait is not delivery lateness.
			metrics.ObserveLateness(late)

			// Built from the re-read copy so an update to the retry settings
			// takes effect on this run rather than the next one.
			attempt := newAttempt(t.Retries, t.RetryInterval, t.RetryMode)

			slog.Info("executing task", "task_id", t.ID, "url", t.URL, "method", t.Method, "late", late)

			t.Status = scheduler.StatusRunning
			if err := r.store.Update(t); err != nil {
				slog.Error("mark task running", "task_id", t.ID, "error", err)
			}

			// Continue the numbering rather than restarting it: a replayed
			// task keeps its earlier attempts, and two attempts both called
			// "n: 1" make the log unreadable at the moment it matters.
			n := len(t.Attempts)
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
				metrics.ObserveDelivery(res.Duration, res.Err == nil)

				if res.Err == nil {
					t.Status = scheduler.StatusSucceeded
					break
				}
				if attempt.next() {
					slog.Warn("retrying task", "task_id", t.ID, "attempt", attempt.count, "retries", attempt.strategy.retries, "error", res.Err)
					continue
				}
				t.Status = scheduler.StatusFailed
				break
			}

			metrics.ObserveTaskFinished(t.Status == scheduler.StatusSucceeded)

			now := time.Now().UTC()
			t.FinishedAt = &now
			if err := r.store.Update(t); err != nil {
				slog.Error("finalize task", "task_id", t.ID, "status", t.Status, "error", err)
			}

			if t.Status == scheduler.StatusFailed {
				r.notifyFailure(t)
			}

			r.reschedule(t, fireTime)
		}(task, i)
	}
}

// skipStale retires a task that came due too long ago to be worth delivering,
// which after an outage is most of the backlog.
//
// It lands in failed rather than a status of its own: the reason is recorded as
// an attempt, so the audit trail says what happened, and the failure callback
// fires - a skipped task must never be silent, or the outage swallows work with
// no trace. A recurring task still re-enqueues, so an outage interrupts the
// chain rather than ending it.
func (r *Runner) skipStale(t scheduler.Task, late time.Duration, fireTime time.Time) {
	slog.Warn("skipping stale task", "task_id", t.ID, "late", late.Round(time.Second), "max_staleness", r.maxStaleness)

	t.Status = scheduler.StatusFailed
	t.Attempts = append(t.Attempts, scheduler.Attempt{
		N:       len(t.Attempts) + 1,
		FiredAt: fireTime,
		Error: fmt.Sprintf("skipped: %s past execute_at, exceeds max staleness %s",
			late.Round(time.Second), r.maxStaleness),
	})
	t.FinishedAt = &fireTime

	metrics.ObserveSkipped()
	metrics.ObserveTaskFinished(false)

	if err := r.store.Update(t); err != nil {
		slog.Error("finalize skipped task", "task_id", t.ID, "error", err)
	}
	r.notifyFailure(t)
	r.reschedule(t, fireTime)
}

// reschedule re-enqueues a recurring task (Schedule set) as a fresh one-shot at
// fireTime+interval, forming a stateless chain on the existing one-shot engine.
// Interval-only by design: no cron, no catch-up. Anchoring the next fire to
// fireTime (not now) keeps a steady cadence; a task that outran its own interval
// simply becomes due immediately, it is never replayed to catch up missed fires.
//
// A cancelled task never reaches here - the pre-fire re-read returns early on any
// non-pending status - so cancelling the pending task stops the chain.
// ponytail: a cancel landing during the brief run window is overwritten by the
// finalize Update (the same residual TOCTOU noted above); the common case
// (cancel while pending between fires) stops the chain reliably.
func (r *Runner) reschedule(t scheduler.Task, fireTime time.Time) {
	if t.Schedule == "" {
		return
	}
	interval, err := time.ParseDuration(t.Schedule)
	if err != nil || interval <= 0 {
		// Validated at create/update time; a bad value here means a hand-edited
		// record. Drop the chain rather than spin.
		slog.Error("recurring task has an invalid schedule, not re-enqueuing", "task_id", t.ID, "schedule", t.Schedule)
		return
	}
	// The successor is this task at a new time: clone it, then reset identity
	// (fresh id, no inherited idempotency key) and per-run state.
	next := t
	next.ID = uuid.NewString()
	next.IdempotencyKey = ""
	next.ExecuteAt = fireTime.Add(interval)
	next.Status = scheduler.StatusPending
	next.Attempts = nil
	next.FinishedAt = nil
	if err := r.store.Save(next); err != nil {
		slog.Error("reschedule task", "task_id", t.ID, "error", err)
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
		slog.Error("failure callback", "task_id", t.ID, "error", res.Err)
	}
}
