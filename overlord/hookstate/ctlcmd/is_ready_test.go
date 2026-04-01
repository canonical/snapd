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

type changeSetupOpts struct {
	withInitiator    bool
	initiatorSnap    string
	withLastAccessed bool
	lastAccessedTime string
}

// setupChangeAndContext creates a state, a change (with configurable attributes),
// and a non-ephemeral hook context for "test-snap".
func (s *isReadySuite) setupChangeAndContext(c *C, taskStatus state.Status, opts changeSetupOpts) (*state.State, *hookstate.Context, string) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("snapctl-install", "install via snapctl")
	task := st.NewTask("test-task", "test task")
	chg.AddTask(task)

	if opts.withInitiator {
		chg.Set("initiated-by-snap", opts.initiatorSnap)
	}
	if opts.withLastAccessed {
		st.Cache("snapctl-test-snap-last-accessed", opts.lastAccessedTime)
	}

	task.SetStatus(taskStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return st, ctx, chg.ID()
}

func (s *isReadySuite) TestIsReady(c *C) {
	// changeIDPlaceholder is substituted with the real change ID at runtime.
	const changeIDPlaceholder = "<change-id>"

	tests := []struct {
		desc           string
		nilContext     bool
		taskStatus     state.Status
		setupOpts      changeSetupOpts
		args           []string // args after "is-ready"; use changeIDPlaceholder for the real change ID
		errPattern     string   // if set, expect err to match this regexp
		errValue       error    // if set, expect err to deep equal this value
		expectedOut    string
		expectedStderr string // if set, checked as regexp match against stderr
		checkSleep     bool
	}{
		{
			desc:       "no context",
			nilContext: true,
			args:       []string{"1"},
			errPattern: `cannot invoke snapctl operation commands.*from outside of a snap`,
		},
		{
			desc:       "too few args",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:       []string{},
			errPattern: `invalid number of arguments: expected 1, got 0`,
		},
		{
			desc:       "too many args",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:       []string{changeIDPlaceholder, "extra-arg"},
			errPattern: `invalid number of arguments: expected 1, got 2`,
		},
		{
			desc:           "change not found",
			taskStatus:     state.DoneStatus,
			setupOpts:      changeSetupOpts{},
			args:           []string{"nonexistent-id"},
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `change "nonexistent-id" not found`,
		},
		{
			desc:       "missing initiator attribute",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:           []string{changeIDPlaceholder},
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `could not find initiator attribute for change .*`,
		},
		{
			desc:       "wrong initiator",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "other-snap", // different from context snap "test-snap"
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:           []string{changeIDPlaceholder},
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `change .* was initiated by another snap`,
		},
		{
			desc:       "missing last accessed attribute",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator: true,
				initiatorSnap: "test-snap",
			},
			args:           []string{changeIDPlaceholder},
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `could not find last accessed attribute for change .*`,
		},
		{
			desc:       "invalid last accessed format",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: "not-a-valid-time",
			},
			args:           []string{changeIDPlaceholder},
			errValue:       &ctlcmd.UnsuccessfulError{ExitCode: 3},
			expectedStderr: `invalid last accessed time format for change .*`,
		},
		{
			desc:       "recent access triggers sleep",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				// Use a timestamp in the future to guarantee since < 200ms.
				lastAccessedTime: time.Now().Add(time.Second).Format(time.RFC3339),
			},
			args:       []string{changeIDPlaceholder},
			checkSleep: true,
		},
		{
			desc:       "done status",
			taskStatus: state.DoneStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args: []string{changeIDPlaceholder},
		},
		{
			desc:       "doing status",
			taskStatus: state.DoingStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:     []string{changeIDPlaceholder},
			errValue: &ctlcmd.UnsuccessfulError{ExitCode: 1},
		},
		{
			desc:       "error status",
			taskStatus: state.ErrorStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:     []string{changeIDPlaceholder},
			errValue: &ctlcmd.UnsuccessfulError{ExitCode: 2},
		},
		{
			desc:       "hold status",
			taskStatus: state.HoldStatus,
			setupOpts: changeSetupOpts{
				withInitiator:    true,
				initiatorSnap:    "test-snap",
				withLastAccessed: true,
				lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
			},
			args:     []string{changeIDPlaceholder},
			errValue: &ctlcmd.UnsuccessfulError{ExitCode: 2},
		},
	}

	for _, tt := range tests {
		c.Log("test case: ", tt.desc)

		var ctx *hookstate.Context
		var changeID string
		if !tt.nilContext {
			_, ctx, changeID = s.setupChangeAndContext(c, tt.taskStatus, tt.setupOpts)
		}

		args := make([]string, 0, len(tt.args)+1)
		args = append(args, "is-ready")
		for _, a := range tt.args {
			if a == changeIDPlaceholder {
				args = append(args, changeID)
			} else {
				args = append(args, a)
			}
		}

		var waitedFor time.Duration
		var restore func()
		if tt.checkSleep {
			restore = ctlcmd.MockTimeAfter(func(d time.Duration) <-chan time.Time {
				waitedFor = d
				return make(chan time.Time) // never fires; chg.Ready() wins
			})
		}

		stdout, stderr, err := ctlcmd.Run(ctx, args, 0, nil)

		if restore != nil {
			restore()
		}

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
			if tt.expectedStderr != "" {
				c.Check(string(stderr), Matches, tt.expectedStderr)
			} else {
				c.Check(string(stderr), Equals, "")
			}
		}

		if tt.checkSleep {
			c.Check(waitedFor > 0, Equals, true)
		}
	}
}
