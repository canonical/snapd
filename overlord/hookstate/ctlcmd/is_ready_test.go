// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ctlcmd_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type isReadySuite struct {
	testutil.BaseTest
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&isReadySuite{})

func (s *isReadySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.mockHandler = hooktest.NewMockHandler()
}

// setupChangeAndContext creates a state, a change (with an optional initiator),
// and a non-ephemeral hook context for "test-snap".
func (s *isReadySuite) setupChangeAndContext(c *C, taskStatus state.Status, initiatorSnap string) (*state.State, *hookstate.Context, string) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", "install via snapctl")
	task := st.NewTask("test-task", "test task")
	chg.AddTask(task)

	if initiatorSnap != "" {
		chg.Set("initiated-by-snap", initiatorSnap)
	}

	task.SetStatus(taskStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return st, ctx, chg.ID()
}

func (s *isReadySuite) TestIsReadyNoContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"is-ready", "1"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot invoke snapctl operation commands.*from outside of a snap`)
}

func (s *isReadySuite) TestIsReadyArgCount(c *C) {
	_, ctx, _ := s.setupChangeAndContext(c, state.DoneStatus, "test-snap")
	_, _, err := ctlcmd.Run(ctx, []string{"is-ready"}, 0, nil)
	c.Assert(err, ErrorMatches, `invalid number of arguments: expected 1, got 0`)

	_, _, err = ctlcmd.Run(ctx, []string{"is-ready", "1", "extra-arg"}, 0, nil)
	c.Assert(err, ErrorMatches, `invalid number of arguments: expected 1, got 2`)
}

func (s *isReadySuite) TestIsReadyChangeNotFound(c *C) {
	_, ctx, _ := s.setupChangeAndContext(c, state.DoneStatus, "")
	_, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", "nonexistent-id"}, 0, nil)
	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 3})
	c.Check(string(stderr), Matches, `change "nonexistent-id" not found`)
}

func (s *isReadySuite) TestIsReadyLogic(c *C) {
	var logicTests = []struct {
		taskStatus     state.Status
		initiatorSnap  string // empty = don't set initiated-by-snap on the change
		errValue       error  // if set, expect err to deep equal this value
		expectedOut    string
		expectedStderr string // if set, checked as regexp match against stderr
	}{
		{
			taskStatus:     state.DoneStatus,
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `change .* not found`,
		},
		{
			taskStatus:     state.DoneStatus,
			initiatorSnap:  "other-snap", // different from context snap "test-snap"
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `change .* not found`,
		},
		{
			taskStatus:    state.DoneStatus,
			initiatorSnap: "test-snap",
		},
		{
			taskStatus:    state.DoingStatus,
			initiatorSnap: "test-snap",
			errValue:      &ctlcmd.UnsuccessfulError{ExitCode: 1},
		},
		{
			taskStatus:     state.ErrorStatus,
			initiatorSnap:  "test-snap",
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 2},
			expectedStderr: `change finished with status Error`,
		},
		{
			taskStatus:     state.HoldStatus,
			initiatorSnap:  "test-snap",
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 2},
			expectedStderr: `change finished with status Hold`,
		},
	}

	for _, tt := range logicTests {
		_, ctx, changeID := s.setupChangeAndContext(c, tt.taskStatus, tt.initiatorSnap)
		stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
		if tt.errValue != nil {
			c.Assert(err, DeepEquals, tt.errValue)
		} else {
			c.Assert(err, IsNil)
		}
		c.Check(string(stdout), Equals, tt.expectedOut)
		if tt.expectedStderr != "" {
			c.Check(string(stderr), Matches, tt.expectedStderr)
		} else {
			c.Check(string(stderr), Equals, "")
		}
	}
}

// Rate-limiting tests
func (s *isReadySuite) rateLimitSetup(c *C, taskStatus state.Status, lastAccessedTime any) (*hookstate.Context, string) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", "install via snapctl")
	task := st.NewTask("test-task", "test task")
	chg.AddTask(task)
	chg.Set("initiated-by-snap", "test-snap")

	if lastAccessedTime != nil {
		st.Cache(ctlcmd.ChangeRateLimitKey{ChangeID: chg.ID()}, lastAccessedTime)
	}

	task.SetStatus(taskStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return ctx, chg.ID()
}

// TestIsReadyMissingLastAccessed verifies that is-ready treats a missing
// last-accessed cache entry (e.g. after a snapd restart) as a first access and
// proceeds to report the real change status rather than returning an error.
func (s *isReadySuite) TestIsReadyMissingLastAccessed(c *C) {
	ctx, changeID := s.rateLimitSetup(c, state.DoneStatus, nil)

	_, _, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, IsNil)
}

// TestIsReadyRateLimitDelaysPolling verifies that when a snap polls within the
// 200 ms debounce window, is-ready sleeps for the remaining window duration
// before checking the change status.
func (s *isReadySuite) TestIsReadyRateLimitDelaysPolling(c *C) {
	// A last-accessed time in the future guarantees we are within the debounce
	// window, ensuring timeAfter is called with a positive duration.
	ctx, changeID := s.rateLimitSetup(c, state.DoneStatus, time.Now().Add(time.Second).UnixNano())

	var waitedFor time.Duration
	restore := ctlcmd.MockTimeAfter(func(d time.Duration) <-chan time.Time {
		waitedFor = d
		return make(chan time.Time) // never fires; chg.Ready() wins
	})
	defer restore()

	_, _, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, IsNil)
	c.Check(waitedFor > 0, Equals, true)
}

// TestIsReadyRateLimitTimerFires verifies that when timeAfter fires before the
// change is ready, is-ready reports DoingStatus (exit code 1) and the timer
// channel is drained.
func (s *isReadySuite) TestIsReadyRateLimitTimerFires(c *C) {
	// A last-accessed time in the future puts us inside the debounce window.
	// The task is left in DoingStatus so chg.Ready() never fires, ensuring
	// the timer case is the only one that can win the select.
	ctx, changeID := s.rateLimitSetup(c, state.DoingStatus, time.Now().Add(time.Second).UnixNano())

	timerCh := make(chan time.Time, 1)
	timerCh <- time.Now() // pre-fill so the timer fires immediately
	restore := ctlcmd.MockTimeAfter(func(d time.Duration) <-chan time.Time {
		return timerCh
	})
	defer restore()

	_, _, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 1})
	c.Check(len(timerCh), Equals, 0) // element was consumed by the select
}

// TestIsReadyExpiredWindowSkipsTimeAfter verifies that when the debounce window
// has already elapsed, is-ready returns the change status directly
func (s *isReadySuite) TestIsReadyExpiredWindowSkipsTimeAfter(c *C) {
	// A last-accessed time sufficiently in the past guarantees toWait <= 0.
	ctx, changeID := s.rateLimitSetup(c, state.DoneStatus, time.Now().Add(-time.Second).UnixNano())

	called := false
	restore := ctlcmd.MockTimeAfter(func(d time.Duration) <-chan time.Time {
		called = true
		return make(chan time.Time)
	})
	defer restore()

	_, _, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}
