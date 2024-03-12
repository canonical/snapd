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

func (s *rebootSuite) taskSetForSnapSetup(snapName, base string, snapType snap.Type) *state.TaskSet {
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			SnapID:   snapName,
			Revision: snap.R(1),
		},
		Type: snapType,
		Base: base,
	}
	t1 := s.state.NewTask("snap-task", "...")
	t1.Set("snap-setup", snapsup)
	t2 := s.state.NewTask("unlink-snap", "...")
	t2.WaitFor(t1)
	t3 := s.state.NewTask("link-snap", "...")
	t3.WaitFor(t2)
	t4 := s.state.NewTask("auto-connect", "...")
	t4.WaitFor(t3)
	ts := state.NewTaskSet(t1, t2, t3, t4)
	// 4 required edges
	ts.MarkEdge(t1, snapstate.BeginEdge)
	ts.MarkEdge(t3, snapstate.MaybeRebootEdge)
	ts.MarkEdge(t4, snapstate.MaybeRebootWaitEdge)
	ts.MarkEdge(t4, snapstate.EndEdge)
	// Assign each TS a lane
	ts.JoinLane(s.state.NewLane())
	return ts
}

func (s *rebootSuite) taskSetForSnapSetupButNoTasks(snapName string, snapType snap.Type) *state.TaskSet {
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
	ts := state.NewTaskSet(t1)
	return ts
}

func (s *rebootSuite) TestTaskSetsByTypeForEssentialSnapsNoBootBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("my-base", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("my-os", "", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
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
		s.taskSetForSnapSetup("my-base", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("my-os", "", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
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

func (s *rebootSuite) hasDoRestartBoundaries(c *C, ts *state.TaskSet) bool {
	t := ts.MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(t, NotNil)

	var boundary restart.RestartBoundaryDirection
	if err := t.Get("restart-boundary", &boundary); err != nil {
		return false
	}
	return true
}

func (s *rebootSuite) hasUndoRestartBoundaries(c *C, ts *state.TaskSet) bool {
	t := s.findUnlinkTask(ts)
	c.Assert(t, NotNil)

	var boundary restart.RestartBoundaryDirection
	if err := t.Get("restart-boundary", &boundary); err != nil {
		return false
	}
	return true
}

func (s *rebootSuite) TestSetEssentialSnapsRestartBoundariesUC16(c *C) {
	defer snapstatetest.MockDeviceModel(DefaultModel())()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("core", "", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
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
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("core", "", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.SetEssentialSnapsRestartBoundaries(s.state, nil, tss)
	c.Assert(err, IsNil)
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[3]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[4]), Equals, false)
}

func (s *rebootSuite) TestSplitTaskSetByRebootEdgesHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t1 := s.state.NewTask("first", "...")
	t2 := s.state.NewTask("second", "...")
	t2.WaitFor(t1)
	t3 := s.state.NewTask("third", "...")
	t3.WaitFor(t2)
	t4 := s.state.NewTask("fourth", "...")
	t4.WaitFor(t3)
	t5 := s.state.NewTask("fifth", "...")
	t5.WaitFor(t4)
	ts := state.NewTaskSet(t1, t2, t3, t4, t5)

	// 4 required edges
	ts.MarkEdge(t1, snapstate.BeginEdge)
	ts.MarkEdge(t3, snapstate.MaybeRebootEdge)
	ts.MarkEdge(t4, snapstate.MaybeRebootWaitEdge)
	ts.MarkEdge(t5, snapstate.EndEdge)

	// Split it into two task-sets with new edges
	before, after, err := snapstate.SplitTaskSetByRebootEdges(ts)
	c.Check(err, IsNil)
	c.Check(before, NotNil)
	c.Check(after, NotNil)

	// verify the new task-sets have edges set
	c.Check(before.MaybeEdge(snapstate.BeginEdge), Equals, t1)
	c.Check(before.MaybeEdge(snapstate.EndEdge), Equals, t3)

	c.Check(after.MaybeEdge(snapstate.BeginEdge), Equals, t4)
	c.Check(after.MaybeEdge(snapstate.EndEdge), Equals, t5)

	// verify that before and after consists of expected tasks
	c.Check(before.Tasks(), HasLen, 3)
	c.Check(after.Tasks(), HasLen, 2)
}

func (s *rebootSuite) TestSplitTaskSetByRebootEdgesMissingEdges(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	t := s.state.NewTask("first", "...")
	ts := state.NewTaskSet(t)

	// Test without any edges
	before, after, err := snapstate.SplitTaskSetByRebootEdges(ts)
	c.Check(err, ErrorMatches, `internal error: task-set is missing required edges \("begin"/"end"\)`)
	c.Check(before, IsNil)
	c.Check(after, IsNil)

	// Set begin, end
	ts.MarkEdge(t, snapstate.BeginEdge)
	ts.MarkEdge(t, snapstate.EndEdge)

	before, after, err = snapstate.SplitTaskSetByRebootEdges(ts)
	c.Check(err, ErrorMatches, `internal error: task-set is missing required edge "maybe-reboot"`)
	c.Check(before, IsNil)
	c.Check(after, IsNil)

	// set MaybeRebootEdge
	ts.MarkEdge(t, snapstate.MaybeRebootEdge)

	before, after, err = snapstate.SplitTaskSetByRebootEdges(ts)
	c.Check(err, ErrorMatches, `internal error: task-set is missing required edge "maybe-reboot-wait"`)
	c.Check(before, IsNil)
	c.Check(after, IsNil)
}

func (s *rebootSuite) setDependsOn(c *C, ts, dep *state.TaskSet) bool {
	firstTaskOfTs, err := ts.Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	lastTaskOfDep, err := dep.Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	for _, wt := range firstTaskOfTs.WaitTasks() {
		if wt == lastTaskOfDep {
			return true
		}
	}
	return false
}

func (s *rebootSuite) TestArrangeSnapToWaitForBaseIfPresentHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("my-base", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-app", "my-base", snap.TypeApp),
	}

	err := snapstate.ArrangeSnapToWaitForBaseIfPresent(tss[1], map[string]*state.TaskSet{
		"my-base": tss[0],
	})
	c.Check(err, IsNil)
	c.Check(s.setDependsOn(c, tss[1], tss[0]), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapToWaitForBaseIfPresentNotPresent(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("my-base", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-app", "my-other-base", snap.TypeApp),
	}

	err := snapstate.ArrangeSnapToWaitForBaseIfPresent(tss[1], map[string]*state.TaskSet{
		"my-base": tss[0],
	})
	c.Check(err, IsNil)
	c.Check(s.setDependsOn(c, tss[1], tss[0]), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartUC16NoSplits(c *C) {
	defer snapstatetest.MockDeviceModel(DefaultModel())()

	s.state.Lock()
	defer s.state.Unlock()

	// Run without gadget, as that will make it also non-split currently
	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core", "", snap.TypeOS),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// core, kernel should have individual restart boundaries
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[3]), Equals, false)

	// core and kernel are not transactional on UC16
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartSnapdAndEssential(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("snapd", "", snap.TypeSnapd),
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Snapd should have no restart boundaries
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, false)

	// boot-base, gadget, kernel setup for single-reboot
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[1]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[2]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[2]), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, tss[3]), Equals, true)
	c.Check(s.hasUndoRestartBoundaries(c, tss[3]), Equals, false)

	// TypeApp should have no boundaries
	c.Check(s.hasRestartBoundaries(c, tss[4]), Equals, false)

	// base, gadget and kernel are transactional
	c.Check(taskSetsShareLane(tss[1], tss[2], tss[3]), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartBaseKernel(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Expect restart boundaries on both
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, true)

	linkSnapBase := tss[0].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapBase, NotNil)
	linkSnapKernel := tss[1].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapKernel, NotNil)

	// linking between the base and kernel is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range tss[0].Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, tss[0].Tasks()[i-1].ID())
		}
		if t == linkSnapBase {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel, err := tss[1].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := tss[1].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := tss[1].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfBase, err := tss[0].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := tss[0].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := tss[0].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of base (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// both should be transactional
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, true)

	// Since they are set up for single-reboot, the base should have restart
	// boundaries for the undo path, and kernel should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[0]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartBaseGadget(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Expect restart boundaries on both
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, true)

	linkSnapBase := tss[0].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapBase, NotNil)
	linkSnapGadget := tss[1].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapGadget, NotNil)

	// linking between the base and gadget is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range tss[0].Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, tss[0].Tasks()[i-1].ID())
		}
		if t == linkSnapBase {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	firstTaskOfGadget, err := tss[1].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := tss[1].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := tss[1].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfBase, err := tss[0].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := tss[0].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := tss[0].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - "prerequisites" (BeginEdge) of gadget must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of gadget (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on the last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// both should be transactional
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, true)

	// Since they are set up for single-reboot, the base should have restart
	// boundaries for the undo path, and gadget should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[0]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartGadgetKernel(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Expect restart boundaries on both
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, true)

	linkSnapGadget := tss[0].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapGadget, NotNil)
	linkSnapKernel := tss[1].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapKernel, NotNil)

	// linking between the gadget and kernel is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range tss[0].Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, tss[0].Tasks()[i-1].ID())
		}
		if t == linkSnapGadget {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel, err := tss[1].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := tss[1].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := tss[1].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := tss[0].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := tss[0].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfGadget, err := tss[0].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of gadget
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of gadget (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfGadget)

	// both should be transactional
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, true)

	// Since they are set up for single-reboot, the gadget should have restart
	// boundaries for the undo path, and kernel should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[0]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartBaseGadgetKernel(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	linkSnapBase := tss[0].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapBase, NotNil)
	linkSnapKernel := tss[1].MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapKernel, NotNil)

	// linking between the base, gadget and kernel is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range tss[0].Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, tss[0].Tasks()[i-1].ID())
		}
		if t == linkSnapBase {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	linkTaskOfBase, err := tss[0].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := tss[0].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := tss[0].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfGadget, err := tss[1].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := tss[1].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := tss[1].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfGadget, err := tss[1].Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfKernel, err := tss[2].Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := tss[2].Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := tss[2].Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)

	// Things that must be correct between base and gadget:
	// - "prerequisites" (BeginEdge) of gadget must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on the last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Things that must be correct between gadget and kernel:
	// - "prerequisites" (BeginEdge) of kernel must depend on "link-snap" (MaybeRebootEdge) of gadget
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of gadget (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfGadget)

	// all three should be transactional
	c.Check(taskSetsShareLane(tss[0], tss[1], tss[2]), Equals, true)

	// Since they are set up for single-reboot, the base should have restart
	// boundaries for the undo path, and kernel should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[0]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[2]), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, tss[2]), Equals, true)

	// Gadget should have no boundaries
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartSnapd(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("snapd", "", snap.TypeSnapd),
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Do not expect any restart boundaries to be set on snapd
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, false)

	// Expect them to be set on core20 as it's the boot-base
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, true)

	// Snapd should never be a part of the single-reboot transaction, we don't
	// need snapd to rollback if an issue should arise in any of the other essential snaps.
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartBootBaseAndOtherBases(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("core18", "", snap.TypeBase),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Only the boot-base should have restart boundary.
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, false)

	// boot-base is transactional, but not with the other base and my-app
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, false)
	c.Check(taskSetsShareLane(tss[0], tss[2]), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageForSnapWithBaseAndWithout(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("snap-base", "", snap.TypeBase),
		s.taskSetForSnapSetup("snap-base-app", "snap-base", snap.TypeApp),
		s.taskSetForSnapSetup("snap-other-app", "other-base", snap.TypeApp),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// No restart boundaries
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, false)

	// no transactional lane set
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, false)
	c.Check(taskSetsShareLane(tss[0], tss[2]), Equals, false)

	// snap-base-app depends on snap-base, but snap-other-app's base
	// is not updated
	c.Check(s.setDependsOn(c, tss[1], tss[0]), Equals, true)
	c.Check(s.setDependsOn(c, tss[2], tss[0]), Equals, false)
	c.Check(s.setDependsOn(c, tss[2], tss[1]), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageForSnapWithBootBaseAndWithout(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("snap-core20-app", "snap-core20", snap.TypeApp),
		s.taskSetForSnapSetup("snap-other-app", "other-base", snap.TypeApp),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// Restart boundaries is set for core20 as the boot-base
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, true)
	c.Check(s.hasRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[2]), Equals, false)

	// Core20 is transactional, but not with the other base and app
	c.Check(taskSetsShareLane(tss[0], tss[1]), Equals, false)
	c.Check(taskSetsShareLane(tss[0], tss[2]), Equals, false)

	// snap-core20-app depends on core20, but snap-other-app' base is
	// not updated. Yet snap-other-base still depends on core20. But there
	// is no dependency between snap-core20-app and snap-other-app
	c.Check(s.setDependsOn(c, tss[1], tss[0]), Equals, true)  // snap-core20-app depend on core20
	c.Check(s.setDependsOn(c, tss[2], tss[0]), Equals, true)  // snap-other-app depend on core20
	c.Check(s.setDependsOn(c, tss[2], tss[1]), Equals, false) // snap-other-app does not depend on snap-core20-app
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartAll(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetup("snapd", "", snap.TypeSnapd),
		s.taskSetForSnapSetup("core20", "", snap.TypeBase),
		s.taskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.taskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.taskSetForSnapSetup("core", "", snap.TypeOS),
		s.taskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, IsNil)

	// snapd has no restart boundaries set
	c.Check(s.hasRestartBoundaries(c, tss[0]), Equals, false)

	// boot-base, gadget, kernel setup for single-reboot
	c.Check(s.hasDoRestartBoundaries(c, tss[1]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[1]), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, tss[2]), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, tss[2]), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, tss[3]), Equals, true)
	c.Check(s.hasUndoRestartBoundaries(c, tss[3]), Equals, false)

	// TypeOS (in this scenario) and TypeApp should have no restart boundaries
	c.Check(s.hasRestartBoundaries(c, tss[4]), Equals, false)
	c.Check(s.hasRestartBoundaries(c, tss[5]), Equals, false)

	// boot-base, gadget and kernel are transactional
	c.Check(taskSetsShareLane(tss[1], tss[2], tss[3]), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapTaskSetsLinkageAndRestartFailsSplit(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	tss := []*state.TaskSet{
		s.taskSetForSnapSetupButNoTasks("my-kernel", snap.TypeKernel),
	}
	err := snapstate.ArrangeSnapTaskSetsLinkageAndRestart(s.state, nil, tss)
	c.Assert(err, ErrorMatches, `internal error: no \"maybe-reboot\" edge set in task-set`)
}
