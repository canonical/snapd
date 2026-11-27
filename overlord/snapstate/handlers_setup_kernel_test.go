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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
)

type setupKernelSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&setupKernelSnapSuite{})

func (s *setupKernelSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)
	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
}

func (s *setupKernelSnapSuite) TestSetupKernelSnap(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	t := s.state.NewTask("prepare-kernel-snap", "test kernel setup")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	var prevKernelRev snap.Revision
	c.Check(t.Get("previous-kernel-rev", &prevKernelRev), IsNil)
	c.Check(prevKernelRev, Equals, snap.R(0))
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "prepare-kernel-snap",
		},
	})
}

func (s *setupKernelSnapSuite) TestSetupKernelSnapTwoTasks(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	ts := s.state.NewTask("nop", "first task")
	ts.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	t := s.state.NewTask("prepare-kernel-snap", "test kernel setup")
	t.Set("snap-setup-task", ts.ID())
	t.WaitFor(ts)
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
	c.Check(t.Status(), Equals, state.DoneStatus)
	var prevKernelRev snap.Revision
	c.Check(ts.Get("previous-kernel-rev", &prevKernelRev), IsNil)
	c.Check(prevKernelRev, Equals, snap.R(0))
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "prepare-kernel-snap",
		},
	})
}

func (s *setupKernelSnapSuite) TestUndoSetupKernelSnap(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	t := s.state.NewTask("prepare-kernel-snap", "test kernel setup")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking undo kernel setup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*provoking undo kernel setup.*`)
	c.Check(t.Status(), Equals, state.UndoneStatus)
	var prevKernelRev snap.Revision
	c.Check(t.Get("previous-kernel-rev", &prevKernelRev), IsNil)
	c.Check(prevKernelRev, Equals, snap.R(0))
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "prepare-kernel-snap",
		},
		{
			op: "remove-kernel-snap-setup",
		},
	})
}

func (s *setupKernelSnapSuite) TestSetupKernelSnapSameRevisionStillRecordsPreviousRev(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
		UserID:  1,
	})

	// same-revision refresh: target revision equals snapst.Current.
	// previous-kernel-rev is still recorded unconditionally; safety is
	// enforced by the consumers (discard-old-kernel-snap-setup and its
	// undo), not by withholding this value. SetupKernelSnap also gets
	// SetupKernelRegenerate here.
	t := s.state.NewTask("prepare-kernel-snap", "test kernel setup")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath:                        testSnap,
		PreUpdateKernelModuleComponents: []*snap.ComponentSideInfo{},
	})
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	var prevKernelRev snap.Revision
	c.Check(t.Get("previous-kernel-rev", &prevKernelRev), IsNil)
	c.Check(prevKernelRev, Equals, snap.R(33))
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:           "prepare-kernel-snap",
			currentComps: []*snap.ComponentSideInfo{},
			regenerate:   true,
		},
	})
}

func (s *setupKernelSnapSuite) TestUndoSetupKernelSnapSameRevisionDoesNotRemove(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
		UserID:  1,
	})

	// same-revision refresh: do's SetupKernelSnap uses SetupKernelRegenerate,
	// not a fresh install, so undo must not remove the live tree.
	t := s.state.NewTask("prepare-kernel-snap", "test kernel setup")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath:                        testSnap,
		PreUpdateKernelModuleComponents: []*snap.ComponentSideInfo{},
	})
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking undo kernel setup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*provoking undo kernel setup.*`)
	c.Check(t.Status(), Equals, state.UndoneStatus)
	s.state.Unlock()

	// no remove-kernel-snap-setup: that would delete the live/mounted tree
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:           "prepare-kernel-snap",
			currentComps: []*snap.ComponentSideInfo{},
			regenerate:   true,
		},
	})
}

func (s *setupKernelSnapSuite) TestSetupKernelSnapSameRevisionRequiresPreUpdateComponents(c *C) {
	s.state.Lock()
	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
	})
	t := s.state.NewTask("prepare-kernel-snap", "test missing component snapshot")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "mykernel", Revision: snap.R(33)},
	})
	chg := s.state.NewChange("refresh", "refresh kernel")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*missing pre-update kernel-modules components.*`)
	s.state.Unlock()
	c.Check(s.fakeBackend.ops, HasLen, 0)
}

func (s *setupKernelSnapSuite) TestSetupKernelSnapPreservesUnlinkedComponentsBeforeKmodSetup(c *C) {
	const name = "mykernel"
	const compName = "kmod"
	rev := snap.R(33)
	oldComp := snap.NewComponentSideInfo(naming.NewComponentRef(naming.SnapName(name), compName), snap.R(7))
	info := createTestSnapInfoForComponents(c, name, rev, map[string]string{compName: "kernel-modules"})
	snaptest.MockSnapInstance(c, name, "name: mykernel\nversion: 1.0\ntype: kernel\ncomponents:\n  kmod:\n    type: kernel-modules\n", &info.SideInfo)

	s.state.Lock()
	setStateWithComponents(s.state, name, rev, []*sequence.ComponentState{
		sequence.NewComponentState(oldComp, snap.KernelModulesComponent),
	})
	setup := s.state.NewTask("nop", "snap setup")
	setup.Set("snap-setup", createTestSnapSetup(info, snapstate.Flags{}))
	unlink := s.state.NewTask("unlink-current-component", "unlink old component")
	unlink.Set("snap-setup-task", setup.ID())
	unlink.Set("component-setup", snapstate.NewComponentSetup(oldComp, snap.KernelModulesComponent, ""))
	unlink.WaitFor(setup)
	prepare := s.state.NewTask("prepare-kernel-snap", "regenerate kernel tree")
	prepare.Set("snap-setup-task", setup.ID())
	prepare.WaitFor(unlink)
	failure := s.state.NewTask("error-trigger", "fail before preparing kernel modules components")
	failure.WaitFor(prepare)
	kmodSetup := s.state.NewTask("prepare-kernel-modules-components", "prepare new components")
	kmodSetup.Set("snap-setup-task", setup.ID())
	kmodSetup.WaitFor(failure)
	chg := s.state.NewChange("refresh", "refresh kernel component")
	for _, task := range []*state.Task{setup, unlink, prepare, failure, kmodSetup} {
		chg.AddTask(task)
	}
	s.state.Unlock()

	for i := 0; i < 10; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*fail before preparing kernel modules components.*`)
	c.Check(prepare.Status(), Equals, state.UndoneStatus)
	c.Check(unlink.Status(), Equals, state.UndoneStatus)
	c.Check(kmodSetup.Status(), Equals, state.HoldStatus)
	var snapsup snapstate.SnapSetup
	c.Assert(setup.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.PreUpdateKernelModuleComponents, DeepEquals, []*snap.ComponentSideInfo{oldComp})
	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, name, &snapst), IsNil)
	c.Check(snapst.CurrentComponentSideInfo(oldComp.Component), DeepEquals, oldComp)
	s.state.Unlock()

	// Undo relinks the old component, but does not regenerate the tree.
	// The earlier regeneration must therefore have used the saved old set.
	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{op: "unlink-component", path: snap.ComponentMountDir(compName, oldComp.Revision, name)},
		{op: "prepare-kernel-snap", currentComps: []*snap.ComponentSideInfo{oldComp}, regenerate: true},
		{op: "link-component", path: snap.ComponentMountDir(compName, oldComp.Revision, name)},
	})
}

func (s *setupKernelSnapSuite) TestRemoveKernelSnapSetup(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
		UserID:  1,
	})
	t := s.state.NewTask("discard-old-kernel-snap-setup", "test discard kernel set-up")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	t.Set("previous-kernel-rev", snap.R(30))
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "remove-kernel-snap-setup",
		},
	})
}

func (s *setupKernelSnapSuite) TestUndoRemoveKernelSnapSetup(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
		UserID:  1,
	})
	t := s.state.NewTask("discard-old-kernel-snap-setup", "test discard setup")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	t.Set("previous-kernel-rev", snap.R(30))
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking undo kernel cleanup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*provoking undo kernel cleanup.*`)
	c.Check(t.Status(), Equals, state.UndoneStatus)
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "remove-kernel-snap-setup",
		},
		{
			op: "prepare-kernel-snap",
		},
	})
}

func (s *setupKernelSnapSuite) TestDiscardOldKernelSnapSetupSameRevisionDoesNotRemove(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
		UserID:  1,
	})
	t := s.state.NewTask("discard-old-kernel-snap-setup", "test discard kernel set-up")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	// defensive guard: previous-kernel-rev set equal to the current
	// revision (should never happen upstream, but must never remove)
	t.Set("previous-kernel-rev", snap.R(33))
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Check(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, HasLen, 0)
}

func (s *setupKernelSnapSuite) TestUndoDiscardOldKernelSnapSetupSameRevisionDoesNotRecreate(c *C) {
	v1 := "name: mykernel\nversion: 1.0\ntype: kernel\n"
	testSnap := snaptest.MakeTestSnapWithFiles(c, v1, nil)

	s.state.Lock()

	snapstate.Set(s.state, "mykernel", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "mykernel", Revision: snap.R(33)},
		}),
		Current: snap.R(33),
		UserID:  1,
	})
	t := s.state.NewTask("discard-old-kernel-snap-setup", "test discard kernel set-up")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "mykernel",
			Revision: snap.R(33),
		},
		SnapPath: testSnap,
	})
	// same-revision case: previous-kernel-rev equals the current revision,
	// so do's RemoveKernelSnapSetup was a no-op; undo must not recreate it
	// either (symmetry).
	t.Set("previous-kernel-rev", snap.R(33))
	chg := s.state.NewChange("test change", "change desc")
	chg.AddTask(t)
	terr := s.state.NewTask("error-trigger", "provoking undo discard kernel setup")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	c.Check(chg.Err(), ErrorMatches, `(?s).*provoking undo discard kernel setup.*`)
	c.Check(t.Status(), Equals, state.UndoneStatus)
	s.state.Unlock()

	c.Check(s.fakeBackend.ops, HasLen, 0)
}
