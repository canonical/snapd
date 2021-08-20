// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type refreshSuite struct {
	testutil.BaseTest
	st          *state.State
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&refreshSuite{})

func mockRefreshCandidate(snapName, instanceKey, channel, version string, revision snap.Revision) interface{} {
	sup := &snapstate.SnapSetup{
		Channel:     channel,
		InstanceKey: instanceKey,
		SideInfo: &snap.SideInfo{
			Revision: revision,
			RealName: snapName,
		},
	}
	return snapstate.MockRefreshCandidate(sup, version)
}

func (s *refreshSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.st = state.New(nil)
	s.mockHandler = hooktest.NewMockHandler()
}

var refreshFromHookTests = []struct {
	args                []string
	base, restart       bool
	inhibited           bool
	refreshCandidates   map[string]interface{}
	stdout, stderr, err string
	exitCode            int
}{{
	args: []string{"refresh", "--proceed", "--hold"},
	err:  "cannot use --proceed and --hold together",
}, {
	args:              []string{"refresh", "--pending"},
	refreshCandidates: map[string]interface{}{"snap1": mockRefreshCandidate("snap1", "", "edge", "v1", snap.Revision{N: 3})},
	stdout:            "pending: ready\nchannel: edge\nversion: v1\nrevision: 3\nbase: false\nrestart: false\n",
}, {
	args:   []string{"refresh", "--pending"},
	stdout: "pending: none\nchannel: stable\nbase: false\nrestart: false\n",
}, {
	args:    []string{"refresh", "--pending"},
	base:    true,
	restart: true,
	stdout:  "pending: none\nchannel: stable\nbase: true\nrestart: true\n",
}, {
	args:      []string{"refresh", "--pending"},
	inhibited: true,
	stdout:    "pending: inhibited\nchannel: stable\nbase: false\nrestart: false\n",
}}

func (s *refreshSuite) TestRefreshFromHook(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)
	s.st.Unlock()

	for _, test := range refreshFromHookTests {
		mockContext.Lock()
		mockContext.Set("base", test.base)
		mockContext.Set("restart", test.restart)
		s.st.Set("refresh-candidates", test.refreshCandidates)
		snapst := &snapstate.SnapState{
			Active:          true,
			Sequence:        []*snap.SideInfo{{RealName: "snap1", Revision: snap.R(1)}},
			Current:         snap.R(2),
			TrackingChannel: "stable",
		}
		if test.inhibited {
			snapst.RefreshInhibitedTime = &time.Time{}
		}
		snapstate.Set(s.st, "snap1", snapst)
		mockContext.Unlock()

		stdout, stderr, err := ctlcmd.Run(mockContext, test.args, 0)
		comment := Commentf("%s", test.args)
		if test.exitCode > 0 {
			c.Check(err, DeepEquals, &ctlcmd.UnsuccessfulError{ExitCode: test.exitCode}, comment)
		} else {
			if test.err == "" {
				c.Check(err, IsNil, comment)
			} else {
				c.Check(err, ErrorMatches, test.err, comment)
			}
		}

		c.Check(string(stdout), Equals, test.stdout, comment)
		c.Check(string(stderr), Equals, "", comment)
	}
}

func (s *refreshSuite) TestRefreshHold(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)

	mockInstalledSnap(c, s.st, `name: foo
version: 1
`)

	s.st.Unlock()

	mockContext.Lock()
	mockContext.Set("affecting-snaps", []string{"foo"})
	mockContext.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"refresh", "--hold"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	mockContext.Lock()
	defer mockContext.Unlock()
	action := mockContext.Cached("action")
	c.Assert(action, NotNil)
	c.Check(action, Equals, snapstate.GateAutoRefreshHold)

	var gating map[string]map[string]interface{}
	c.Assert(s.st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating["foo"]["snap1"], NotNil)
}

func (s *refreshSuite) TestRefreshProceed(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)

	mockInstalledSnap(c, s.st, `name: foo
version: 1
`)

	// pretend snap foo is held initially
	c.Check(snapstate.HoldRefresh(s.st, "snap1", 0, "foo"), IsNil)
	s.st.Unlock()

	// sanity check
	var gating map[string]map[string]interface{}
	s.st.Lock()
	snapsHold := s.st.Get("snaps-hold", &gating)
	s.st.Unlock()
	c.Assert(snapsHold, IsNil)
	c.Check(gating["foo"]["snap1"], NotNil)

	mockContext.Lock()
	mockContext.Set("affecting-snaps", []string{"foo"})
	mockContext.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	mockContext.Lock()
	defer mockContext.Unlock()
	action := mockContext.Cached("action")
	c.Assert(action, NotNil)
	c.Check(action, Equals, snapstate.GateAutoRefreshProceed)

	// and it is still held (for hook handler to execute actual proceed logic).
	gating = nil
	c.Assert(s.st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating["foo"]["snap1"], NotNil)

	mockContext.Cache("action", nil)

	mockContext.Unlock()
	defer mockContext.Lock()

	// refresh --pending --proceed is the same as just saying --proceed.
	stdout, stderr, err = ctlcmd.Run(mockContext, []string{"refresh", "--pending", "--proceed"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")

	mockContext.Lock()
	defer mockContext.Unlock()
	action = mockContext.Cached("action")
	c.Assert(action, NotNil)
	c.Check(action, Equals, snapstate.GateAutoRefreshProceed)
}

func (s *refreshSuite) TestRefreshFromUnsupportedHook(c *C) {
	s.st.Lock()

	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap", Revision: snap.R(1), Hook: "install"}
	mockContext, err := hookstate.NewContext(task, s.st, setup, s.mockHandler, "")
	c.Check(err, IsNil)
	s.st.Unlock()

	_, _, err = ctlcmd.Run(mockContext, []string{"refresh"}, 0)
	c.Check(err, ErrorMatches, `can only be used from gate-auto-refresh hook`)
}

func (s *refreshSuite) TestRefreshProceedFromSnap(c *C) {
	var called bool
	restore := ctlcmd.MockAutoRefreshForGatingSnap(func(st *state.State, gatingSnap string) error {
		called = true
		c.Check(gatingSnap, Equals, "foo")
		return nil
	})
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()
	mockInstalledSnap(c, s.st, `name: foo
version: 1
`)

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// foo is the snap that is going to call --proceed.
	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext, err := hookstate.NewContext(nil, s.st, setup, nil, "")
	c.Check(err, IsNil)
	s.st.Unlock()
	defer s.st.Lock()

	_, _, err = ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0)
	c.Assert(err, IsNil)
	c.Check(called, Equals, true)
}

func (s *refreshSuite) TestRefreshProceedFromSnapError(c *C) {
	restore := ctlcmd.MockAutoRefreshForGatingSnap(func(st *state.State, gatingSnap string) error {
		c.Check(gatingSnap, Equals, "foo")
		return fmt.Errorf("boom")
	})
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()
	mockInstalledSnap(c, s.st, `name: foo
version: 1
`)

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// foo is the snap that is going to call --proceed.
	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext, err := hookstate.NewContext(nil, s.st, setup, nil, "")
	c.Check(err, IsNil)
	s.st.Unlock()
	defer s.st.Lock()

	_, _, err = ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0)
	c.Assert(err, ErrorMatches, "boom")
}

func (s *refreshSuite) TestRefreshRegularUserForbidden(c *C) {
	s.st.Lock()
	setup := &hookstate.HookSetup{Snap: "snap", Revision: snap.R(1)}
	s.st.Unlock()

	mockContext, err := hookstate.NewContext(nil, s.st, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	_, _, err = ctlcmd.Run(mockContext, []string{"refresh"}, 1000)
	c.Assert(err, ErrorMatches, `cannot use "refresh" with uid 1000, try with sudo`)
	forbidden, _ := err.(*ctlcmd.ForbiddenCommandError)
	c.Assert(forbidden, NotNil)
}
