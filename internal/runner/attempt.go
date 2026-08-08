package runner

import (
	"math/rand/v2"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

var (
	timeFunc  = time.Now
	sleepFunc = time.Sleep
)

// maxBackoff caps the exponential delay so a large retry count can't wait for
// hours. It is not user-facing (see issue #28's lean cut).
// ponytail: fixed cap, expose as a knob only if someone needs a longer horizon.
const maxBackoff = 5 * time.Minute

type attemptStrategy struct {
	interval time.Duration       // base interval between tries
	retries  int                 // number of retries for attempt
	mode     scheduler.RetryMode // fixed or exponential
}

type attempt struct {
	strategy attemptStrategy
	lastTime time.Time
	count    int // number of attempted retries
	// hint is the server-requested wait (Retry-After) for the next retry only.
	// It acts as a floor under the computed delay: backing off harder than the
	// strategy would is respectful, backing off less would hammer a server
	// that just said "not yet". Consumed (reset) by next().
	hint time.Duration
}

// serverHint records the wait the server asked for before the next retry.
// Capped at maxBackoff so a hostile or misconfigured endpoint can't park a
// worker slot for hours.
func (a *attempt) serverHint(d time.Duration) {
	if d > maxBackoff {
		d = maxBackoff
	}
	a.hint = d
}

func newAttempt(rcount, interval int, mode scheduler.RetryMode) *attempt {
	return &attempt{
		strategy: attemptStrategy{
			retries:  rcount,
			interval: time.Duration(interval) * time.Millisecond,
			mode:     mode,
		},
	}
}

func (a *attempt) next() bool {
	if a.shouldRetry() {
		d := a.delay()
		if !a.lastTime.IsZero() {
			timeSince := timeFunc().Sub(a.lastTime)
			if timeSince < d {
				sleepFunc(d - timeSince)
			}
		}
		a.lastTime = timeFunc()
		a.count++
		a.hint = 0
		return true
	}
	return false
}

// delay returns how long to wait before the next retry. Fixed mode returns the
// base interval; exponential mode returns full jitter over
// min(base * 2^count, cap) - a uniform point in [0, that], which spreads
// retries from many clients instead of synchronising them.
func (a *attempt) delay() time.Duration {
	d := a.strategy.interval
	if a.strategy.mode == scheduler.RetryExponential {
		backoff := a.strategy.interval << a.count
		if backoff <= 0 || backoff > maxBackoff { // <=0 catches shift overflow
			backoff = maxBackoff
		}
		d = time.Duration(rand.Int64N(int64(backoff) + 1))
	}
	// A Retry-After from the server floors the delay: never come back sooner
	// than asked, even when jitter rolled a small wait.
	if a.hint > d {
		d = a.hint
	}
	return d
}

func (a *attempt) shouldRetry() bool {
	return a.count < a.strategy.retries
}
