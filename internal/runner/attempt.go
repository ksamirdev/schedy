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
		return true
	}
	return false
}

// delay returns how long to wait before the next retry. Fixed mode returns the
// base interval; exponential mode returns full jitter over
// min(base * 2^count, cap) - a uniform point in [0, that], which spreads
// retries from many clients instead of synchronising them.
func (a *attempt) delay() time.Duration {
	if a.strategy.mode != scheduler.RetryExponential {
		return a.strategy.interval
	}
	backoff := a.strategy.interval << a.count
	if backoff <= 0 || backoff > maxBackoff { // <=0 catches shift overflow
		backoff = maxBackoff
	}
	return time.Duration(rand.Int64N(int64(backoff) + 1))
}

func (a *attempt) shouldRetry() bool {
	return a.count < a.strategy.retries
}
