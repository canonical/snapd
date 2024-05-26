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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
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

func mockRefreshCandidate(snapName, channel, version string, revision snap.Revision) interface{} {
	sup := &snapstate.SnapSetup{
		Channel: channel,
		SideInfo: &snap.SideInfo{
			Revision: revision,
			RealName: snapName,
		},
		Version: version,
	}
	return snapstate.MockRefreshCandidate(sup)
}

func (s *refreshSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })
	s.st = state.New(nil)
	s.mockHandler = hooktest.NewMockHandler()

	// snapstate.AffectedByRefreshCandidates needs a cached iface repo
	repo := interfaces.NewRepository()
	// no interfaces needed for this test suite
	s.st.Lock()
	defer s.st.Unlock()
	ifacerepo.Replace(s.st, repo)
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
	args: []string{"refresh", "--proceed", "--show-lock"},
	err:  "cannot use --proceed and --show-lock together",
}, {
	args: []string{"refresh", "--hold", "--show-lock"},
	err:  "cannot use --hold and --show-lock together",
}, {
	args: []string{"refresh", "--pending"},
	refreshCandidates: map[string]interface{}{
		"snap1": mockRefreshCandidate("snap1", "edge", "v1", snap.Revision{N: 3}),
	},
	stdout: "pending: ready\nchannel: edge\nversion: v1\nrevision: 3\nbase: false\nrestart: false\n",
}, {
	args:   []string{"refresh", "--pending"},
	stdout: "pending: none\nchannel: stable\nbase: false\nrestart: false\n",
}, {
	args: []string{"refresh", "--pending"},
	refreshCandidates: map[string]interface{}{
		"snap1-base": mockRefreshCandidate("snap1-base", "edge", "v1", snap.Revision{N: 3}),
	},
	stdout: "pending: none\nchannel: stable\nbase: true\nrestart: false\n",
}, {
	args: []string{"refresh", "--pending"},
	refreshCandidates: map[string]interface{}{
		"kernel": mockRefreshCandidate("kernel", "edge", "v1", snap.Revision{N: 3}),
	},
	stdout: "pending: none\nchannel: stable\nbase: false\nrestart: true\n",
}, {
	args:      []string{"refresh", "--pending"},
	inhibited: true,
	stdout:    "pending: inhibited\nchannel: stable\nbase: false\nrestart: false\n",
}, {
	args: []string{"refresh", "--hold"},
	err:  `internal error: snap "snap1" is not affected by any snaps`,
}}

func (s *refreshSuite) TestRefreshFromHook(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)
	mockInstalledSnap(c, s.st, `name: snap1
base: snap1-base
version: 1
hooks:
 gate-auto-refresh:
`, "")
	mockInstalledSnap(c, s.st, `name: snap1-base
type: base
version: 1
`, "")
	mockInstalledSnap(c, s.st, `name: kernel
type: kernel
version: 1
`, "")
	s.st.Unlock()

	for _, test := range refreshFromHookTests {
		s.st.Lock()
		s.st.Set("refresh-candidates", test.refreshCandidates)
		if test.inhibited {
			var snapst snapstate.SnapState
			c.Assert(snapstate.Get(s.st, "snap1", &snapst), IsNil)
			snapst.RefreshInhibitedTime = &time.Time{}
			snapstate.Set(s.st, "snap1", &snapst)
		}
		s.st.Unlock()

		stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, test.args, 0))
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
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)

	mockInstalledSnap(c, s.st, `name: snap1
base: snap1-base
version: 1
hooks:
 gate-auto-refresh:
`, "")
	mockInstalledSnap(c, s.st, `name: snap1-base
type: base
version: 1
`, "")

	candidates := map[string]interface{}{
		"snap1-base": mockRefreshCandidate("snap1-base", "edge", "v1", snap.Revision{N: 3}),
	}
	s.st.Set("refresh-candidates", candidates)

	s.st.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--hold"}, 0))

	c.Check(string(stdout), Equals, "hold: 48h0m0s\n")
	c.Check(string(stderr), Equals, "")

	mockContext.Lock()
	defer mockContext.Unlock()
	action := mockContext.Cached("action")
	c.Assert(action, NotNil)
	c.Check(action, Equals, snapstate.GateAutoRefreshHold)

	var gating map[string]map[string]interface{}
	c.Assert(s.st.Get("snaps-hold", &gating), IsNil)
	c.Check(gating["snap1-base"]["snap1"], NotNil)
}

func (s *refreshSuite) TestRefreshProceed(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)

	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "")

	// pretend snap foo is held initially
	_ = mylog.Check2(snapstate.HoldRefresh(s.st, snapstate.HoldAutoRefresh, "snap1", 0, "foo"))
	c.Check(err, IsNil)
	s.st.Unlock()

	// validity check
	var gating map[string]map[string]interface{}
	s.st.Lock()
	snapsHold := s.st.Get("snaps-hold", &gating)
	s.st.Unlock()
	c.Assert(snapsHold, IsNil)
	c.Check(gating["foo"]["snap1"], NotNil)

	mockContext.Lock()
	mockContext.Set("affecting-snaps", []string{"foo"})
	mockContext.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0))

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
	stdout, stderr = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--pending", "--proceed"}, 0))

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
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)
	s.st.Unlock()

	_, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh"}, 0))
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

	// note: don't mock the plug, it's enough to have it in conns
	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "")

	s.st.Set("conns", map[string]interface{}{
		"foo:plug core:slot": map[string]interface{}{"interface": "snap-refresh-control"},
	})

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// foo is the snap that is going to call --proceed.
	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext := mylog.Check2(hookstate.NewContext(nil, s.st, setup, nil, ""))
	c.Check(err, IsNil)
	s.st.Unlock()
	defer s.st.Lock()

	_, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0))

	c.Check(called, Equals, true)
}

func (s *refreshSuite) TestPendingFromSnapNoRefreshCandidates(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "")

	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext := mylog.Check2(hookstate.NewContext(nil, s.st, setup, nil, ""))
	c.Check(err, IsNil)
	s.st.Unlock()
	defer s.st.Lock()

	stdout, _ := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--pending"}, 0))

	c.Check(string(stdout), Equals, "pending: none\nchannel: stable\nbase: false\nrestart: false\n")
}

func (s *refreshSuite) TestPendingFromSnapWithCohort(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "some-cohort-key")

	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext := mylog.Check2(hookstate.NewContext(nil, s.st, setup, nil, ""))
	c.Check(err, IsNil)

	s.st.Unlock()
	defer s.st.Lock()

	stdout, _ := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--pending"}, 0))

	// cohort is not printed if snap-refresh-control isn't connected
	c.Check(string(stdout), Equals, "pending: none\nchannel: stable\nbase: false\nrestart: false\n")

	s.st.Lock()
	s.st.Set("conns", map[string]interface{}{
		"foo:plug core:slot": map[string]interface{}{"interface": "snap-refresh-control"},
	})
	s.st.Unlock()

	stdout, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--pending"}, 0))

	// cohort is printed
	c.Check(string(stdout), Equals, "pending: none\nchannel: stable\ncohort: some-cohort-key\nbase: false\nrestart: false\n")
}

func (s *refreshSuite) TestPendingWithCohort(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "some-cohort-key")

	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)

	s.st.Set("conns", map[string]interface{}{
		"foo:plug core:slot": map[string]interface{}{"interface": "snap-refresh-control"},
	})

	s.st.Unlock()
	defer s.st.Lock()

	stdout, _ := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--pending"}, 0))

	// cohort is printed
	c.Check(string(stdout), Equals, "pending: none\nchannel: stable\ncohort: some-cohort-key\nbase: false\nrestart: false\n")
}

func (s *refreshSuite) TestRefreshProceedFromSnapError(c *C) {
	restore := ctlcmd.MockAutoRefreshForGatingSnap(func(st *state.State, gatingSnap string) error {
		c.Check(gatingSnap, Equals, "foo")
		return fmt.Errorf("boom")
	})
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()
	// note: don't mock the plug, it's enough to have it in conns
	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "")
	s.st.Set("conns", map[string]interface{}{
		"foo:plug core:slot": map[string]interface{}{"interface": "snap-refresh-control"},
	})

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// foo is the snap that is going to call --proceed.
	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext := mylog.Check2(hookstate.NewContext(nil, s.st, setup, nil, ""))
	c.Check(err, IsNil)
	s.st.Unlock()
	defer s.st.Lock()

	_, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0))
	c.Assert(err, ErrorMatches, "boom")
}

func (s *refreshSuite) TestRefreshProceedFromSnapErrorNoSnapRefreshControl(c *C) {
	var called bool
	restore := ctlcmd.MockAutoRefreshForGatingSnap(func(st *state.State, gatingSnap string) error {
		called = true
		return nil
	})
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()
	// note: don't mock the plug, it's enough to have it in conns
	mockInstalledSnap(c, s.st, `name: foo
version: 1
`, "")
	s.st.Set("conns", map[string]interface{}{
		"foo:plug core:slot": map[string]interface{}{
			"interface": "snap-refresh-control",
			"undesired": true,
		},
	})

	// enable gate-auto-refresh-hook feature
	tr := config.NewTransaction(s.st)
	tr.Set("core", "experimental.gate-auto-refresh-hook", true)
	tr.Commit()

	// foo is the snap that is going to call --proceed.
	setup := &hookstate.HookSetup{Snap: "foo", Revision: snap.R(1)}
	mockContext := mylog.Check2(hookstate.NewContext(nil, s.st, setup, nil, ""))
	c.Check(err, IsNil)
	s.st.Unlock()
	defer s.st.Lock()

	_, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0))
	c.Assert(err, ErrorMatches, "cannot proceed: requires snap-refresh-control interface")
	c.Assert(called, Equals, false)

	s.st.Lock()
	s.st.Set("conns", map[string]interface{}{
		"foo:plug core:slot": map[string]interface{}{
			"interface": "other",
		},
	})
	s.st.Unlock()

	_, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--proceed"}, 0))
	c.Assert(err, ErrorMatches, "cannot proceed: requires snap-refresh-control interface")
	c.Assert(called, Equals, false)
}

func (s *refreshSuite) TestRefreshRegularUserForbidden(c *C) {
	s.st.Lock()
	setup := &hookstate.HookSetup{Snap: "snap", Revision: snap.R(1)}
	s.st.Unlock()

	mockContext := mylog.Check2(hookstate.NewContext(nil, s.st, setup, s.mockHandler, ""))

	_, _ = mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh"}, 1000))
	c.Assert(err, ErrorMatches, `cannot use "refresh" with uid 1000, try with sudo`)
	forbidden, _ := err.(*ctlcmd.ForbiddenCommandError)
	c.Assert(forbidden, NotNil)
}

func (s *refreshSuite) TestRefreshPrintInhibitHint(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)
	s.st.Unlock()

	lock := mylog.Check2(snaplock.OpenLock("snap1"))

	mylog.Check(lock.Lock())

	inhibitInfo := runinhibit.InhibitInfo{Previous: snap.R(1)}
	c.Check(runinhibit.LockWithHint("snap1", runinhibit.HintInhibitedForRefresh, inhibitInfo), IsNil)
	lock.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--show-lock"}, 0))

	c.Check(string(stdout), Equals, "refresh")
	c.Check(string(stderr), Equals, "")
}

func (s *refreshSuite) TestRefreshPrintInhibitHintEmpty(c *C) {
	s.st.Lock()
	task := s.st.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "some-snap", Revision: snap.R(1), Hook: "gate-auto-refresh"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.st, setup, s.mockHandler, ""))
	c.Check(err, IsNil)
	s.st.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"refresh", "--show-lock"}, 0))

	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}
