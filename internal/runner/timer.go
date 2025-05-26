package runner

import "time"

type Timer interface {
	C() <-chan time.Time
	Stop()
}

type realTimer struct {
	t *time.Timer
}

func NewTimer(d time.Duration) Ticker {
	return &realTimer{t: time.NewTimer(d)}
}

func (rt *realTimer) C() <-chan time.Time {
	return rt.t.C
}

func (rt *realTimer) Stop() {
	rt.t.Stop()
}
