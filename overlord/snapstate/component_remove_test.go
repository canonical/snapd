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
	"errors"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

func expectedComponentRemoveTasks(opts int) []string {
	var removeTasks []string
	removeTasks = append(removeTasks, "unlink-current-component")
	if opts&compTypeIsKernMods != 0 {
		removeTasks = append(removeTasks, "prepare-kernel-modules-components")
	}
	if opts&compCurrentIsDiscarded != 0 {
		removeTasks = append(removeTasks, "discard-component")
	}
	return removeTasks
}

func verifyComponentRemoveTasks(c *C, opts int, ts *state.TaskSet) {
	kinds := taskKinds(ts.Tasks())

	expected := expectedComponentRemoveTasks(opts)
	c.Assert(kinds, DeepEquals, expected)

	checkSetupTasks(c, ts)
}

func (s *snapmgrTestSuite) TestRemoveComponent(c *C) {
	s.testRemoveComponent(c, snapstate.RemoveComponentsOpts{})
}

func (s *snapmgrTestSuite) TestRemoveComponentRefreshProf(c *C) {
	s.testRemoveComponent(c, snapstate.RemoveComponentsOpts{RefreshProfile: true})
}

func (s *snapmgrTestSuite) testRemoveComponent(c *C, opts snapstate.RemoveComponentsOpts) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(7)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneComponent(s.state, snapName, snapRev, compName, compRev)

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName}, opts)
	c.Assert(err, IsNil)

	numTaskSets := 1
	if opts.RefreshProfile {
		numTaskSets += 1
	}
	c.Assert(len(tss), Equals, numTaskSets)
	totalTasks := 0
	for i, ts := range tss {
		if i == len(tss)-1 && opts.RefreshProfile {
			kinds := taskKinds(ts.Tasks())
			c.Assert(kinds, DeepEquals, []string{"setup-profiles"})
		} else {
			verifyComponentRemoveTasks(c, compCurrentIsDiscarded, ts)
		}
		totalTasks += len(ts.Tasks())
	}

	c.Assert(s.state.TaskCount(), Equals, totalTasks)
}

func (s *snapmgrTestSuite) TestRemoveComponents(c *C) {
	s.testRemoveComponents(c, snapstate.RemoveComponentsOpts{})
}

func (s *snapmgrTestSuite) TestRemoveComponentsRefreshProf(c *C) {
	s.testRemoveComponents(c, snapstate.RemoveComponentsOpts{RefreshProfile: true})
}

func (s *snapmgrTestSuite) testRemoveComponents(c *C, opts snapstate.RemoveComponentsOpts) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "other-comp"
	snapRev := snap.R(1)

	s.state.Lock()
	defer s.state.Unlock()

	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(1))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName2), snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName, compName2}, opts)
	c.Assert(err, IsNil)

	numTaskSets := 2
	if opts.RefreshProfile {
		numTaskSets += 1
	}
	c.Assert(len(tss), Equals, numTaskSets)
	totalTasks := 0
	for i, ts := range tss {
		if i == len(tss)-1 && opts.RefreshProfile {
			kinds := taskKinds(ts.Tasks())
			c.Assert(kinds, DeepEquals, []string{"setup-profiles"})
		} else {
			verifyComponentRemoveTasks(c, compTypeIsKernMods|compCurrentIsDiscarded, ts)
		}
		totalTasks += len(ts.Tasks())
	}

	c.Assert(s.state.TaskCount(), Equals, totalTasks)
}

func (s *snapmgrTestSuite) TestRemoveComponentNoSnap(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"

	s.state.Lock()
	defer s.state.Unlock()

	tss, err := snapstate.RemoveComponents(s.state, snapName,
		[]string{compName}, snapstate.RemoveComponentsOpts{})
	c.Assert(tss, IsNil)
	var notInstalledError *snap.NotInstalledError
	c.Assert(errors.As(err, &notInstalledError), Equals, true)
	c.Assert(notInstalledError, DeepEquals, &snap.NotInstalledError{
		Snap: snapName,
		Rev:  snap.R(0),
	})
}

func (s *snapmgrTestSuite) TestRemoveNonPresentComponent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)

	s.state.Lock()
	defer s.state.Unlock()

	setStateWithOneSnap(s.state, snapName, snapRev)

	tss, err := snapstate.RemoveComponents(s.state, snapName,
		[]string{compName}, snapstate.RemoveComponentsOpts{})
	c.Assert(tss, IsNil)
	var notInstalledError *snap.ComponentNotInstalledError
	c.Assert(errors.As(err, &notInstalledError), Equals, true)
	c.Assert(notInstalledError, DeepEquals, &snap.ComponentNotInstalledError{
		NotInstalledError: snap.NotInstalledError{
			Snap: snapName,
			Rev:  snap.R(1),
		},
		Component: compName,
		CompRev:   snap.R(0),
	})
}

func (s *snapmgrTestSuite) TestRemoveComponentPathRun(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "other-comp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, _ := createTestComponent(c, snapName, compName, info)
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	cref1 := naming.NewComponentRef(snapName, compName)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi1 := snap.NewComponentSideInfo(cref1, snap.R(1))
	csi2 := snap.NewComponentSideInfo(cref2, snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	tss, err := snapstate.RemoveComponents(s.state, snapName,
		[]string{compName}, snapstate.RemoveComponentsOpts{})
	c.Assert(err, IsNil)

	c.Assert(len(tss), Equals, 1)

	chg := s.state.NewChange("remove component", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	c.Assert(chg.Err(), IsNil)
	c.Assert(chg.IsReady(), Equals, true)
	for _, ts := range tss {
		verifyComponentRemoveTasks(c, compTypeIsKernMods|compCurrentIsDiscarded, ts)
	}

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)

	c.Assert(snapst.IsComponentInCurrentSeq(cref1), Equals, false)
	c.Assert(snapst.IsComponentInCurrentSeq(cref2), Equals, true)
}

func (s *snapmgrTestSuite) TestRemoveComponentsPathRunWithError(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "other-comp"
	snapRev := snap.R(1)
	info := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ci, _ := createTestComponent(c, snapName, compName, info)
	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
		return ci, nil
	}))

	s.state.Lock()
	defer s.state.Unlock()

	cref1 := naming.NewComponentRef(snapName, compName)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi1 := snap.NewComponentSideInfo(cref1, snap.R(1))
	csi2 := snap.NewComponentSideInfo(cref2, snap.R(33))
	cs1 := sequence.NewComponentState(csi1, snap.KernelModulesComponent)
	cs2 := sequence.NewComponentState(csi2, snap.KernelModulesComponent)
	setStateWithComponents(s.state, snapName, snapRev, []*sequence.ComponentState{cs1, cs2})

	// try to remove both components
	tss, err := snapstate.RemoveComponents(s.state, snapName,
		[]string{compName, compName2}, snapstate.RemoveComponentsOpts{RefreshProfile: true})
	c.Assert(err, IsNil)

	c.Assert(len(tss), Equals, 3)

	chg := s.state.NewChange("remove component", "...")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	// Add error task for the first component we want to remove
	ts0 := tss[0].Tasks()
	// it will happen before discard component and setup profile
	ts0lastOk := ts0[len(ts0)-2]
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	c.Assert(len(ts0lastOk.Lanes()), Equals, 1)
	terr.JoinLane(ts0lastOk.Lanes()[0])
	terr.WaitFor(ts0lastOk)
	// make sure update profiles waits on this error task
	updateProfTask := tss[2].Tasks()[0]
	updateProfTask.WaitTasks()
	updateProfTask.WaitFor(terr)
	chg.AddTask(terr)

	s.settle(c)

	c.Assert(chg.Err().Error(), Equals,
		"cannot perform the following tasks:\n- provoking total undo (error out)")
	c.Assert(chg.IsReady(), Equals, true)
	verifyComponentRemoveTasks(c, compTypeIsKernMods|compCurrentIsDiscarded, tss[0])
	verifyComponentRemoveTasks(c, compTypeIsKernMods|compCurrentIsDiscarded, tss[1])
	kinds := taskKinds(tss[2].Tasks())
	c.Assert(kinds, DeepEquals, []string{"setup-profiles"})

	// component tasks are undone/hold
	for i := 0; i < 2; i++ {
		ts := tss[i].Tasks()
		c.Check(ts[0].Status(), Equals, state.UndoneStatus)
		c.Check(ts[1].Status(), Equals, state.UndoneStatus)
		c.Check(ts[2].Status(), Equals, state.HoldStatus)
	}
	// update profile is hold
	c.Check(updateProfTask.Status(), Equals, state.HoldStatus)

	var snapst snapstate.SnapState
	c.Assert(snapstate.Get(s.state, snapName, &snapst), IsNil)

	// we could not remove the components
	c.Assert(snapst.IsComponentInCurrentSeq(cref1), Equals, true)
	c.Assert(snapst.IsComponentInCurrentSeq(cref2), Equals, true)
}

func (s *snapmgrTestSuite) TestRemoveComponentsRevInTwoSeqPts(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	opts := snapstate.RemoveComponentsOpts{RefreshProfile: true}

	s.state.Lock()
	defer s.state.Unlock()

	// Current component is present in current and in another sequence point
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snap.R(10),
		SnapID: "some-snap-id"}
	currentCsi := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName), snap.R(3))
	compsSi := []*sequence.ComponentState{
		sequence.NewComponentState(currentCsi, snap.KernelModulesComponent),
	}
	snapst := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, compsSi),
				sequence.NewRevisionSideState(ssi2, compsSi),
			}),
		Current: snapRev,
	}
	snapstate.Set(s.state, snapName, snapst)

	tss, err := snapstate.RemoveComponents(s.state, snapName, []string{compName}, opts)
	c.Assert(err, IsNil)

	c.Assert(len(tss), Equals, 2)
	totalTasks := 0
	for i, ts := range tss {
		if i == len(tss)-1 {
			kinds := taskKinds(ts.Tasks())
			c.Assert(kinds, DeepEquals, []string{"setup-profiles"})
		} else {
			verifyComponentRemoveTasks(c, compTypeIsKernMods, ts)
		}
		totalTasks += len(ts.Tasks())
	}

	c.Assert(s.state.TaskCount(), Equals, totalTasks)
}
