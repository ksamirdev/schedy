package runner

import "time"

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct {
	t *time.Ticker
}

func NewTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

func (rt *realTicker) C() <-chan time.Time {
	return rt.t.C
}

func (rt *realTicker) Stop() {
	rt.t.Stop()
}
