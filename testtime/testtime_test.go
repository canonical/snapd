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

package testtime_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testtime"
)

func Test(t *testing.T) { TestingT(t) }

type testtimeSuite struct{}

var _ = Suite(&testtimeSuite{})

var (
	_ = testtime.Timer(&testtime.RealTimer{})
	_ = testtime.Timer(&testtime.TestTimer{})
)

func (s *testtimeSuite) TestTimerInterfaceCompatibility(c *C) {
	var t testtime.Timer

	t = testtime.NewTimer(time.Second)
	active := t.Reset(time.Second)
	c.Check(active, Equals, true)
	active = t.Stop()
	c.Check(active, Equals, true)
	c.Check(t.C(), NotNil)
	t = testtime.AfterFunc(time.Second, func() { return })
	active = t.Reset(time.Second)
	c.Check(active, Equals, true)
	active = t.Stop()
	c.Check(active, Equals, true)
	c.Check(t.C(), IsNil)

	restore := testtime.MockTimers()
	defer restore()

	t = testtime.NewTimer(time.Second)
	active = t.Reset(time.Second)
	c.Check(active, Equals, true)
	active = t.Stop()
	c.Check(active, Equals, true)
	c.Check(t.C(), NotNil)
	t = testtime.AfterFunc(time.Second, func() { return })
	active = t.Reset(time.Second)
	c.Check(active, Equals, true)
	active = t.Stop()
	c.Check(active, Equals, true)
	c.Check(t.C(), IsNil)
}

func (s *testtimeSuite) TestAfterFunc(c *C) {
	restore := testtime.MockTimers()
	defer restore()

	// Create a non-buffered channel on which a message will be sent when the
	// callback is called. Use a non-buffered channel so that we ensure that
	// the callback runs in its own goroutine.
	callbackChan := make(chan string)

	result := testtime.AfterFunc(time.Hour, func() {
		callbackChan <- "called"
	})

	timer := result.(*testtime.TestTimer)

	c.Check(timer.C(), IsNil)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-callbackChan:
		c.Fatal("callback fired early")
	default:
	}

	// Manually advance the timer so that it will fire
	timer.Elapse(time.Hour)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case msg := <-callbackChan:
		c.Assert(msg, Equals, "called")
	case <-time.NewTimer(time.Minute).C:
		// Goroutine may not start immediately, so allow some grace period
		c.Fatal("callback did not complete")
	}

	// Reset timer to check that if it fires again, the callback will be called again
	active := timer.Reset(time.Nanosecond)
	c.Check(active, Equals, false)

	c.Check(timer.C(), IsNil)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-callbackChan:
		c.Fatal("callback fired early")
	default:
	}

	// Manually fire the timer with the current time, though the time doesn't matter here
	err := timer.Fire(time.Now())
	c.Check(err, IsNil)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 2)
	select {
	case msg := <-callbackChan:
		c.Assert(msg, Equals, "called")
	case <-time.NewTimer(time.Minute).C:
		// Goroutine may not start immediately, so allow some grace period
		c.Fatal("callback did not complete")
	}
}

func (s *testtimeSuite) TestNewTimer(c *C) {
	restore := testtime.MockTimers()
	defer restore()

	result := testtime.NewTimer(time.Second)

	timer := result.(*testtime.TestTimer)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.C():
		c.Fatal("timer fired early")
	default:
	}

	// Manually advance the timer so that it will fire
	timer.Elapse(time.Second)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
	default:
		c.Fatal("timer did not fire")
	}

	// Reset timer to check that if it fires again, the callback will be called again
	active := timer.Reset(time.Nanosecond)
	c.Check(active, Equals, false)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
		c.Fatal("timer fired early")
	default:
	}

	// Manually fire the timer with the current time
	currTime := time.Now()
	err := timer.Fire(currTime)
	c.Check(err, IsNil)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 2)
	select {
	case t := <-timer.C():
		c.Assert(t.Equal(currTime), Equals, true)
	default:
		c.Fatal("timer did not fire")
	}
}

func (s *testtimeSuite) TestReset(c *C) {
	restore := testtime.MockTimers()
	defer restore()

	result := testtime.NewTimer(time.Millisecond)

	timer := result.(*testtime.TestTimer)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.C():
		c.Fatal("timer fired early")
	default:
	}

	err := timer.Fire(time.Now())
	c.Check(err, IsNil)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)

	active := timer.Reset(time.Millisecond)
	c.Check(active, Equals, false)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	// Check that receiving from the timer channel blocks after reset, even
	// though the timer previously fired and write time to channel.
	select {
	case <-timer.C():
		c.Fatal("timer fired after reset")
	default:
	}

	// Reset the timer
	active = timer.Reset(3 * time.Second)
	c.Check(active, Equals, true)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
		c.Fatal("timer fired early")
	default:
	}

	// Elapse more than half the time
	timer.Elapse(2 * time.Second)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
		c.Fatal("timer fired early")
	default:
	}

	// Reset the timer
	active = timer.Reset(3 * time.Second)
	c.Check(active, Equals, true)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
		c.Fatal("timer fired after reset")
	default:
	}

	// Elapse more than half the time again
	timer.Elapse(2 * time.Second)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
		c.Fatal("timer fired after time elapsed following reset")
	default:
	}

	// Elapse the remaining time
	timer.Elapse(time.Second)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 2)
	select {
	case <-timer.C():
	default:
		c.Fatal("timer did not fire")
	}

	active = timer.Reset(time.Second)
	c.Check(active, Equals, false)
	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 2)
}

func (s *testtimeSuite) TestStop(c *C) {
	restore := testtime.MockTimers()
	defer restore()

	result := testtime.NewTimer(time.Millisecond)

	timer := result.(*testtime.TestTimer)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.C():
		c.Fatal("timer fired early")
	default:
	}

	active := timer.Stop()
	c.Check(active, Equals, true)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.C():
		c.Fatal("timer fired after Stop")
	default:
	}

	// Elapse time so the timer would have fired if it were not stopped
	timer.Elapse(time.Millisecond)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.C():
		c.Fatal("received from timer chan after Stop and Elapse")
	default:
	}

	// Reset the timer, and check that the timer was not previously active
	active = timer.Reset(time.Second)
	c.Check(active, Equals, false)
	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)

	// Elapse time so that the timer fires
	timer.Elapse(1500 * time.Millisecond)

	c.Check(active, Equals, false)

	// Stop the timer after it has fired
	active = timer.Stop()
	c.Check(active, Equals, false)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.C():
		c.Fatal("received from timer chan after Stop called after firing")
	default:
	}
}

func (s *testtimeSuite) TestFireErrors(c *C) {
	restore := testtime.MockTimers()
	defer restore()

	result := testtime.AfterFunc(time.Hour, func() { c.Fatal("should not have been called") })

	timer := result.(*testtime.TestTimer)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)

	timer.Stop()

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)
	currTime := time.Now()
	err := timer.Fire(currTime)
	c.Check(err, ErrorMatches, "cannot fire timer which is not active")

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)

	// Re-declare timer with callback which doesn't cause error
	result = testtime.AfterFunc(time.Minute, func() {})
	timer = result.(*testtime.TestTimer)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)

	timer.Elapse(time.Minute)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)

	err = timer.Fire(currTime)
	c.Check(err, ErrorMatches, "cannot fire timer which is not active")

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)

	active := timer.Stop()
	c.Check(active, Equals, false)

	err = timer.Fire(currTime)
	c.Check(err, ErrorMatches, "cannot fire timer which is not active")
	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)
}
