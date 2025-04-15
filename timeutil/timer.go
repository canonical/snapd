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

package timeutil

import (
	"time"
)

// Timer is an interface which wraps time.Timer so that it may be mocked.
//
// Timer is fully compatible with time.Timer, except that since interfaces
// cannot have instance variables, we must expose the C channel as a method.
// Therefore, when replacing a time.Timer with timeutil.Timer, any accesses of C
// must be replaced with ExpireC().
//
// For more information about time.Timer, see: https://pkg.go.dev/time#Timer
type Timer interface {
	Reset(d time.Duration) bool
	Stop() bool
	// ExpiredC is equivalent to t.C for StdlibTimer and time.Timer.
	ExpiredC() <-chan time.Time
}

type StdlibTimer struct {
	*time.Timer
}

// AfterFunc waits for the duration to elapse and then calls f in its own
// goroutine. It returns a Timer that can be used to cancel the call using its
// Stop method. The returned Timer's C field is not used and will be nil.
//
// See here for more information: https://pkg.go.dev/time#AfterFunc
func AfterFunc(d time.Duration, f func()) StdlibTimer {
	return StdlibTimer{time.AfterFunc(d, f)}
}

// After waits for the duration to elapse and then closes the channel.
//
// See here for more information: https://pkg.go.dev/time#After
func After(d time.Duration) <-chan time.Time {
	return StdlibTimer{time.NewTimer(d)}.ExpiredC()
}

// NewTimer creates a new Timer that will send the current time on its channel
// after at least duration d.
//
// See here for more information: https://pkg.go.dev/time#NewTimer
func NewTimer(d time.Duration) StdlibTimer {
	return StdlibTimer{time.NewTimer(d)}
}

// ExpiredC returns the channel t.C over which the current time will be sent
// when the timer expires, assuming the timer was created via NewTimer.
//
// If the timer was created via AfterFunc, then t.C is nil, so this function
// returns nil.
func (t StdlibTimer) ExpiredC() <-chan time.Time {
	return t.C
}
