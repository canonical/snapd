// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Package testtime provides a wrapper around time.Timer and its associated
// functions so that timers can be mocked in tests.
package testtime

import (
	"fmt"
	"sync"
	"time"

	"github.com/snapcore/snapd/testutil"
)

// Timer is an interface which wraps time.Timer so that it may be mocked.
//
// Timer is fully compatible with time.Timer, except that since interfaces
// cannot have instance variables, we must expose the C channel as a method.
// Therefore, when replacing a time.Timer with testtime.Timer, any direct
// accesses of C must be replaced with C().
//
// For more information about time.Timer, see: https://pkg.go.dev/time#Timer
type Timer interface {
	C() <-chan time.Time
	Reset(d time.Duration) bool
	Stop() bool
}

// useMockedTimers indicates whether AfterFunc and NewTimer should return
// TestTimer instances instead of RealTimer instances.
var useMockedTimers = false

// MockTimers makes AfterFunc and NewTimer return TestTimers instead of
// RealTimers. This can be reversed by calling the returned restore function.
func MockTimers() (restore func()) {
	return testutil.Mock(&useMockedTimers, true)
}

// AfterFunc waits for the timer to fire and then calls f in its own goroutine.
// It returns a Timer that can be used to cancel the call using its Stop method.
// The returned Timer's C field is not used and will be nil.
//
// By default, AfterFunc directly calls time.AfterFunc and returns a wrapper
// around the result. If MockTimers has been called, AfterFunc returns a
// TestTimer which simulates the behavior of a timer which was created via
// time.AfterFunc. See here for more details: https://pkg.go.dev/time#AfterFunc
func AfterFunc(d time.Duration, f func()) Timer {
	if useMockedTimers {
		return afterFuncTest(d, f)
	}
	return afterFuncReal(d, f)
}

// NewTimer creates a new Timer that will send the current time on its channel
// after the timer fires.
//
// By default, NewTimer directly calls time.Newtimer and returns a wrapper
// around the result. If MockTimers has been called, NewTimer returns a
// TestTimer which simulates the behavior of a timer which was created via
// time.NewTimer. See here for more details: https://pkg.go.dev/time#NewTimer
func NewTimer(d time.Duration) Timer {
	if useMockedTimers {
		return newTimerTest(d)
	}
	return newTimerReal(d)
}

// RealTimer is a wrapper around time.Timer so that the C channel can be used
// by instances of the interface, without needing to know the concrete type.
type RealTimer struct {
	timer *time.Timer
}

// C returns the underlying C channel of the timer.
func (t *RealTimer) C() <-chan time.Time {
	return t.timer.C
}

// Reset changes the timer to expire after duration d. It returns true if the
// timer had been active, false if the timer had expired or been stopped.
//
// Reset directly invokes Timer.Reset from the time package.
func (t *RealTimer) Reset(d time.Duration) bool {
	return t.timer.Reset(d)
}

// Stop prevents the Timer from firing. It returns true if the call stops the
// timer, false if the timer has already expired or been stopped.
func (t *RealTimer) Stop() bool {
	return t.timer.Stop()
}

var _ = Timer(&RealTimer{})

// afterFuncReal calls time.AfterFunc and returns the result wrapped in a
// RealTimer.
func afterFuncReal(d time.Duration, f func()) *RealTimer {
	return &RealTimer{
		timer: time.AfterFunc(d, f),
	}
}

// newTimerReal calls time.NewTimer and returns the result wrapped in a
// RealTimer.
func newTimerReal(d time.Duration) *RealTimer {
	return &RealTimer{
		timer: time.NewTimer(d),
	}
}

// TestTimer is a mocked version of time.Timer for which the passage of time or
// the direct expiration of the timer is controlled manually.
//
// TestTimer also provides methods to introspect whether the timer is active or
// how many times it has fired.
type TestTimer struct {
	lock       sync.Mutex
	currTime   time.Time
	expiration time.Time
	active     bool
	fireCount  int
	callback   func()
	c          chan time.Time
}

var _ = Timer(&TestTimer{})

// afterFuncTest creates a new timer which will call the given callback in its
// own goroutine when the timer fires. The returned TestTimer's C field is not
// used and will be nil.
//
// This simulates the behavior of AfterFunc() from the time package.
// See here for more details: https://pkg.go.dev/time#AfterFunc
func afterFuncTest(d time.Duration, f func()) *TestTimer {
	currTime := time.Now()
	return &TestTimer{
		currTime:   currTime,
		expiration: currTime.Add(d),
		active:     true,
		callback:   f,
	}
}

// newTimerTest creates a new timer which, when it fires, will send the time
// that the timer fired over the C channel.
//
// This simulates the behavior of NewTimer() from the time package.
// See here for more details: https://pkg.go.dev/time#NewTimer
func newTimerTest(d time.Duration) *TestTimer {
	currTime := time.Now()
	c := make(chan time.Time, 1)
	return &TestTimer{
		currTime:   currTime,
		expiration: currTime.Add(d),
		active:     true,
		c:          c,
	}
}

// C returns the underlying C channel of the timer.
func (t *TestTimer) C() <-chan time.Time {
	return t.c
}

// Reset changes the timer to expire after duration d. It returns true if the
// timer had been active, false if the timer had expired or been stopped.
//
// As the test timer does not actually count down, Reset sets the timer's
// expiration to be the given duration added to the timer's internal current
// time. This internal time must be advanced manually using Elapse.
//
// This simulates the behavior of Timer.Reset() from the time package.
// See here fore more details: https://pkg.go.dev/time#Timer.Reset
func (t *TestTimer) Reset(d time.Duration) bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	active := t.active
	t.active = true
	t.expiration = t.currTime.Add(d)
	if t.c != nil {
		// Drain the channel, guaranteeing that a receive after Reset will
		// block until the timer fires again, and not receive a time value
		// from the timer firing before the reset occurred.
		// This complies with the new behavior of Reset as of Go 1.23.
		// See: https://pkg.go.dev/time#Timer.Reset
		select {
		case <-t.c:
		default:
		}
	}
	return active
}

// Stop prevents the timer from firing. It returns true if the call stops the
// timer, false if the timer has already expired or been stopped.
//
// This simulates the behavior of Timer.Stop() from the time package.
// See here for more details: https://pkg.go.dev/time#Timer.Stop
func (t *TestTimer) Stop() bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	wasActive := t.active
	t.active = false
	if t.c != nil {
		// Drain the channel, guaranteeing that a receive after Stop will block
		// and not receive a time value from the timer firing before the stop
		// occurred. This complies with the new behavior of Stop as of Go 1.23.
		// See: https://pkg.go.dev/time#Timer.Stop
		select {
		case <-t.c:
		default:
		}
	}
	return wasActive
}

// Active returns true if the timer is active, false if the timer has expired
// or been stopped.
func (t *TestTimer) Active() bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.active
}

// FireCount returns the number of times the timer has fired.
func (t *TestTimer) FireCount() int {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.fireCount
}

// Elapse simulates the current time advancing by the given duration, which
// potentially causes the timer to fire.
//
// The timer will fire if the time after the elapsed duration is after the
// expiration time and the timer has not yet fired.
func (t *TestTimer) Elapse(duration time.Duration) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.currTime = t.currTime.Add(duration)
	if !t.currTime.Before(t.expiration) {
		t.doFire(t.expiration)
	}
}

// Fire causes the timer to fire. If the timer was created via NewTimer, then
// sends the given current time over the C channel.
//
// To avoid accidental misuse, throw an error if the timer has already fired or
// been stopped.
func (t *TestTimer) Fire(currTime time.Time) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if !t.active {
		return fmt.Errorf("cannot fire timer which is not active")
	}
	t.doFire(currTime)
	return nil
}

// doFire carries out the timer firing. The caller must hold the timer lock.
func (t *TestTimer) doFire(currTime time.Time) {
	if !t.active {
		return
	}
	t.active = false
	t.fireCount++
	// Either t.callback or t.C should be non-nil, and the other should be nil.
	if t.callback != nil {
		go t.callback()
	}
	if t.c != nil {
		t.c <- currTime
	}
}
