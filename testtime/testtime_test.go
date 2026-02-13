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
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/testtime"
)

func Test(t *testing.T) { TestingT(t) }

type testtimeSuite struct{}

var _ = Suite(&testtimeSuite{})

func (s *testtimeSuite) TestTimerInterfaceCompatibility(c *C) {
	t := testtime.NewTimer(time.Second)
	active := t.Reset(time.Second)
	c.Check(active, Equals, true)
	active = t.Stop()
	c.Check(active, Equals, true)
	c.Check(t.ExpiredC(), NotNil)
	t = testtime.AfterFunc(time.Second, func() { return })
	active = t.Reset(time.Second)
	c.Check(active, Equals, true)
	active = t.Stop()
	c.Check(active, Equals, true)
	c.Check(t.ExpiredC(), IsNil)
}

func (s *testtimeSuite) TestAfterFunc(c *C) {
	// Create a non-buffered channel on which a message will be sent when the
	// callback is called. Use a non-buffered channel so that we ensure that
	// the callback runs in its own goroutine.
	callbackChan := make(chan string)

	timer := testtime.AfterFunc(time.Hour, func() {
		callbackChan <- "called"
	})

	c.Check(timer.ExpiredC(), IsNil)

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
	case <-time.After(time.Minute):
		// Goroutine may not start immediately, so allow some grace period
		c.Fatal("callback did not complete")
	}

	// Reset timer to check that if it fires again, the callback will be called again
	active := timer.Reset(time.Nanosecond)
	c.Check(active, Equals, false)

	c.Check(timer.ExpiredC(), IsNil)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-callbackChan:
		c.Fatal("callback fired early")
	default:
	}

	// Manually fire the timer with the current time, though the time doesn't matter here
	timer.Fire(time.Now())

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 2)
	select {
	case msg := <-callbackChan:
		c.Assert(msg, Equals, "called")
	case <-time.After(time.Minute):
		// Goroutine may not start immediately, so allow some grace period
		c.Fatal("callback did not complete")
	}

	// Firing inactive timer panics
	c.Check(func() { timer.Fire(time.Now()) }, PanicMatches, "cannot fire timer which is not active")
}

func (s *testtimeSuite) TestNewTimer(c *C) {
	timer := testtime.NewTimer(time.Second)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired early")
	default:
	}

	// Manually advance the timer so that it will fire
	timer.Elapse(time.Second)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.ExpiredC():
	default:
		c.Fatal("timer did not fire")
	}

	// Reset timer to check that if it fires again, the callback will be called again
	active := timer.Reset(time.Nanosecond)
	c.Check(active, Equals, false)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired early")
	default:
	}

	// Manually fire the timer with the current time
	currTime := time.Now()
	timer.Fire(currTime)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 2)
	select {
	case t := <-timer.ExpiredC():
		c.Assert(t.Equal(currTime), Equals, true)
	default:
		c.Fatal("timer did not fire")
	}

	// Firing inactive timer panics
	c.Check(func() { timer.Fire(currTime) }, PanicMatches, "cannot fire timer which is not active")
}

func (s *testtimeSuite) TestReset(c *C) {
	timer := testtime.NewTimer(time.Millisecond)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired early")
	default:
	}

	timer.Fire(time.Now())

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)

	active := timer.Reset(time.Millisecond)
	c.Check(active, Equals, false)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	// Check that receiving from the timer channel blocks after reset, even
	// though the timer previously fired and write time to channel.
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired after reset")
	default:
	}

	// Reset the timer
	active = timer.Reset(3 * time.Second)
	c.Check(active, Equals, true)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired early")
	default:
	}

	// Elapse more than half the time
	timer.Elapse(2 * time.Second)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired early")
	default:
	}

	// Reset the timer
	active = timer.Reset(3 * time.Second)
	c.Check(active, Equals, true)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired after reset")
	default:
	}

	// Elapse more than half the time again
	timer.Elapse(2 * time.Second)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 1)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired after time elapsed following reset")
	default:
	}

	// Elapse the remaining time
	timer.Elapse(time.Second)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 2)
	select {
	case <-timer.ExpiredC():
	default:
		c.Fatal("timer did not fire")
	}

	active = timer.Reset(time.Second)
	c.Check(active, Equals, false)
	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 2)
}

func (s *testtimeSuite) TestStop(c *C) {
	timer := testtime.NewTimer(time.Millisecond)

	c.Check(timer.Active(), Equals, true)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired early")
	default:
	}

	active := timer.Stop()
	c.Check(active, Equals, true)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.ExpiredC():
		c.Fatal("timer fired after Stop")
	default:
	}

	// Elapse time so the timer would have fired if it were not stopped
	timer.Elapse(time.Millisecond)

	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)
	select {
	case <-timer.ExpiredC():
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
	case <-timer.ExpiredC():
		c.Fatal("received from timer chan after Stop called after firing")
	default:
	}
}

// Tests from the Go standard library which relate to timers

// Adapted from src/time/time_test.go as of go1.23.3.
//
// Issue 25686: hard crash on concurrent timer access.
// Issue 37400: panic with "racy use of timers"
// This test deliberately invokes a race condition.
// We are testing that we don't crash with "fatal error: panic holding locks",
// and that we also don't panic.
func (s *testtimeSuite) TestStdlibConcurrentTimerReset(c *C) {
	const goroutines = 8
	const tries = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	timer := testtime.NewTimer(time.Hour)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < tries; j++ {
				timer.Reset(time.Hour + time.Duration(i*j))
			}
		}(i)
	}
	wg.Wait()
}

// Adapted from src/time/time_test.go as of go1.23.3.
//
// Issue 37400: panic with "racy use of timers".
func (s *testtimeSuite) TestStdlibConcurrentTimerResetStop(c *C) {
	const goroutines = 8
	const tries = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	timer := testtime.NewTimer(time.Hour)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < tries; j++ {
				timer.Reset(time.Hour + time.Duration(i*j))
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			timer.Stop()
		}(i)
	}
	wg.Wait()
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// newTimerFunc simulates NewTimer using AfterFunc,
// but this version will not hit the special cases for channels
// that are used when calling NewTimer.
// This makes it easy to test both paths.
func newTimerFunc(d time.Duration) *testtime.TestTimer {
	c := make(chan time.Time, 1)
	t := testtime.AfterFunc(d, func() { c <- time.Now() })
	t.SetCChan(c)
	return t
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func (s *testtimeSuite) TestStdlibAfterStopNewTimer(c *C) {
	testAfterStop(c, testtime.NewTimer)
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func (s *testtimeSuite) TestStdlibAfterStopAfterFunc(c *C) {
	testAfterStop(c, newTimerFunc)
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func testAfterStop(c *C, newTimer func(time.Duration) *testtime.TestTimer) {
	// We want to test that we stop a timer before it runs.
	// We also want to test that it didn't run after a longer timer.
	// Since we don't want the test to run for too long, we don't
	// want to use lengthy times. That makes the test inherently flaky.
	// So only report an error if it fails five times in a row.

	var errs []string
	logErrs := func() {
		for _, e := range errs {
			c.Log(e)
		}
	}

	for i := 0; i < 5; i++ {
		tInitial := testtime.AfterFunc(100*time.Millisecond, func() {})
		t0 := newTimer(50 * time.Millisecond)
		c1 := make(chan bool, 1)
		t1 := testtime.AfterFunc(150*time.Millisecond, func() { c1 <- true })
		if !t0.Stop() {
			errs = append(errs, "failed to stop event 0")
			continue
		}
		if !t1.Stop() {
			errs = append(errs, "failed to stop event 1")
			continue
		}
		for _, timer := range []*testtime.TestTimer{tInitial, t0, t1} {
			timer.Elapse(200 * time.Millisecond)
		}
		select {
		case <-t0.ExpiredC():
			errs = append(errs, "event 0 was not stopped")
			continue
		case <-c1:
			errs = append(errs, "event 1 was not stopped")
			continue
		default:
		}
		if t1.Stop() {
			errs = append(errs, "Stop returned true twice")
			continue
		}

		// Test passed, so all done.
		if len(errs) > 0 {
			c.Logf("saw %d errors, ignoring to avoid flakiness", len(errs))
			logErrs()
		}

		return
	}

	c.Errorf("saw %d errors", len(errs))
	logErrs()
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func (s *testtimeSuite) TestStdlibTimerStopStress(c *C) {
	for i := 0; i < 100; i++ {
		go func(i int) {
			timer := testtime.AfterFunc(2*time.Second, func() {
				c.Errorf("timer %d was not stopped", i)
			})
			timer.Elapse(1 * time.Second)
			timer.Stop()
			timer.Elapse(1 * time.Second)
		}(i)
	}
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func testReset(d time.Duration) error {
	t0 := testtime.NewTimer(2 * d)
	t0.Elapse(d)
	if !t0.Reset(3 * d) {
		return errors.New("resetting unfired timer returned false")
	}
	t0.Elapse(2 * d)
	select {
	case <-t0.ExpiredC():
		return errors.New("timer fired early")
	default:
	}
	t0.Elapse(2 * d)
	select {
	case <-t0.ExpiredC():
	default:
		return errors.New("reset timer did not fire")
	}

	if t0.Reset(50 * time.Millisecond) {
		return errors.New("resetting expired timer returned true")
	}
	return nil
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func (s *testtimeSuite) TestStdlibReset(c *C) {
	// We try to run this test with increasingly larger multiples
	// until one works so slow, loaded hardware isn't as flaky,
	// but without slowing down fast machines unnecessarily.
	//
	// (maxDuration is several orders of magnitude longer than we
	// expect this test to actually take on a fast, unloaded machine.)
	d := 1 * time.Millisecond
	const maxDuration = 10 * time.Second
	for {
		err := testReset(d)
		if err == nil {
			break
		}
		d *= 2
		if d > maxDuration {
			c.Error(err)
		}
		c.Logf("%v; trying duration %v", err, d)
	}
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test that zero duration timers aren't missed by the scheduler. Regression test for issue 44868.
func (s *testtimeSuite) TestStdlibZeroTimerNewTimer(c *C) {
	testZeroTimer(c, testtime.NewTimer)
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test that zero duration timers aren't missed by the scheduler. Regression test for issue 44868.
func (s *testtimeSuite) TestStdlibZeroTimerAfterFunc(c *C) {
	testZeroTimer(c, newTimerFunc)
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test that zero duration timers aren't missed by the scheduler. Regression test for issue 44868.
func (s *testtimeSuite) TestStdlibZeroTimerAfterFuncReset(c *C) {
	timer := newTimerFunc(time.Hour)
	testZeroTimer(c, func(d time.Duration) *testtime.TestTimer {
		timer.Reset(d)
		return timer
	})
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func testZeroTimer(c *C, newTimer func(time.Duration) *testtime.TestTimer) {
	// XXX: stdlib does 1000000, but that's really slow, so do 1/10 that
	for i := 0; i < 100000; i++ {
		s := time.Now()
		ti := newTimer(0)
		<-ti.ExpiredC()
		if diff := time.Since(s); diff > 2*time.Second {
			c.Errorf("Expected time to get value from Timer channel in less than 2 sec, took %v", diff)
		}
	}
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test that rapidly moving a timer earlier doesn't cause it to get dropped.
// Issue 47329.
func (s *testtimeSuite) TestStdlibTimerModifiedEarlier(c *C) {
	past := time.Until(time.Unix(0, 0))
	count := 1000
	fail := 0
	for i := 0; i < count; i++ {
		timer := newTimerFunc(time.Hour)
		for j := 0; j < 10; j++ {
			if !timer.Stop() {
				select {
				case <-timer.ExpiredC():
					// This shouldn't be necessary since we comply with 1.23
					// behavior:
					// "as of Go 1.23, any receive from t.C after Stop has
					// returned is guaranteed to block rather than receive a
					// stale time value from before the Stop"
					//
					// See: https://cs.opensource.google/go/go/+/refs/tags/go1.23.3:src/time/sleep.go;l=105
				default:
				}
			}
			timer.Reset(past)
		}

		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		now := time.Now()
		select {
		case <-timer.ExpiredC():
			if since := time.Since(now); since > 8*time.Second {
				c.Errorf("timer took too long (%v)", since)
				fail++
			}
		case <-deadline.C:
			c.Error("deadline expired")
		}
	}

	if fail > 0 {
		c.Errorf("%d failures", fail)
	}
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test that rapidly moving timers earlier and later doesn't cause
// some of the sleep times to be lost.
// Issue 47762
func (s *testtimeSuite) TestStdlibAdjustTimers(c *C) {
	timers := make([]*testtime.TestTimer, 100)
	states := make([]int, len(timers))
	indices := randutil.Perm(len(timers))

	for len(indices) != 0 {
		var ii = randutil.Intn(len(indices))
		var i = indices[ii]

		var timer = timers[i]
		var state = states[i]
		states[i]++

		switch state {
		case 0:
			timers[i] = newTimerFunc(0)

		case 1:
			<-timer.ExpiredC() // Timer is now idle.

		// Reset to various long durations, which we'll cancel.
		case 2:
			if timer.Reset(1 * time.Minute) {
				panic("shouldn't be active (1)")
			}
		case 4:
			if timer.Reset(3 * time.Minute) {
				panic("shouldn't be active (3)")
			}
		case 6:
			if timer.Reset(2 * time.Minute) {
				panic("shouldn't be active (2)")
			}

		// Stop and drain a long-duration timer.
		case 3, 5, 7:
			if !timer.Stop() {
				c.Logf("timer %d state %d Stop returned false", i, state)
				<-timer.ExpiredC()
			}

		// Start a short-duration timer we expect to select without blocking.
		case 8:
			if timer.Reset(0) {
				c.Fatal("timer.Reset returned true")
			}
		case 9:
			now := time.Now()
			<-timer.ExpiredC()
			dur := time.Since(now)
			if dur > 750*time.Millisecond {
				c.Errorf("timer %d took %v to complete", i, dur)
			}

		// Timer is done. Swap with tail and remove.
		case 10:
			indices[ii] = indices[len(indices)-1]
			indices = indices[:len(indices)-1]
		}
	}
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func (s *testtimeSuite) TestStdlibStopResult(c *C) {
	testStopResetResult(c, true)
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
func (s *testtimeSuite) TestStdlibResetResult(c *C) {
	testStopResetResult(c, false)
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test that when racing between running a timer and stopping a timer Stop
// consistently indicates whether a value can be read from the channel.
// Issue #69312.
func testStopResetResult(c *C, testStop bool) {
	stopOrReset := func(timer *testtime.TestTimer) bool {
		if testStop {
			return timer.Stop()
		} else {
			return timer.Reset(1 * time.Hour)
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	const N = 1000
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 100; j++ {
				timer1 := testtime.NewTimer(1 * time.Millisecond)
				timer2 := testtime.NewTimer(1 * time.Millisecond)
				if randutil.Intn(2) == 0 {
					timer1.Elapse(time.Millisecond)
				} else {
					timer2.Elapse(time.Millisecond)
				}
				select {
				case <-timer1.ExpiredC():
					if !stopOrReset(timer2) {
						// The test fails if this
						// channel read times out.
						<-timer2.ExpiredC()
					}
				case <-timer2.ExpiredC():
					if !stopOrReset(timer1) {
						// The test fails if this
						// channel read times out.
						<-timer1.ExpiredC()
					}
				}
			}
		}()
	}
	close(start)
	wg.Wait()
}

// Adapted from src/time/sleep_test.go as of go1.23.3.
//
// Test having a large number of goroutines wake up a timer simultaneously.
// This used to trigger a crash when run under x/tools/cmd/stress.
func (s *testtimeSuite) TestStdlibMultiWakeupTimer(c *C) {
	goroutines := runtime.GOMAXPROCS(0)
	timer := testtime.NewTimer(time.Nanosecond)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 10000; j++ {
				select {
				case <-timer.ExpiredC():
				default:
				}
				timer.Reset(time.Nanosecond)
			}
		}()
	}
	doneChan := make(chan struct{})
	go func() {
		// Time won't elapse on its own, so we do it manually
		for {
			select {
			case <-doneChan:
				return
			default:
				timer.Elapse(time.Nanosecond)
			}
		}
	}()
	wg.Wait()
	close(doneChan)
}
