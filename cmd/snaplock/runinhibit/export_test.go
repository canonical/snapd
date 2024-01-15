package runinhibit

import "time"

type fakeTicker struct {
	wait func()
}

func (t *fakeTicker) Wait() {
	if t.wait != nil {
		t.wait()
	}
}

func MockNewTicker(wait func()) (restore func()) {
	old := newTicker
	newTicker = func(interval time.Duration) ticker {
		return &fakeTicker{wait}
	}
	return func() {
		newTicker = old
	}
}
