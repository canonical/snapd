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
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
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
