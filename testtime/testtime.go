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

// Package testtime provides a mocked version of time.Timer for use in tests.
package testtime

import (
	"sync"
	"time"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/timeutil"
)

// TestTimer is a mocked version of time.Timer for which the passage of time or
// the direct expiration of the timer is controlled manually.
//
// TestTimer implements timeutil.Timer.
//
// TestTimer also provides methods to introspect whether the timer is active or
// how many times it has fired.
type TestTimer struct {
	mu        sync.Mutex
	duration  time.Duration
	elapsed   time.Duration
	active    bool
	fireCount int
	callback  func()
	c         chan time.Time
}

var _ timeutil.Timer = (*TestTimer)(nil)

// AfterFunc waits for the timer to fire and then calls f in its own goroutine.
// It returns a Timer that can be used to cancel the call using its Stop method.
// The returned Timer's C field is not used and will be nil.
//
// AfterFunc returns a TestTimer which simulates the behavior of a timer which
// was created via time.AfterFunc.
//
// See here for more details: https://pkg.go.dev/time#AfterFunc
func AfterFunc(d time.Duration, f func()) *TestTimer {
	osutil.MustBeTestBinary("testtime timers cannot be used outside of tests")
	timer := &TestTimer{
		duration: d,
		active:   true,
		callback: f,
	}
	// If duration is 0 or negative, ensure timer fires
	defer timer.maybeFire()
	return timer
}

// NewTimer creates a new Timer that will send the current time on its channel
// after the timer fires.
//
// NewTimer returns a TestTimer which simulates the behavior of a timer which
// was created via time.NewTimer.
//
// See here for more details: https://pkg.go.dev/time#NewTimer
func NewTimer(d time.Duration) *TestTimer {
	osutil.MustBeTestBinary("testtime timers cannot be used outside of tests")
	c := make(chan time.Time, 1)
	timer := &TestTimer{
		duration: d,
		active:   true,
		c:        c,
	}
	// If duration is 0 or negative, ensure timer fires
	defer timer.maybeFire()
	return timer
}

// ExpiredC returns the underlying C channel of the timer.
func (t *TestTimer) ExpiredC() <-chan time.Time {
	return t.c
}

// Reset changes the timer to expire after duration d. It returns true if the
// timer had been active, false if the timer had expired or been stopped.
//
// As the test timer does not actually count down, Reset sets the timer's
// elapsed time to 0 and set its duration to the given duration. The elapsed
// time must be advanced manually using Elapse.
//
// This simulates the behavior of Timer.Reset() from the time package.
// See here fore more details: https://pkg.go.dev/time#Timer.Reset
func (t *TestTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	active := t.active
	t.active = true
	t.duration = d
	t.elapsed = 0
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
	// If duration is 0 or negative, ensure timer fires
	defer t.maybeFire()
	return active
}

// Stop prevents the timer from firing. It returns true if the call stops the
// timer, false if the timer has already expired or been stopped.
//
// This simulates the behavior of Timer.Stop() from the time package.
// See here for more details: https://pkg.go.dev/time#Timer.Stop
func (t *TestTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
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
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

// FireCount returns the number of times the timer has fired.
func (t *TestTimer) FireCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.fireCount
}

// Elapse simulates time advancing by the given duration, which potentially
// causes the timer to fire.
//
// The timer will fire if the total elapsed time since the timer was created
// or reset is greater than the timer's duration and the timer has not yet
// fired.
func (t *TestTimer) Elapse(duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.elapsed += duration
	t.maybeFire()
}

// maybeFire fires the timer if the elapsed time is greater than the timer's
// duration. The caller must hold the timer lock.
func (t *TestTimer) maybeFire() {
	if t.elapsed >= t.duration {
		t.doFire(time.Now())
	}
}

// Fire causes the timer to fire. If the timer was created via NewTimer, then
// sends the given current time over the C channel.
//
// To avoid accidental misuse, panics if the timer is not active (if it has
// already fired or been stopped).
func (t *TestTimer) Fire(currTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		panic("cannot fire timer which is not active")
	}
	t.doFire(currTime)
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
	} else if t.c != nil {
		t.c <- currTime
	}
}
