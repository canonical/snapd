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
		chg.Set("snapctl-initiated-by", opts.initiatorSnap)
	}
	if opts.withLastAccessed {
		chg.Set("snapctl-last-accessed", opts.lastAccessedTime)
	}

	task.SetStatus(taskStatus)

	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "install"}
	ctx, err := hookstate.NewContext(task, st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	return st, ctx, chg.ID()
}

func (s *isReadySuite) TestIsReadyNoContext(c *C) {
	stdout, stderr, err := ctlcmd.Run(nil, []string{"is-ready", "1"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot invoke snapctl operation commands.*from outside of a snap`)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *isReadySuite) TestIsReadyInvalidArgsTooFew(c *C) {
	_, ctx, _ := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready"}, 0, nil)
	c.Assert(err, ErrorMatches, `invalid number of arguments: expected 1, got 0`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyInvalidArgsTooMany(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID, "extra-arg"}, 0, nil)
	c.Assert(err, ErrorMatches, `invalid number of arguments: expected 1, got 2`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyChangeNotFound(c *C) {
	_, ctx, _ := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", "nonexistent-id"}, 0, nil)
	c.Assert(err, ErrorMatches, `change "nonexistent-id" not found`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyMissingInitiatorAttr(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		// withInitiator intentionally omitted
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, ErrorMatches, `could not find initiator attribute for change .*`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyWrongInitiator(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "other-snap", // different from context snap "test-snap"
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, ErrorMatches, `change .* was initiated by another snap`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyMissingLastAccessedAttr(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator: true,
		initiatorSnap: "test-snap",
		// withLastAccessed intentionally omitted
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, ErrorMatches, `could not find last accessed attribute for change .*`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyInvalidLastAccessedFormat(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: "not-a-valid-time",
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, ErrorMatches, `invalid last accessed time format for change .*`)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyRecentAccessSleep(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		// Use a timestamp in the future to guarantee since < 200ms.
		lastAccessedTime: time.Now().Add(time.Second).Format(time.RFC3339),
	})

	var sleptFor time.Duration
	s.AddCleanup(ctlcmd.MockTimeSleep(func(d time.Duration) {
		sleptFor = d
	}))

	stdout, _, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)

	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "Done")
	// A sleep should have been triggered because the access was very recent.
	c.Check(sleptFor > 0, Equals, true)
}

func (s *isReadySuite) TestIsReadyStatusDone(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoneStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "Done")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyStatusDoing(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.DoingStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 1})
	c.Check(string(stdout), Equals, "Doing")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyStatusError(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.ErrorStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 1})
	c.Check(string(stdout), Equals, "Error")
	c.Check(string(stderr), Equals, "")
}

func (s *isReadySuite) TestIsReadyStatusHold(c *C) {
	_, ctx, changeID := s.setupChangeAndContext(c, state.HoldStatus, changeSetupOpts{
		withInitiator:    true,
		initiatorSnap:    "test-snap",
		withLastAccessed: true,
		lastAccessedTime: time.Now().Add(-time.Second).Format(time.RFC3339),
	})

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"is-ready", changeID}, 0, nil)
	c.Assert(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: 1})
	c.Check(string(stdout), Equals, "Hold")
	c.Check(string(stderr), Equals, "")
}
