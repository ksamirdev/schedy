package runner

import "time"

var (
	timeFunc  = time.Now
	sleepFunc = time.Sleep
)

type attemptStrategy struct {
	interval time.Duration // interval between each try in milliseconds
	retries  int           // number of retries for attempt
}

type attempt struct {
	strategy attemptStrategy
	lastTime time.Time
	count    int // number of attempted retries
}

func newAttempt(rcount, interval int) *attempt {
	return &attempt{
		strategy: attemptStrategy{
			retries:  rcount,
			interval: time.Duration(interval) * time.Millisecond,
		},
	}
}

func (a *attempt) next() bool {
	if a.shouldRetry() {
		if !a.lastTime.IsZero() {
			timeSince := timeFunc().Sub(a.lastTime)
			if timeSince < a.strategy.interval {
				sleepFunc(a.strategy.interval - timeSince)
			}
		}
		a.lastTime = timeFunc()
		a.count++
		return true
	}
	return false
}

func (a *attempt) shouldRetry() bool {
	return a.count < a.strategy.retries
}
