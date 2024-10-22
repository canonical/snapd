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

package timeutil_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/timeutil"
)

type timerSuite struct{}

var _ = Suite(&timerSuite{})

func (s *timerSuite) TestFakeAfterFunc(c *C) {
	// Create a non-buffered channel on which a message will be sent when the
	// callback is called. Use a non-buffered channel so that we ensure that
	// the callback runs in its own goroutine.
	callbackChan := make(chan string)

	timer := timeutil.FakeAfterFunc(time.Hour, func() {
		callbackChan <- "called"
	})

	c.Check(timer.C, IsNil)

	select {
	case <-callbackChan:
		c.Fatal("callback fired early")
	default:
	}

	// Manually advance the timer so that it will fire
	timer.Elapse(time.Hour)

	select {
	case msg := <-callbackChan:
		c.Assert(msg, Equals, "called")
	case <-time.NewTimer(time.Minute).C:
		// Goroutine may not start immediately, so allow some grace period
		c.Fatal("callback did not complete")
	}

	// Reset timer to check that if it fires again, the callback will be called again
	timer.Reset(time.Nanosecond)

	c.Check(timer.C, IsNil)

	select {
	case <-callbackChan:
		c.Fatal("callback fired early")
	default:
	}

	// Manually fire the timer with the current time, though the time doesn't matter here
	err := timer.Fire(time.Now())
	c.Check(err, IsNil)

	select {
	case msg := <-callbackChan:
		c.Assert(msg, Equals, "called")
	case <-time.NewTimer(time.Minute).C:
		// Goroutine may not start immediately, so allow some grace period
		c.Fatal("callback did not complete")
	}
}

func (s *timerSuite) TestFakeNewTimer(c *C) {
	timer := timeutil.FakeNewTimer(time.Second)

	select {
	case <-timer.C:
		c.Fatal("timer fired early")
	default:
	}

	// Manually advance the timer so that it will fire
	timer.Elapse(time.Second)

	select {
	case <-timer.C:
	default:
		c.Fatal("timer did not fire")
	}

	// Reset timer to check that if it fires again, the callback will be called again
	timer.Reset(time.Nanosecond)

	select {
	case <-timer.C:
		c.Fatal("timer fired early")
	default:
	}

	// Manually fire the timer with the current time
	currTime := time.Now()
	err := timer.Fire(currTime)
	c.Check(err, IsNil)

	select {
	case t := <-timer.C:
		c.Assert(t.Equal(currTime), Equals, true)
	default:
		c.Fatal("timer did not fire")
	}
}

func (s *timerSuite) TestTimerInterfaceCompatibility(c *C) {
	var t timeutil.Timer

	t = time.NewTimer(time.Second)
	t.Reset(time.Second)
	t.Stop()
	t = time.AfterFunc(time.Second, func() { return })
	t.Reset(time.Second)
	t.Stop()
	t = timeutil.FakeNewTimer(time.Second)
	t.Reset(time.Second)
	t.Stop()
	t = timeutil.FakeAfterFunc(time.Second, func() { return })
	t.Reset(time.Second)
	t.Stop()
}

func (s *timerSuite) TestFakeTimerReset(c *C) {
	timer := timeutil.FakeNewTimer(time.Millisecond)

	select {
	case <-timer.C:
		c.Fatal("timer fired early")
	default:
	}

	err := timer.Fire(time.Now())
	c.Check(err, IsNil)

	notFired := timer.Reset(time.Millisecond)
	c.Check(notFired, Equals, false)

	// Check that receiving from the timer channel blocks after reset, even
	// though the timer previously fired
	select {
	case <-timer.C:
		c.Fatal("timer fired after reset")
	default:
	}

	// Reset the timer
	notFired = timer.Reset(3 * time.Second)
	c.Check(notFired, Equals, true)

	select {
	case <-timer.C:
		c.Fatal("timer fired early")
	default:
	}

	// Elapse more than half the time
	timer.Elapse(2 * time.Second)

	select {
	case <-timer.C:
		c.Fatal("timer fired early")
	default:
	}

	// Reset the timer
	notFired = timer.Reset(3 * time.Second)
	c.Check(notFired, Equals, true)

	select {
	case <-timer.C:
		c.Fatal("timer fired after reset")
	default:
	}

	// Elapse more than half the time again
	timer.Elapse(2 * time.Second)

	select {
	case <-timer.C:
		c.Fatal("timer fired after time elapsed following reset")
	default:
	}

	// Elapse the remaining time
	timer.Elapse(time.Second)

	select {
	case <-timer.C:
	default:
		c.Fatal("timer did not fire")
	}

	notFired = timer.Reset(time.Second)
	c.Check(notFired, Equals, false)
}

func (s *timerSuite) TestFakeTimerStop(c *C) {
	timer := timeutil.FakeNewTimer(time.Millisecond)

	select {
	case <-timer.C:
		c.Fatal("timer fired early")
	default:
	}

	notFired := timer.Stop()
	c.Check(notFired, Equals, true)

	select {
	case <-timer.C:
		c.Fatal("timer fired after Stop")
	default:
	}

	// Elapse time so the timer would have fired if it were not stopped
	timer.Elapse(time.Millisecond)

	select {
	case <-timer.C:
		c.Fatal("received from timer chan after Stop and Elapse")
	default:
	}

	// Reset the timer, and check that the timer was not previously fired
	notFired = timer.Reset(time.Second)
	c.Check(notFired, Equals, true)

	// Elapse time so that the timer fires
	timer.Elapse(1500 * time.Millisecond)

	// Stop the timer after it has fired
	notFired = timer.Stop()
	c.Check(notFired, Equals, false)

	select {
	case <-timer.C:
		c.Fatal("received from timer chan after Stop called after firing")
	default:
	}
}

func (s *timerSuite) TestFakeTimerFireErrors(c *C) {
	timer := timeutil.FakeAfterFunc(time.Hour, func() { c.Fatal("should not have been called") })

	timer.Stop()

	currTime := time.Now()
	err := timer.Fire(currTime)
	c.Check(err, ErrorMatches, "cannot fire timer which has already been stopped")

	notFired := timer.Reset(time.Minute)
	c.Check(notFired, Equals, true)

	// Re-declare timer with callback which doesn't cause error
	timer = timeutil.FakeAfterFunc(time.Minute, func() {})

	timer.Elapse(time.Minute)
	err = timer.Fire(currTime)
	c.Check(err, ErrorMatches, "cannot fire timer which has already fired")

	notFired = timer.Stop()
	c.Check(notFired, Equals, false)

	// Check that the error from calling Fire on a stopped timer preempts the
	// error for calling Fire on a timer which has already fired.
	err = timer.Fire(currTime)
	c.Check(err, ErrorMatches, "cannot fire timer which has already been stopped")
}
