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

func (s *rebootSuite) snapInstallTaskSetForSnapSetup(snapName, base string, snapType snap.Type) snapstate.SnapInstallTaskSet {
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: snapName,
			SnapID:   snapName,
			Revision: snap.R(1),
		},
		Type: snapType,
		Base: base,
	}
	prereq := s.state.NewTask("prerequisites", "...")
	prereq.Set("snap-setup", snapsup)
	prepareSnap := s.state.NewTask("prepare-snap", "...")
	prepareSnap.WaitFor(prereq)
	unlinkSnap := s.state.NewTask("unlink-snap", "...")
	unlinkSnap.WaitFor(prepareSnap)
	linkSnap := s.state.NewTask("link-snap", "...")
	linkSnap.WaitFor(unlinkSnap)
	autoConnect := s.state.NewTask("auto-connect", "...")
	autoConnect.WaitFor(linkSnap)
	startServices := s.state.NewTask("start-snap-services", "...")
	startServices.WaitFor(autoConnect)
	ts := state.NewTaskSet(prereq, prepareSnap, unlinkSnap, linkSnap, autoConnect, startServices)

	ts.MarkEdge(prereq, snapstate.BeginEdge)
	ts.MarkEdge(prepareSnap, snapstate.LastBeforeLocalModificationsEdge)
	ts.MarkEdge(linkSnap, snapstate.MaybeRebootEdge)
	ts.MarkEdge(autoConnect, snapstate.MaybeRebootWaitEdge)
	ts.MarkEdge(startServices, snapstate.EndEdge)

	ts.JoinLane(s.state.NewLane())

	return snapstate.NewSnapInstallTaskSetForTest(
		snapsup,
		ts,
		[]*state.Task{prereq, prepareSnap},  // before local modification tasks
		[]*state.Task{unlinkSnap, linkSnap}, // modification inducing tasks before reboot
		[]*state.Task{autoConnect, startServices}, // post reboot tasks
	)
}

func taskSetsFromInstallSets(stss []snapstate.SnapInstallTaskSet) []*state.TaskSet {
	tss := make([]*state.TaskSet, 0, len(stss))
	for _, sts := range stss {
		tss = append(tss, sts.TaskSet())
	}
	return tss
}

func (s *rebootSuite) TestTaskSetsByTypeForEssentialSnapsNoBootBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("my-base", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("my-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("my-os", "", snap.TypeOS),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	mappedTaskSets, err := snapstate.TaskSetsByTypeForEssentialSnaps(taskSetsFromInstallSets(stss), "")
	c.Assert(err, IsNil)
	c.Check(mappedTaskSets, DeepEquals, map[snap.Type]*state.TaskSet{
		snap.TypeGadget: stss[1].TaskSet(),
		snap.TypeKernel: stss[2].TaskSet(),
	})
}

func (s *rebootSuite) TestTaskSetsByTypeForEssentialSnapsBootBase(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("my-base", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("my-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("my-os", "", snap.TypeOS),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	mappedTaskSets, err := snapstate.TaskSetsByTypeForEssentialSnaps(taskSetsFromInstallSets(stss), "my-base")
	c.Assert(err, IsNil)
	c.Check(mappedTaskSets, DeepEquals, map[snap.Type]*state.TaskSet{
		snap.TypeBase:   stss[0].TaskSet(),
		snap.TypeGadget: stss[1].TaskSet(),
		snap.TypeKernel: stss[2].TaskSet(),
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

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("my-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("core", "", snap.TypeOS),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.SetEssentialSnapsRestartBoundaries(s.state, nil, taskSetsFromInstallSets(stss))
	c.Assert(err, IsNil)
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[2].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[3].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[4].TaskSet()), Equals, false)
}

func (s *rebootSuite) TestSetEssentialSnapsRestartBoundariesUC20(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("core", "", snap.TypeOS),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.SetEssentialSnapsRestartBoundaries(s.state, nil, taskSetsFromInstallSets(stss))
	c.Assert(err, IsNil)
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[2].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[3].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[4].TaskSet()), Equals, false)
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

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsUC16NoSplits(c *C) {
	defer snapstatetest.MockDeviceModel(DefaultModel())()

	s.state.Lock()
	defer s.state.Unlock()

	// Run without gadget, as that will make it also non-split currently
	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core", "", snap.TypeOS),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// core, kernel should have individual restart boundaries
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[2].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[3].TaskSet()), Equals, false)

	// core and kernel are not transactional on UC16
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsSnapdAndEssential(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("snapd", "", snap.TypeSnapd),
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Snapd should have no restart boundaries
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, false)

	// boot-base, gadget, kernel setup for single-reboot
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[2].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[2].TaskSet()), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, stss[3].TaskSet()), Equals, true)
	c.Check(s.hasUndoRestartBoundaries(c, stss[3].TaskSet()), Equals, false)

	// TypeApp should have no boundaries
	c.Check(s.hasRestartBoundaries(c, stss[4].TaskSet()), Equals, false)

	// snapd is refreshed in this change. thus, essential snaps should not start
	// prerequisites/download before snapd fully finishes.
	snapdEndTask, err := stss[0].TaskSet().Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	for _, idx := range []int{1, 2, 3} {
		beginTask, err := stss[idx].TaskSet().Edge(snapstate.BeginEdge)
		c.Assert(err, IsNil)
		c.Check(beginTask.WaitTasks(), testutil.Contains, snapdEndTask)
	}

	// base, gadget and kernel are transactional
	c.Check(taskSetsShareLane(stss[1].TaskSet(), stss[2].TaskSet(), stss[3].TaskSet()), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsBaseKernel(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Expect restart boundaries on both
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)

	linkSnapBase := stss[0].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapBase, NotNil)
	linkSnapKernel := stss[1].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapKernel, NotNil)

	// linking between the base and kernel is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range stss[0].TaskSet().Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, stss[0].TaskSet().Tasks()[i-1].ID())
		}
		if t == linkSnapBase {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel := firstTaskAfterLocalModifications(c, stss[1].TaskSet())
	beginTaskOfKernel, err := stss[1].TaskSet().Edge(snapstate.BeginEdge)
	c.Assert(err, IsNil)
	linkTaskOfKernel, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - first local modification task of kernel must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - prerequisites/download should not be serialized behind base link
	c.Check(beginTaskOfKernel.WaitTasks(), Not(testutil.Contains), linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of base (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// both should be transactional
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, true)

	// Since they are set up for single-reboot, the base should have restart
	// boundaries for the undo path, and kernel should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[0].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsBaseGadget(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Expect restart boundaries on both
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)

	linkSnapBase := stss[0].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapBase, NotNil)
	linkSnapGadget := stss[1].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapGadget, NotNil)

	// linking between the base and gadget is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range stss[0].TaskSet().Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, stss[0].TaskSet().Tasks()[i-1].ID())
		}
		if t == linkSnapBase {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	firstTaskOfGadget := firstTaskAfterLocalModifications(c, stss[1].TaskSet())
	linkTaskOfGadget, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - first local modification task of gadget must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of gadget (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on the last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// both should be transactional
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, true)

	// Since they are set up for single-reboot, the base should have restart
	// boundaries for the undo path, and gadget should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[0].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsGadgetKernel(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Expect restart boundaries on both
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)

	linkSnapGadget := stss[0].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapGadget, NotNil)
	linkSnapKernel := stss[1].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapKernel, NotNil)

	// linking between the gadget and kernel is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range stss[0].TaskSet().Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, stss[0].TaskSet().Tasks()[i-1].ID())
		}
		if t == linkSnapGadget {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	firstTaskOfKernel := firstTaskAfterLocalModifications(c, stss[1].TaskSet())
	linkTaskOfKernel, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	linkTaskOfGadget, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfGadget, err := stss[0].TaskSet().Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)

	// Things that must be correct:
	// - first local modification task of kernel must depend on "link-snap" (MaybeRebootEdge) of gadget
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of gadget (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfGadget)

	// both should be transactional
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, true)

	// Since they are set up for single-reboot, the gadget should have restart
	// boundaries for the undo path, and kernel should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[0].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsBaseGadgetKernel(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	linkSnapBase := stss[0].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapBase, NotNil)
	linkSnapKernel := stss[1].TaskSet().MaybeEdge(snapstate.MaybeRebootEdge)
	c.Assert(linkSnapKernel, NotNil)

	// linking between the base, gadget and kernel is now expected to be split
	// expect tasks up to and including 'link-snap' to have no other dependencies
	// than the previous task.
	for i, t := range stss[0].TaskSet().Tasks() {
		if i == 0 {
			c.Check(t.WaitTasks(), HasLen, 0)
		} else {
			c.Check(t.WaitTasks(), HasLen, 1)
			c.Check(t.WaitTasks()[0].ID(), Equals, stss[0].TaskSet().Tasks()[i-1].ID())
		}
		if t == linkSnapBase {
			break
		}
	}

	// Grab the tasks we need to check dependencies between
	linkTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfBase, err := stss[0].TaskSet().Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfGadget := firstTaskAfterLocalModifications(c, stss[1].TaskSet())
	linkTaskOfGadget, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfGadget, err := stss[1].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)
	lastTaskOfGadget, err := stss[1].TaskSet().Edge(snapstate.EndEdge)
	c.Assert(err, IsNil)
	firstTaskOfKernel := firstTaskAfterLocalModifications(c, stss[2].TaskSet())
	linkTaskOfKernel, err := stss[2].TaskSet().Edge(snapstate.MaybeRebootEdge)
	c.Assert(err, IsNil)
	acTaskOfKernel, err := stss[2].TaskSet().Edge(snapstate.MaybeRebootWaitEdge)
	c.Assert(err, IsNil)

	// Things that must be correct between base and gadget:
	// - first local modification task of gadget must depend on "link-snap" (MaybeRebootEdge) of base
	c.Check(firstTaskOfGadget.WaitTasks(), testutil.Contains, linkTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of base must depend on "link-snap" of kernel (MaybeRebootEdge)
	c.Check(acTaskOfBase.WaitTasks(), testutil.Contains, linkTaskOfKernel)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on the last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)

	// Things that must be correct between gadget and kernel:
	// - first local modification task of kernel must depend on "link-snap" (MaybeRebootEdge) of gadget
	c.Check(firstTaskOfKernel.WaitTasks(), testutil.Contains, linkTaskOfGadget)
	// - "auto-connect" (MaybeRebootWaitEdge) of gadget must depend on last task of base (EndEdge)
	c.Check(acTaskOfGadget.WaitTasks(), testutil.Contains, lastTaskOfBase)
	// - "auto-connect" (MaybeRebootWaitEdge) of kernel must depend on the last task of gadget (EndEdge)
	c.Check(acTaskOfKernel.WaitTasks(), testutil.Contains, lastTaskOfGadget)

	// all three should be transactional
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet(), stss[2].TaskSet()), Equals, true)

	// Since they are set up for single-reboot, the base should have restart
	// boundaries for the undo path, and kernel should have for do path.
	c.Check(s.hasUndoRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[0].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[2].TaskSet()), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, stss[2].TaskSet()), Equals, true)

	// Gadget should have no boundaries
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsSnapd(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("snapd", "", snap.TypeSnapd),
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Do not expect any restart boundaries to be set on snapd
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, false)

	// Expect them to be set on core20 as it's the boot-base
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, true)

	// Snapd should never be a part of the single-reboot transaction, we don't
	// need snapd to rollback if an issue should arise in any of the other essential snaps.
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsBootBaseAndOtherBases(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("core18", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Only the boot-base should have restart boundary.
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[2].TaskSet()), Equals, false)

	// boot-base is transactional, but not with the other base and my-app
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, false)
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[2].TaskSet()), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsForSnapWithBaseAndWithout(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("snap-base", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("snap-base-app", "snap-base", snap.TypeApp),
		s.snapInstallTaskSetForSnapSetup("snap-other-app", "other-base", snap.TypeApp),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// No restart boundaries
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[2].TaskSet()), Equals, false)

	// no transactional lane set
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, false)
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[2].TaskSet()), Equals, false)

	// snap-base-app depends on snap-base, but snap-other-app's base
	// is not updated
	c.Check(s.setDependsOn(c, stss[1].TaskSet(), stss[0].TaskSet()), Equals, true)
	c.Check(s.setDependsOn(c, stss[2].TaskSet(), stss[0].TaskSet()), Equals, false)
	c.Check(s.setDependsOn(c, stss[2].TaskSet(), stss[1].TaskSet()), Equals, false)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsForSnapWithBootBaseAndWithout(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("snap-core20-app", "snap-core20", snap.TypeApp),
		s.snapInstallTaskSetForSnapSetup("snap-other-app", "other-base", snap.TypeApp),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// Restart boundaries is set for core20 as the boot-base
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, true)
	c.Check(s.hasRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[2].TaskSet()), Equals, false)

	// Core20 is transactional, but not with the other base and app
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[1].TaskSet()), Equals, false)
	c.Check(taskSetsShareLane(stss[0].TaskSet(), stss[2].TaskSet()), Equals, false)

	// snap-core20-app depends on core20, but snap-other-app' base is
	// not updated. Yet snap-other-base still depends on core20. But there
	// is no dependency between snap-core20-app and snap-other-app
	c.Check(s.setDependsOn(c, stss[1].TaskSet(), stss[0].TaskSet()), Equals, true)  // snap-core20-app depend on core20
	c.Check(s.setDependsOn(c, stss[2].TaskSet(), stss[0].TaskSet()), Equals, true)  // snap-other-app depend on core20
	c.Check(s.setDependsOn(c, stss[2].TaskSet(), stss[1].TaskSet()), Equals, false) // snap-other-app does not depend on snap-core20-app
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsAll(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		s.snapInstallTaskSetForSnapSetup("snapd", "", snap.TypeSnapd),
		s.snapInstallTaskSetForSnapSetup("core20", "", snap.TypeBase),
		s.snapInstallTaskSetForSnapSetup("brand-gadget", "", snap.TypeGadget),
		s.snapInstallTaskSetForSnapSetup("my-kernel", "", snap.TypeKernel),
		s.snapInstallTaskSetForSnapSetup("core", "", snap.TypeOS),
		s.snapInstallTaskSetForSnapSetup("my-app", "", snap.TypeApp),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, IsNil)

	// snapd has no restart boundaries set
	c.Check(s.hasRestartBoundaries(c, stss[0].TaskSet()), Equals, false)

	// boot-base, gadget, kernel setup for single-reboot
	c.Check(s.hasDoRestartBoundaries(c, stss[1].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[1].TaskSet()), Equals, true)
	c.Check(s.hasDoRestartBoundaries(c, stss[2].TaskSet()), Equals, false)
	c.Check(s.hasUndoRestartBoundaries(c, stss[2].TaskSet()), Equals, false)
	c.Check(s.hasDoRestartBoundaries(c, stss[3].TaskSet()), Equals, true)
	c.Check(s.hasUndoRestartBoundaries(c, stss[3].TaskSet()), Equals, false)

	// TypeOS (in this scenario) and TypeApp should have no restart boundaries
	c.Check(s.hasRestartBoundaries(c, stss[4].TaskSet()), Equals, false)
	c.Check(s.hasRestartBoundaries(c, stss[5].TaskSet()), Equals, false)

	// boot-base, gadget and kernel are transactional
	c.Check(taskSetsShareLane(stss[1].TaskSet(), stss[2].TaskSet(), stss[3].TaskSet()), Equals, true)
}

func (s *rebootSuite) TestArrangeSnapInstallTaskSetsFailsSplit(c *C) {
	defer snapstatetest.MockDeviceModel(MakeModel20("brand-gadget", nil))()

	s.state.Lock()
	defer s.state.Unlock()

	stss := []snapstate.SnapInstallTaskSet{
		snapstate.NewSnapInstallTaskSetForTest(nil, nil, nil, nil, nil),
	}
	err := snapstate.ArrangeInstallTasksForSingleReboot(s.state, stss)
	c.Assert(err, ErrorMatches, `internal error: snap install task set has empty slices`)
}
