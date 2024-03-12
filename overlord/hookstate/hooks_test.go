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

package hookstate_test

import (
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

const snapaYaml = `name: snap-a
version: 1
base: base-snap-a
hooks:
    gate-auto-refresh:
`

const snapaBaseYaml = `name: base-snap-a
version: 1
type: base
`

const snapbYaml = `name: snap-b
version: 1
`

type gateAutoRefreshHookSuite struct {
	baseHookManagerSuite
}

var _ = Suite(&gateAutoRefreshHookSuite{})

func (s *gateAutoRefreshHookSuite) SetUpTest(c *C) {
	s.commonSetUpTest(c)

	s.state.Lock()
	defer s.state.Unlock()

	// disable refresh-app-awareness (it's enabled by default);
	// specific tests below enable it back.
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.refresh-app-awareness", false)
	tr.Commit()

	si := &snap.SideInfo{RealName: "snap-a", SnapID: "snap-a-id1", Revision: snap.R(1)}
	snaptest.MockSnap(c, snapaYaml, si)
	snapstate.Set(s.state, "snap-a", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
	})

	si2 := &snap.SideInfo{RealName: "snap-b", SnapID: "snap-b-id1", Revision: snap.R(1)}
	snaptest.MockSnap(c, snapbYaml, si2)
	snapstate.Set(s.state, "snap-b", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si2}),
		Current:  snap.R(1),
	})

	si3 := &snap.SideInfo{RealName: "base-snap-a", SnapID: "base-snap-a-id1", Revision: snap.R(1)}
	snaptest.MockSnap(c, snapaBaseYaml, si3)
	snapstate.Set(s.state, "base-snap-a", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si3}),
		Current:  snap.R(1),
	})

	repo := interfaces.NewRepository()
	// no interfaces needed for this test suite
	ifacerepo.Replace(s.state, repo)
}

func (s *gateAutoRefreshHookSuite) TearDownTest(c *C) {
	s.commonTearDownTest(c)
}

func mockRefreshCandidate(snapName, instanceKey, channel, version string, revision snap.Revision) interface{} {
	sup := &snapstate.SnapSetup{
		Channel:     channel,
		InstanceKey: instanceKey,
		SideInfo: &snap.SideInfo{
			Revision: revision,
			RealName: snapName,
		},
		Version: version,
	}
	return snapstate.MockRefreshCandidate(sup)
}

func (s *gateAutoRefreshHookSuite) settle(c *C) {
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
}

func checkIsHeld(c *C, st *state.State, heldSnap, gatingSnap string) {
	var held map[string]map[string]interface{}
	c.Assert(st.Get("snaps-hold", &held), IsNil)
	c.Check(held[heldSnap][gatingSnap], NotNil)
}

func checkIsNotHeld(c *C, st *state.State, heldSnap string) {
	var held map[string]map[string]interface{}
	c.Assert(st.Get("snaps-hold", &held), IsNil)
	c.Check(held[heldSnap], IsNil)
}

func (s *gateAutoRefreshHookSuite) TestGateAutorefreshHookProceedRuninhibitLock(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")
		ctx.Lock()
		defer ctx.Unlock()

		// check that runinhibit hint has been set by Before() hook handler.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintInhibitedGateRefresh)
		c.Check(info, Equals, runinhibit.InhibitInfo{Previous: snap.R(1)})

		// action is normally set via snapctl; pretend it is --proceed.
		action := snapstate.GateAutoRefreshProceed
		ctx.Cache("action", action)
		return nil, nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// enable refresh-app-awareness
	tr := config.NewTransaction(st)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintInhibitedForRefresh)
	c.Check(info, Equals, runinhibit.InhibitInfo{Previous: snap.R(1)})
}

func (s *gateAutoRefreshHookSuite) TestGateAutorefreshHookHoldUnlocksRuninhibit(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")
		ctx.Lock()
		defer ctx.Unlock()

		// check that runinhibit hint has been set by Before() hook handler.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintInhibitedGateRefresh)
		c.Check(info, Equals, runinhibit.InhibitInfo{Previous: snap.R(1)})

		// action is normally set via snapctl; pretend it is --hold.
		action := snapstate.GateAutoRefreshHold
		ctx.Cache("action", action)
		return nil, nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// enable refresh-app-awareness
	tr := config.NewTransaction(st)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// runinhibit lock is released.
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

// Test that if gate-auto-refresh hook does nothing, the hook handler
// assumes --proceed.
func (s *gateAutoRefreshHookSuite) TestGateAutorefreshDefaultProceedUnlocksRuninhibit(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// validity, refresh is inhibited for snap-a.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintInhibitedGateRefresh)
		c.Check(info, Equals, runinhibit.InhibitInfo{Previous: snap.R(1)})

		// this hook does nothing (action not set to proceed/hold).
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")
		return nil, nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// pretend that snap-a is initially held by itself.
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-a")
	c.Assert(err, IsNil)
	// validity
	checkIsHeld(c, st, "snap-a", "snap-a")

	// enable refresh-app-awareness
	tr := config.NewTransaction(st)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	checkIsNotHeld(c, st, "snap-a")

	// runinhibit lock is released.
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

// Test that if gate-auto-refresh hook does nothing, the hook handler
// assumes --proceed.
func (s *gateAutoRefreshHookSuite) TestGateAutorefreshDefaultProceed(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// no runinhibit because the refresh-app-awareness feature is disabled.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintNotInhibited)
		c.Check(info, Equals, runinhibit.InhibitInfo{})

		// this hook does nothing (action not set to proceed/hold).
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")
		return nil, nil
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// pretend that snap-b is initially held by snap-a.
	_, err := snapstate.HoldRefresh(st, snapstate.HoldAutoRefresh, "snap-a", 0, "snap-b")
	c.Assert(err, IsNil)
	// validity
	checkIsHeld(c, st, "snap-b", "snap-a")

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(change.Err(), IsNil)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	checkIsNotHeld(c, st, "snap-b")

	// no runinhibit because the refresh-app-awareness feature is disabled.
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

// Test that if gate-auto-refresh hook errors out, the hook handler
// assumes --hold.
func (s *gateAutoRefreshHookSuite) TestGateAutorefreshHookError(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// no runinhibit because the refresh-app-awareness feature is disabled.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintNotInhibited)
		c.Check(info, Equals, runinhibit.InhibitInfo{})

		// this hook does nothing (action not set to proceed/hold).
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")
		return []byte("fail"), fmt.Errorf("boom")
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	candidates := map[string]interface{}{"snap-a": mockRefreshCandidate("snap-a", "", "edge", "v1", snap.Revision{N: 3})}
	st.Set("refresh-candidates", candidates)

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(strings.Join(task.Log(), ""), testutil.Contains, "ignoring hook error: fail")
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// and snap-a is now held.
	checkIsHeld(c, st, "snap-a", "snap-a")

	// no runinhibit because the refresh-app-awareness feature is disabled.
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

// Test that if gate-auto-refresh hook errors out, the hook handler
// assumes --hold even if --proceed was requested.
func (s *gateAutoRefreshHookSuite) TestGateAutorefreshHookErrorAfterProceed(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// no runinhibit because the refresh-app-awareness feature is disabled.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintNotInhibited)
		c.Check(info, Equals, runinhibit.InhibitInfo{})

		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")

		// action is normally set via snapctl; pretend it is --proceed.
		ctx.Lock()
		defer ctx.Unlock()
		action := snapstate.GateAutoRefreshProceed
		ctx.Cache("action", action)

		return []byte("fail"), fmt.Errorf("boom")
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	candidates := map[string]interface{}{"snap-a": mockRefreshCandidate("snap-a", "", "edge", "v1", snap.Revision{N: 3})}
	st.Set("refresh-candidates", candidates)

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(strings.Join(task.Log(), ""), testutil.Contains, "ignoring hook error: fail")
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// and snap-a is now held.
	checkIsHeld(c, st, "snap-a", "snap-a")

	// no runinhibit because the refresh-app-awareness feature is disabled.
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

// Test that if gate-auto-refresh hook errors out, the hook handler
// assumes --hold.
func (s *gateAutoRefreshHookSuite) TestGateAutorefreshHookErrorRuninhibitUnlock(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// no runinhibit because the refresh-app-awareness feature is disabled.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintInhibitedGateRefresh)
		c.Check(info, Equals, runinhibit.InhibitInfo{Previous: snap.R(1)})

		// this hook does nothing (action not set to proceed/hold).
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")
		return []byte("fail"), fmt.Errorf("boom")
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// enable refresh-app-awareness
	tr := config.NewTransaction(st)
	tr.Set("core", "experimental.refresh-app-awareness", true)
	tr.Commit()

	candidates := map[string]interface{}{"snap-a": mockRefreshCandidate("snap-a", "", "edge", "v1", snap.Revision{N: 3})}
	st.Set("refresh-candidates", candidates)

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(strings.Join(task.Log(), ""), testutil.Contains, "ignoring hook error: fail")
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// and snap-a is now held.
	checkIsHeld(c, st, "snap-a", "snap-a")

	// inhibit lock is unlocked
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}

func (s *gateAutoRefreshHookSuite) TestGateAutorefreshHookErrorHoldErrorLogged(c *C) {
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// no runinhibit because the refresh-app-awareness feature is disabled.
		hint, info, err := runinhibit.IsLocked("snap-a")
		c.Assert(err, IsNil)
		c.Check(hint, Equals, runinhibit.HintNotInhibited)
		c.Check(info, Equals, runinhibit.InhibitInfo{})

		// this hook does nothing (action not set to proceed/hold).
		c.Check(ctx.HookName(), Equals, "gate-auto-refresh")
		c.Check(ctx.InstanceName(), Equals, "snap-a")

		// simulate failing hook
		return []byte("fail"), fmt.Errorf("boom")
	}
	restore := hookstate.MockRunHook(hookInvoke)
	defer restore()

	st := s.state
	st.Lock()
	defer st.Unlock()

	candidates := map[string]interface{}{"snap-a": mockRefreshCandidate("snap-a", "", "edge", "v1", snap.Revision{N: 3})}
	st.Set("refresh-candidates", candidates)

	task := hookstate.SetupGateAutoRefreshHook(st, "snap-a")
	change := st.NewChange("kind", "summary")
	change.AddTask(task)

	// pretend snap-a wasn't updated for a very long time.
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(st, "snap-a", &snapst), IsNil)
	t := time.Now().Add(-365 * 24 * time.Hour)
	snapst.LastRefreshTime = &t
	snapstate.Set(st, "snap-a", &snapst)

	st.Unlock()
	s.settle(c)
	st.Lock()

	c.Assert(strings.Join(task.Log(), ""), Matches, `.*error: cannot hold some snaps:
 - snap "snap-a" cannot hold snap "snap-a" anymore, maximum refresh postponement exceeded \(while handling previous hook error: fail\)`)
	c.Assert(change.Status(), Equals, state.DoneStatus)

	// and snap-b is not held (due to hold error).
	var held map[string]map[string]interface{}
	c.Assert(st.Get("snaps-hold", &held), IsNil)
	c.Check(held, HasLen, 0)

	// no runinhibit because the refresh-app-awareness feature is disabled.
	hint, info, err := runinhibit.IsLocked("snap-a")
	c.Assert(err, IsNil)
	c.Check(hint, Equals, runinhibit.HintNotInhibited)
	c.Check(info, Equals, runinhibit.InhibitInfo{})
}
