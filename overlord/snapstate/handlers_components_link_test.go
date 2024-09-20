// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package snapstate_test

import (
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

type linkCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&linkCompSnapSuite{})

var taskRunTime time.Time

func (s *linkCompSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))

	var err error
	taskRunTime, err = time.Parse(time.RFC3339, "2024-01-01T00:00:00Z")
	c.Assert(err, IsNil)
	s.AddCleanup(snapstate.MockTimeNow(func() time.Time {
		return taskRunTime
	}))
}

func (s *linkCompSnapSuite) testDoLinkComponent(c *C, snapName string, snapRev snap.Revision, kmodComps []*snap.ComponentSideInfo) {
	const compName = "mycomp"
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask("link-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(chg.Err(), IsNil)
	// undo information has been stored
	var storedCs sequence.ComponentState
	cs := sequence.NewComponentState(csi, snap.TestComponent)
	t.Get("linked-component", &storedCs)
	c.Assert(&storedCs, DeepEquals, cs)
	// the link has been created
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "link-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
	})
	// state is modified as expected
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	stateCsi := snapst.CurrentComponentSideInfo(cref)
	c.Assert(stateCsi, DeepEquals, csi)
	c.Assert(snapst.LastCompRefreshTime[csi.Component.ComponentName], Equals, taskRunTime)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	var snapsup snapstate.SnapSetup
	err := t.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Assert(snapsup.PreUpdateKernelModuleComponents, DeepEquals, kmodComps)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoLinkComponent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state does not contain the component
	setStateWithOneSnap(s.state, snapName, snapRev)
	s.state.Unlock()

	s.testDoLinkComponent(c, snapName, snapRev, []*snap.ComponentSideInfo{})
}

func (s *linkCompSnapSuite) TestDoLinkComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()

	kmodCsi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "kmod-comp"), snap.R(10))
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	// state with some component around already
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{
		sequence.NewComponentState(csi, snap.TestComponent),
		sequence.NewComponentState(kmodCsi, snap.KernelModulesComponent),
	})

	s.state.Unlock()

	s.testDoLinkComponent(c, snapName, snapRev, []*snap.ComponentSideInfo{kmodCsi})
}

func (s *linkCompSnapSuite) testDoLinkThenUndoLinkComponent(c *C, snapName string, snapRev snap.Revision) {
	const compName = "mycomp"
	compRev := snap.R(7)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	// state does not contain the component
	setStateWithOneSnap(s.state, snapName, snapRev)

	t := s.state.NewTask("link-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo link")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo link (error out)")
	// undo information was stored
	var storedCs sequence.ComponentState
	cs := sequence.NewComponentState(csi, snap.TestComponent)
	t.Get("linked-component", &storedCs)
	c.Assert(&storedCs, DeepEquals, cs)
	// the link has been created and then removed
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "link-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
		{
			op: "unlink-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
	})
	// the component is not in the state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	c.Assert(snapst.CurrentComponentSideInfo(cref), IsNil)
	c.Assert(snapst.LastCompRefreshTime[csi.Component.ComponentName], Equals, taskRunTime)
	c.Assert(t.Status(), Equals, state.UndoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoLinkThenUndoLinkComponent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state does not contain the component
	setStateWithOneSnap(s.state, snapName, snapRev)
	s.state.Unlock()

	s.testDoLinkThenUndoLinkComponent(c, snapName, snapRev)
}

func (s *linkCompSnapSuite) TestDoLinkThenUndoLinkComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	snapRev := snap.R(1)

	s.state.Lock()
	// state with some component around already
	setStateWithOneComponent(s.state, snapName, snapRev, "other-comp", snap.R(33))
	s.state.Unlock()

	s.testDoLinkThenUndoLinkComponent(c, snapName, snapRev)
}

func (s *linkCompSnapSuite) testDoUnlinkComponent(c *C, snapName string, snapRev snap.Revision, compName string, compRev snap.Revision, unlinkTaskType string, kmodComps []*snap.ComponentSideInfo) {
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask(unlinkTaskType, "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	c.Check(chg.Err(), IsNil)
	// undo information has been stored
	var unlinkedComp sequence.ComponentState
	c.Assert(t.Get("unlinked-component", &unlinkedComp), IsNil)
	c.Assert(&unlinkedComp, DeepEquals,
		sequence.NewComponentState(csi, snap.TestComponent))
	// the link has been removed
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "unlink-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
	})
	// state is modified as expected
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	c.Assert(snapst.CurrentComponentSideInfo(cref), IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	var snapsup snapstate.SnapSetup
	err := t.Get("snap-setup", &snapsup)
	c.Assert(err, IsNil)

	c.Assert(snapsup.PreUpdateKernelModuleComponents, DeepEquals, kmodComps)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoUnlinkCurrentComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)
	s.state.Unlock()

	s.testDoUnlinkComponent(c, snapName, snapRev, compName, compRev, "unlink-current-component", []*snap.ComponentSideInfo{})
}

func (s *linkCompSnapSuite) TestDoUnlinkCurrentComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component, we add some additional components as well
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.TestComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})
	s.state.Unlock()

	s.testDoUnlinkComponent(c, snapName, snapRev, compName, compRev, "unlink-current-component", []*snap.ComponentSideInfo{cs2.SideInfo})
}

func (s *linkCompSnapSuite) TestDoUnlinkCurrentComponentTwoTasks(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)
	s.state.Unlock()

	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	ts := s.state.NewTask("nop", "first task")
	t := s.state.NewTask("unlink-current-component", "task desc")
	t.WaitFor(ts)
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	ts.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	ts.Set("snap-setup", ssu)
	t.Set("snap-setup-task", ts.ID())
	t.Set("component-setup-task", ts.ID())
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(ts)
	chg.AddTask(t)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err(), IsNil)
	// undo information has been stored in the setup task
	var unlinkedComp sequence.ComponentState
	c.Assert(ts.Get("unlinked-component", &unlinkedComp), IsNil)
	c.Assert(&unlinkedComp, DeepEquals,
		sequence.NewComponentState(csi, snap.TestComponent))
	// the link has been removed
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "unlink-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
	})
	// state is modified as expected
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	c.Assert(snapst.CurrentComponentSideInfo(cref), IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) testDoUnlinkThenUndoUnlinkComponent(c *C, snapName string, snapRev snap.Revision, compName string, compRev snap.Revision, unlinkTaskType string) {
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask(unlinkTaskType, "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, ""))
	t.Set("snap-setup", ssu)
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking undo link")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()

	c.Check(chg.Err().Error(), Equals, "cannot perform the following tasks:\n"+
		"- provoking undo link (error out)")
	// undo information was stored
	var unlinkedComp sequence.ComponentState
	c.Assert(t.Get("unlinked-component", &unlinkedComp), IsNil)
	c.Assert(&unlinkedComp, DeepEquals,
		sequence.NewComponentState(csi, snap.TestComponent))
	// the link has been removed and then re-created
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "unlink-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
		{
			op: "link-component",
			path: filepath.Join(
				dirs.SnapMountDir, snapName, "components",
				"mnt", compName, compRev.String()),
		},
	})
	// the component is still in the state
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)
	stateCsi := snapst.CurrentComponentSideInfo(cref)
	c.Assert(stateCsi, DeepEquals, csi)
	c.Assert(t.Status(), Equals, state.UndoneStatus)

	s.state.Unlock()
}

func (s *linkCompSnapSuite) TestDoUnlinkThenUndoUnlinkCurrentComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component
	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)
	s.state.Unlock()

	s.testDoUnlinkThenUndoUnlinkComponent(c, snapName, snapRev,
		compName, compRev, "unlink-current-component")
}

func (s *linkCompSnapSuite) TestDoUnlinkThenUndoUnlinkCurrentComponentOtherCompPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	// state must contain the component, we add some additional components as well
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(33))
	setStateWithComponents(s.state, snapName, snapRev,
		[]*sequence.ComponentState{sequence.NewComponentState(csi1, snap.TestComponent), sequence.NewComponentState(csi2, snap.TestComponent)})
	s.state.Unlock()

	s.testDoUnlinkThenUndoUnlinkComponent(c, snapName, snapRev, compName, compRev, "unlink-current-component")
}

func (s *linkCompSnapSuite) TestDoUnlinkComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()

	// State must contain the component. Note that in this case
	// the snap does not need to be active.
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi, snap.TestComponent),
	}
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, comps)}),
		Current: snapRev,
	})

	s.state.Unlock()

	s.testDoUnlinkComponent(c, snapName, snapRev, compName, compRev, "unlink-component", nil)
}

func (s *linkCompSnapSuite) TestDoUnlinkThenUndoUnlinkComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()

	// State must contain the component. Note that in this case
	// the snap does not need to be active.
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), compRev)
	comps := []*sequence.ComponentState{sequence.NewComponentState(csi, snap.TestComponent)}
	snapstate.Set(s.state, snapName, &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, comps)}),
		Current: snapRev,
	})

	s.state.Unlock()

	s.testDoUnlinkThenUndoUnlinkComponent(c, snapName, snapRev,
		compName, compRev, "unlink-component")
}
