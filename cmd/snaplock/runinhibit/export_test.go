package runinhibit

import "time"

type fakeTicker struct {
	wait func() <-chan time.Time
}

func (t *fakeTicker) Wait() <-chan time.Time {
	if t.wait != nil {
		return t.wait()
	}
	return nil
}

func MockNewTicker(wait func() <-chan time.Time) (restore func()) {
	old := newTicker
	newTicker = func(interval time.Duration) ticker {
		return &fakeTicker{wait}
	}
	return func() {
		newTicker = old
	}
}
