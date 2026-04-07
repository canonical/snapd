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

// isReadyTestCase describes a single is-ready invocation for the logic tests.
type isReadyTestCase struct {
	desc            string
	nilContext      bool
	taskStatus      state.Status
	initiatorSnap   string   // empty = don't set initiated-by-snap on the change
	appendChangeID bool     // if true, the real change ID is prepended to args
	args            []string // args after "is-ready" (and optional change ID)
	errPattern      string   // if set, expect err to match this regexp
	errValue        error    // if set, expect err to deep equal this value
	expectedOut     string
	expectedStderr  string // if set, checked as regexp match against stderr
}

// setupChangeAndContext creates a state, a change (with an optional initiator),
// and a non-ephemeral hook context for "test-snap".
// The last-accessed cache is always pre-set to one second in the past so that
// the rate-limiter does not interfere with logic tests.
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

	// Pre-set a past last-accessed timestamp so the rate-limiter never fires
	// during logic tests.
	st.Cache("snapctl-test-snap-last-accessed", time.Now().Add(-time.Second).UnixNano())

	task.SetStatus(taskStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return st, ctx, chg.ID()
}

func (s *isReadySuite) runIsReadyTest(c *C, tt isReadyTestCase) {
	var ctx *hookstate.Context
	var changeID string
	if !tt.nilContext {
		_, ctx, changeID = s.setupChangeAndContext(c, tt.taskStatus, tt.initiatorSnap)
	}

	args := []string{"is-ready"}
	if tt.appendChangeID {
		args = append(args, changeID)
	}
	args = append(args, tt.args...)

	stdout, stderr, err := ctlcmd.Run(ctx, args, 0, nil)

	switch {
	case tt.errPattern != "":
		c.Assert(err, ErrorMatches, tt.errPattern)
	case tt.errValue != nil:
		c.Assert(err, DeepEquals, tt.errValue)
	default:
		c.Assert(err, IsNil)
	}

	if tt.nilContext {
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	} else {
		c.Check(string(stdout), Equals, tt.expectedOut)
		c.Check(string(stderr), Matches, tt.expectedStderr)
	}
}

func (s *isReadySuite) TestIsReadyLogic(c *C) {
	var logicTests = []isReadyTestCase{
		{
			desc:       "no context",
			nilContext: true,
			args:       []string{"1"},
			errPattern: `cannot invoke snapctl operation commands.*from outside of a snap`,
		},
		{
			desc:          "too few args",
			taskStatus:    state.DoneStatus,
			initiatorSnap: "test-snap",
			args:          []string{},
			errPattern:    `invalid number of arguments: expected 1, got 0`,
		},
		{
			desc:            "too many args",
			taskStatus:      state.DoneStatus,
			initiatorSnap:   "test-snap",
			appendChangeID: true,
			args:            []string{"extra-arg"},
			errPattern:      `invalid number of arguments: expected 1, got 2`,
		},
		{
			desc:           "change not found",
			taskStatus:     state.DoneStatus,
			args:           []string{"nonexistent-id"},
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `change "nonexistent-id" not found`,
		},
		{
			desc:            "missing initiator attribute",
			taskStatus:      state.DoneStatus,
			appendChangeID: true,
			errValue:        &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr:  `could not find initiator attribute for change .*`,
		},
		{
			desc:            "wrong initiator",
			taskStatus:      state.DoneStatus,
			initiatorSnap:   "other-snap", // different from context snap "test-snap"
			appendChangeID: true,
			errValue:        &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr:  `change .* was initiated by another snap`,
		},
		{
			desc:            "done status",
			taskStatus:      state.DoneStatus,
			initiatorSnap:   "test-snap",
			appendChangeID: true,
		},
		{
			desc:            "doing status",
			taskStatus:      state.DoingStatus,
			initiatorSnap:   "test-snap",
			appendChangeID: true,
			errValue:        &ctlcmd.UnsuccessfulError{ExitCode: 1},
		},
		{
			desc:            "error status",
			taskStatus:      state.ErrorStatus,
			initiatorSnap:   "test-snap",
			appendChangeID: true,
			errValue:        &ctlcmd.UnsuccessfulError{ExitCode: 2},
		},
		{
			desc:            "hold status",
			taskStatus:      state.HoldStatus,
			initiatorSnap:   "test-snap",
			appendChangeID: true,
			errValue:        &ctlcmd.UnsuccessfulError{ExitCode: 2},
		},
	}
	
	for _, tt := range logicTests {
		c.Log("test case: ", tt.desc)
		s.runIsReadyTest(c, tt)
	}
}

// Rate-limiting tests
func (s *isReadySuite) rateLimitSetup(c *C, lastAccessedTime any) (*hookstate.Context, string) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", "install via snapctl")
	task := st.NewTask("test-task", "test task")
	chg.AddTask(task)
	chg.Set("initiated-by-snap", "test-snap")

	if lastAccessedTime != nil {
		st.Cache("snapctl-test-snap-last-accessed", lastAccessedTime)
	}

	task.SetStatus(state.DoneStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return ctx, chg.ID()
}

// TestIsReadyMissingLastAccessed verifies that is-ready returns exit code 3
// when no last-accessed cache entry exists for the calling snap.
func (s *isReadySuite) TestIsReadyMissingLastAccessed(c *C) {
	ctx, changeID := s.rateLimitSetup(c, nil)

	_, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 3})
	c.Check(string(stderr), Matches, `could not find last accessed attribute for change .*`)
}

// TestIsReadyInvalidLastAccessedFormat verifies that is-ready returns exit
// code 3 when the last-accessed cache entry cannot be parsed as RFC 3339.
func (s *isReadySuite) TestIsReadyInvalidLastAccessedFormat(c *C) {
	ctx, changeID := s.rateLimitSetup(c, "not-a-valid-time") // string triggers int64 type-assertion failure

	_, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 3})
	c.Check(string(stderr), Matches, `invalid last accessed time format for change .*`)
}

// TestIsReadyRateLimitDelaysPolling verifies that when a snap polls within the
// 200 ms debounce window, is-ready sleeps for the remaining window duration
// before checking the change status.
func (s *isReadySuite) TestIsReadyRateLimitDelaysPolling(c *C) {
	// A last-accessed time in the future guarantees we are within the debounce
	// window, ensuring timeAfter is called with a positive duration.
	ctx, changeID := s.rateLimitSetup(c, time.Now().Add(time.Second).UnixNano())

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

// TestIsReadyExpiredWindowSkipsTimeAfter verifies that when the debounce window
// has already elapsed, is-ready returns the change status directly from the
// non-blocking select without ever calling timeAfter. Without the st.Lock +
// return in the first select's ready case, the code falls through to the second
// select where timeAfter(-duration) fires immediately, creating a race that can
// return DoingStatus instead of the real status.
func (s *isReadySuite) TestIsReadyExpiredWindowSkipsTimeAfter(c *C) {
	// A last-accessed time sufficiently in the past guarantees toWait <= 0.
	ctx, changeID := s.rateLimitSetup(c, time.Now().Add(-time.Second).UnixNano())

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
