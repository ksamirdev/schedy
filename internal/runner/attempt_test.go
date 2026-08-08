package runner

import (
	"testing"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

// captureSleeps swaps sleepFunc/timeFunc so next() runs instantly and records
// every requested delay. lastTime spacing is neutralised by advancing the clock
// past each delay before the following attempt.
func captureSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	now := time.Unix(0, 0)
	origSleep, origTime := sleepFunc, timeFunc
	sleepFunc = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
	timeFunc = func() time.Time { return now }
	t.Cleanup(func() { sleepFunc, timeFunc = origSleep, origTime })
	return &slept
}

func TestFixedDelay(t *testing.T) {
	slept := captureSleeps(t)
	a := newAttempt(3, 100, scheduler.RetryFixed)
	for a.next() {
	}
	// First next() sets lastTime with no sleep; the rest wait the fixed interval.
	for i, d := range *slept {
		if d != 100*time.Millisecond {
			t.Fatalf("sleep %d = %v, want 100ms", i, d)
		}
	}
}

func TestExponentialFullJitterBounds(t *testing.T) {
	slept := captureSleeps(t)
	a := newAttempt(20, 100, scheduler.RetryExponential)
	for a.next() {
	}
	// The first delay() (count=0) fires with lastTime zero, so no sleep is
	// recorded for it; recorded sleep i therefore has count=i+1 and is jittered
	// in [0, min(100ms<<(i+1), cap)].
	base := 100 * time.Millisecond
	for i, d := range *slept {
		ceil := base << (i + 1)
		if ceil <= 0 || ceil > maxBackoff {
			ceil = maxBackoff
		}
		if d < 0 || d > ceil {
			t.Fatalf("delay %d = %v, want within [0,%v]", i, d, ceil)
		}
		if d > maxBackoff {
			t.Fatalf("delay %d = %v exceeds cap %v", i, d, maxBackoff)
		}
	}
}

func TestServerHintFloorsDelay(t *testing.T) {
	slept := captureSleeps(t)
	a := newAttempt(2, 100, scheduler.RetryFixed)
	a.next() // first retry: lastTime zero, no sleep recorded

	a.serverHint(3 * time.Second)
	a.next()
	if got := (*slept)[len(*slept)-1]; got != 3*time.Second {
		t.Fatalf("hinted delay = %v, want 3s", got)
	}
	if a.hint != 0 {
		t.Fatalf("hint not consumed after next()")
	}
}

func TestServerHintCappedAtMaxBackoff(t *testing.T) {
	a := newAttempt(1, 100, scheduler.RetryFixed)
	a.serverHint(2 * time.Hour)
	if a.hint != maxBackoff {
		t.Fatalf("hint = %v, want cap %v", a.hint, maxBackoff)
	}
}

func TestServerHintBelowComputedDelayIgnored(t *testing.T) {
	slept := captureSleeps(t)
	a := newAttempt(2, 100, scheduler.RetryFixed)
	a.next()

	a.serverHint(10 * time.Millisecond) // below the 100ms interval: floor, not override
	a.next()
	if got := (*slept)[len(*slept)-1]; got != 100*time.Millisecond {
		t.Fatalf("delay = %v, want 100ms", got)
	}
}
