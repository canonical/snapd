// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package healthstate_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

func TestHealthState(t *testing.T) { check.TestingT(t) }

type healthSuite struct {
	testutil.BaseTest
	o       *overlord.Overlord
	se      *overlord.StateEngine
	state   *state.State
	hookMgr *hookstate.HookManager
	info    *snap.Info
}

var _ = check.Suite(&healthSuite{})

func (s *healthSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(healthstate.MockCheckTimeout(time.Second))
	dirs.SetRootDir(c.MkDir())

	s.o = overlord.Mock()
	s.state = s.o.State()

	s.hookMgr = mylog.Check2(hookstate.Manager(s.state, s.o.TaskRunner()))
	c.Assert(err, check.IsNil)
	s.se = s.o.StateEngine()
	s.o.AddManager(s.hookMgr)
	s.o.AddManager(s.o.TaskRunner())

	healthstate.Init(s.hookMgr)

	c.Assert(s.o.StartUp(), check.IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	snapstate.ReplaceStore(s.state, storetest.Store{})
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	s.info = snaptest.MockSnapCurrent(c, "{name: test-snap, version: v1}", sideInfo)
}

func (s *healthSuite) TearDownTest(c *check.C) {
	s.hookMgr.StopHooks()
	s.se.Stop()
	s.BaseTest.TearDownTest(c)
}

type healthHookTestCondition int

const (
	noHook = iota
	badHook
	goodHook
)

func (s *healthSuite) TestHealthNoHook(c *check.C) {
	s.testHealth(c, noHook)
}

func (s *healthSuite) TestHealthFailingHook(c *check.C) {
	s.testHealth(c, badHook)
}

func (s *healthSuite) TestHealth(c *check.C) {
	s.testHealth(c, goodHook)
}

func (s *healthSuite) testHealth(c *check.C, cond healthHookTestCondition) {
	var cmd *testutil.MockCmd
	switch cond {
	case badHook:
		cmd = testutil.MockCommand(c, "snap", "exit 1")
	default:
		cmd = testutil.MockCommand(c, "snap", "exit 0")
	}

	if cond != noHook {
		hookFn := filepath.Join(s.info.MountDir(), "meta", "hooks", "check-health")
		c.Assert(os.MkdirAll(filepath.Dir(hookFn), 0755), check.IsNil)
		// the hook won't actually be called, but needs to exist
		c.Assert(os.WriteFile(hookFn, nil, 0755), check.IsNil)
	}

	s.state.Lock()
	task := healthstate.Hook(s.state, "test-snap", snap.R(42))
	change := s.state.NewChange("kind", "summary")
	change.AddTask(task)
	s.state.Unlock()

	c.Assert(task.Kind(), check.Equals, "run-hook")
	var hooksup hookstate.HookSetup

	s.state.Lock()
	mylog.Check(task.Get("hook-setup", &hooksup))
	s.state.Unlock()
	c.Check(err, check.IsNil)

	c.Check(hooksup, check.DeepEquals, hookstate.HookSetup{
		Snap:        "test-snap",
		Hook:        "check-health",
		Revision:    snap.R(42),
		Optional:    true,
		Timeout:     time.Second,
		IgnoreError: false,
	})

	t0 := time.Now()
	s.se.Ensure()
	s.se.Wait()
	tf := time.Now()
	var healths map[string]*healthstate.HealthState
	var health *healthstate.HealthState
	var err2 error
	s.state.Lock()
	status := change.Status()
	mylog.Check(s.state.Get("health", &healths))
	health, err2 = healthstate.Get(s.state, "test-snap")
	s.state.Unlock()
	c.Assert(err2, check.IsNil)

	switch cond {
	case badHook:
		c.Assert(status, check.Equals, state.ErrorStatus)
	default:
		c.Assert(status, check.Equals, state.DoneStatus)
	}
	if cond != noHook {
		c.Assert(err, check.IsNil)
		c.Assert(healths, check.HasLen, 1)
		c.Assert(healths["test-snap"], check.NotNil)
		c.Check(health, check.DeepEquals, healths["test-snap"])
		c.Check(health.Revision, check.Equals, snap.R(42))
		c.Check(health.Status, check.Equals, healthstate.UnknownStatus)
		if cond == badHook {
			c.Check(health.Message, check.Equals, "hook failed")
			c.Check(health.Code, check.Equals, "snapd-hook-failed")
		} else {
			c.Check(health.Message, check.Equals, "hook did not call set-health")
			c.Check(health.Code, check.Equals, "snapd-hook-no-health-set")
		}
		com := check.Commentf("%s ⩼ %s ⩼ %s", t0.Format(time.StampNano), health.Timestamp.Format(time.StampNano), tf.Format(time.StampNano))
		c.Check(health.Timestamp.After(t0) && health.Timestamp.Before(tf), check.Equals, true, com)
		c.Check(cmd.Calls(), check.DeepEquals, [][]string{{"snap", "run", "--hook", "check-health", "-r", "42", "test-snap"}})
	} else {
		// no script -> no health
		c.Assert(err, testutil.ErrorIs, state.ErrNoState)
		c.Check(healths, check.IsNil)
		c.Check(health, check.IsNil)
		c.Check(cmd.Calls(), check.HasLen, 0)
	}
}

func (*healthSuite) TestStatusHappy(c *check.C) {
	for i, str := range healthstate.KnownStatuses {
		status := mylog.Check2(healthstate.StatusLookup(str))
		c.Check(err, check.IsNil, check.Commentf("%v", str))
		c.Check(status, check.Equals, healthstate.HealthStatus(i), check.Commentf("%v", str))
		c.Check(healthstate.HealthStatus(i).String(), check.Equals, str, check.Commentf("%v", str))
	}
}

func (*healthSuite) TestStatusUnhappy(c *check.C) {
	status := mylog.Check2(healthstate.StatusLookup("rabbits"))
	c.Check(status, check.Equals, healthstate.HealthStatus(-1))
	c.Check(err, check.ErrorMatches, `invalid status "rabbits".*`)
	c.Check(status.String(), check.Equals, "invalid (-1)")
}

func (s *healthSuite) TestSetFromHookContext(c *check.C) {
	ctx := mylog.Check2(hookstate.NewContext(nil, s.state, &hookstate.HookSetup{Snap: "foo"}, nil, ""))
	c.Assert(err, check.IsNil)

	ctx.Lock()
	defer ctx.Unlock()

	var hs map[string]*healthstate.HealthState
	c.Check(s.state.Get("health", &hs), testutil.ErrorIs, state.ErrNoState)

	ctx.Set("health", &healthstate.HealthState{Status: 42})
	mylog.Check(healthstate.SetFromHookContext(ctx))
	c.Assert(err, check.IsNil)

	hs = mylog.Check2(healthstate.All(s.state))
	c.Check(err, check.IsNil)
	c.Check(hs, check.DeepEquals, map[string]*healthstate.HealthState{
		"foo": {Status: 42},
	})
}

func (s *healthSuite) TestSetFromHookContextEmpty(c *check.C) {
	ctx := mylog.Check2(hookstate.NewContext(nil, s.state, &hookstate.HookSetup{Snap: "foo"}, nil, ""))
	c.Assert(err, check.IsNil)

	ctx.Lock()
	defer ctx.Unlock()

	var hs map[string]healthstate.HealthState
	c.Check(s.state.Get("health", &hs), testutil.ErrorIs, state.ErrNoState)
	mylog.Check(healthstate.SetFromHookContext(ctx))
	c.Assert(err, check.IsNil)

	// no health in the context -> no health in state
	c.Check(s.state.Get("health", &hs), testutil.ErrorIs, state.ErrNoState)
}
