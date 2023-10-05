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

package snapstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type rebootSuite struct {
	testutil.BaseTest
	state *state.State
}

var _ = Suite(&rebootSuite{})

func (s *rebootSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.state = state.New(nil)
}

func (s *rebootSuite) taskSetForSnapSetup(snapName string, snapType snap.Type) *state.TaskSet {
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			SnapID:   snapName,
			Revision: snap.R(1),
		},
		Type: snapType,
	}
	t1 := s.state.NewTask("snap-task", "...")
	t1.Set("snap-setup", snapsup)
	t2 := s.state.NewTask("link-snap", "...")
	t3 := s.state.NewTask("unlink-snap", "...")
	ts := state.NewTaskSet(t1, t2, t3)
	ts.MarkEdge(t2, snapstate.MaybeRebootEdge)
	return ts
}

func (s *rebootSuite) TestTaskSetsByTypeForEssentialSnapsNoBootBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("my-base", snap.TypeBase),
		s.taskSetForSnapSetup("my-gadget", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", snap.TypeKernel),
		s.taskSetForSnapSetup("my-os", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", snap.TypeApp),
	}

	mappedTaskSets, err := snapstate.TaskSetsByTypeForEssentialSnaps(tss, "")
	c.Assert(err, IsNil)
	c.Check(mappedTaskSets, DeepEquals, map[snap.Type]*state.TaskSet{
		snap.TypeGadget: tss[1],
		snap.TypeKernel: tss[2],
		snap.TypeOS:     tss[3],
	})
}

func (s *rebootSuite) TestTaskSetsByTypeForEssentialSnapsBootBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("my-base", snap.TypeBase),
		s.taskSetForSnapSetup("my-gadget", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", snap.TypeKernel),
		s.taskSetForSnapSetup("my-os", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", snap.TypeApp),
	}

	mappedTaskSets, err := snapstate.TaskSetsByTypeForEssentialSnaps(tss, "my-base")
	c.Assert(err, IsNil)
	c.Check(mappedTaskSets, DeepEquals, map[snap.Type]*state.TaskSet{
		snap.TypeBase:   tss[0],
		snap.TypeGadget: tss[1],
		snap.TypeKernel: tss[2],
		snap.TypeOS:     tss[3],
	})
}

func (s *rebootSuite) TestSetDefaultRestartBoundariesLink(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t1 := s.state.NewTask("first-thing", "...")
	t2 := s.state.NewTask("other-thing", "...")
	ts := state.NewTaskSet(t1, t2)
	ts.MarkEdge(t1, snapstate.MaybeRebootEdge)

	snapstate.SetDefaultRestartBoundaries(ts)

	var boundary restart.RestartBoundaryDirection
	c.Check(t1.Get("restart-boundary", &boundary), IsNil)
	c.Check(t2.Get("restart-boundary", &boundary), ErrorMatches, `no state entry for key "restart-boundary"`)
}

func (s *rebootSuite) TestSetDefaultRestartBoundariesUnlink(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t1 := s.state.NewTask("unlink-snap", "...")
	t2 := s.state.NewTask("other-thing", "...")
	ts := state.NewTaskSet(t1, t2)

	snapstate.SetDefaultRestartBoundaries(ts)

	var boundary restart.RestartBoundaryDirection
	c.Check(t1.Get("restart-boundary", &boundary), IsNil)
	c.Check(t2.Get("restart-boundary", &boundary), ErrorMatches, `no state entry for key "restart-boundary"`)
}

func (s *rebootSuite) TestSetDefaultRestartBoundariesUnlinkCurrent(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t1 := s.state.NewTask("unlink-current-snap", "...")
	t2 := s.state.NewTask("other-thing", "...")
	ts := state.NewTaskSet(t1, t2)

	snapstate.SetDefaultRestartBoundaries(ts)

	var boundary restart.RestartBoundaryDirection
	c.Check(t1.Get("restart-boundary", &boundary), IsNil)
	c.Check(t2.Get("restart-boundary", &boundary), ErrorMatches, `no state entry for key "restart-boundary"`)
}

func (s *rebootSuite) TestDeviceModelBootBaseEmpty(c *C) {
	defer snapstatetest.MockDeviceModel(nil)()
	bootBase, err := snapstate.DeviceModelBootBase(s.state, nil)
	c.Check(err, IsNil)
	c.Check(bootBase, Equals, "")
}

func (s *rebootSuite) TestDeviceModelBootBaseUC16(c *C) {
	defer snapstatetest.MockDeviceModel(DefaultModel())()
	bootBase, err := snapstate.DeviceModelBootBase(s.state, nil)
	c.Check(err, IsNil)
	c.Check(bootBase, Equals, "core")
}

func (s *rebootSuite) TestDeviceModelBootBaseUC(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()
	bootBase, err := snapstate.DeviceModelBootBase(s.state, nil)
	c.Check(err, IsNil)
	c.Check(bootBase, Equals, "core20")
}

func (s *rebootSuite) TestDeviceModelBootBaseClassic(c *C) {
	defer snapstatetest.MockDeviceModel(ClassicModel())()
	bootBase, err := snapstate.DeviceModelBootBase(s.state, nil)
	c.Check(err, IsNil)
	c.Check(bootBase, Equals, "core")
}

func (s *rebootSuite) TestDeviceModelBootBaseClassicModelProvided(c *C) {
	// Mock the model to classic
	defer snapstatetest.MockDeviceModel(ClassicModel())()

	// then provide a UC18 model instead
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: ModelWithBase("core18")}
	bootBase, err := snapstate.DeviceModelBootBase(s.state, deviceCtx)
	c.Check(err, IsNil)
	c.Check(bootBase, Equals, "core18")
}

func (s *rebootSuite) findUnlinkTask(ts *state.TaskSet) *state.Task {
	for _, t := range ts.Tasks() {
		switch t.Kind() {
		case "unlink-snap", "unlink-current-snap":
			return t
		}
	}
	return nil
}

func (s *rebootSuite) hasRestartBoundaries(c *C, ts *state.TaskSet) bool {
	t1 := ts.MaybeEdge(snapstate.MaybeRebootEdge)
	t2 := s.findUnlinkTask(ts)
	c.Assert(t1, NotNil)
	c.Assert(t2, NotNil)

	var boundary restart.RestartBoundaryDirection
	if err := t1.Get("restart-boundary", &boundary); err != nil {
		return false
	}
	if err := t2.Get("restart-boundary", &boundary); err != nil {
		return false
	}
	return true
}

func (s *rebootSuite) TestSetEssentialSnapsRestartBoundariesUC16(c *C) {
	defer snapstatetest.MockDeviceModel(DefaultModel())()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", snap.TypeBase),
		s.taskSetForSnapSetup("my-gadget", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", snap.TypeKernel),
		s.taskSetForSnapSetup("core", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", snap.TypeApp),
	}
	err := snapstate.SetEssentialSnapsRestartBoundaries(s.state, nil, tss)
	c.Assert(err, IsNil)
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[3]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[4]), Equals, false)
}

func (s *rebootSuite) TestSetEssentialSnapsRestartBoundariesUC20(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", snap.TypeBase),
		s.taskSetForSnapSetup("brand-gadget", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", snap.TypeKernel),
		s.taskSetForSnapSetup("core", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", snap.TypeApp),
	}
	err := snapstate.SetEssentialSnapsRestartBoundaries(s.state, nil, tss)
	c.Assert(err, IsNil)
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[3]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[4]), Equals, false)
}
