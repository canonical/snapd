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

// Package timer provides a wrapper around time.Timer and its associated
// functions so that timers can be mocked in tests.
package timeutil

import (
	"fmt"
	"sync"
	"time"
)

// Timer is an interface which wraps time.Timer so that it may be mocked.
type Timer interface {
	Reset(d time.Duration) bool
	Stop() bool
}

// FakeAfterFunc creates a new fake timer which will call the given callback in
// its own goroutine when the timer fires. The returned FakeTimer's C field is
// not used and will be nil.
//
// This simulates the behavior of AfterFunc() from the time package.
// See here for more details: https://pkg.go.dev/time#AfterFunc
func FakeAfterFunc(d time.Duration, f func()) *FakeTimer {
	currTime := time.Now()
	return &FakeTimer{
		currTime:   currTime,
		expiration: currTime.Add(d),
		active:     true,
		callback:   f,
	}
}

// FakeNewTimer creates a new fake timer which, when it fires, will send the
// time that the timer fires over the C channel.
//
// This simulates the behavior of NewTimer() from the time package.
// See here for more details: https://pkg.go.dev/time#NewTimer
func FakeNewTimer(d time.Duration) *FakeTimer {
	currTime := time.Now()
	c := make(chan time.Time, 1)
	return &FakeTimer{
		currTime:   currTime,
		expiration: currTime.Add(d),
		active:     true,
		c:          c,
		C:          c,
	}
}

// FakeTimer is a mocked version of time.Timer for which the passage of time or
// the direct expiration of the timer is controlled manually.
type FakeTimer struct {
	lock       sync.Mutex
	currTime   time.Time
	expiration time.Time
	active     bool
	fireCount  int
	callback   func()
	c          chan<- time.Time // internally, c is write-only
	C          <-chan time.Time // export c as read-only
}

// Reset changes the timer to expire after duration d. It returns true if the
// timer had been active, false if the timer had expired or been stopped.
//
// As the fake timer does not actually count down, Reset sets the timer's
// expiration to be the given duration added to the timer's internal current
// time. This internal time must be advanced manually using Elapse.
//
// This simulates the behavior of Timer.Reset() from the time package.
// See here fore more details: https://pkg.go.dev/time#Timer.Reset
func (t *FakeTimer) Reset(d time.Duration) bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	active := t.active
	t.active = true
	t.expiration = t.currTime.Add(d)
	if t.C != nil {
		// Drain the channel, guaranteeing that a receive after Reset will
		// block until the timer fires again, and not receive a time value
		// from the timer firing before the reset occurred.
		// This complies with the new behavior of Reset as of Go 1.23.
		// See: https://pkg.go.dev/time#Timer.Reset
		select {
		case <-t.C:
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
func (t *FakeTimer) Stop() bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	wasActive := t.active
	t.active = false
	if t.C != nil {
		// Drain the channel, guaranteeing that a receive after Stop will block
		// and not receive a time value from the timer firing before the stop
		// occurred. This complies with the new behavior of Stop as of Go 1.23.
		// See: https://pkg.go.dev/time#Timer.Stop
		select {
		case <-t.C:
		default:
		}
	}
	return wasActive
}

// isActive returns true if the timer is active, false if the timer has expired
// or been stopped.
func (t *FakeTimer) Active() bool {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.active
}

// FireCount returns the number of times the timer has fired.
func (t *FakeTimer) FireCount() int {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.fireCount
}

// Elapse simulates the current time advancing by the given duration, which
// potentially causes the timer to fire.
//
// The timer will fire if the time after the elapsed duration is after the
// expiration time and the timer has not yet fired.
func (t *FakeTimer) Elapse(duration time.Duration) {
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
func (t *FakeTimer) Fire(currTime time.Time) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	if !t.active {
		return fmt.Errorf("cannot fire timer which is not active")
	}
	t.doFire(currTime)
	return nil
}

// doFire carries out the timer firing. The caller must hold the timer lock.
func (t *FakeTimer) doFire(currTime time.Time) {
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
