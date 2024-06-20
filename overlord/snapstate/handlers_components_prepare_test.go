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
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

type prepareCompSnapSuite struct {
	baseHandlerSuite
}

var _ = Suite(&prepareCompSnapSuite{})

func (s *prepareSnapSuite) TestDoPrepareComponentSimple(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	// Unset component revision
	compRev := snap.R(0)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	t := s.state.NewTask("prepare-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, "path-to-component"))
	t.Set("snap-setup", ssu)

	s.state.NewChange("test change", "change desc").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var csup snapstate.ComponentSetup
	t.Get("component-setup", &csup)
	// Revision should have been set to x1 (-1)
	c.Check(csup.CompSideInfo, DeepEquals, snap.NewComponentSideInfo(
		cref, snap.R(-1),
	))
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *prepareSnapSuite) TestDoPrepareComponentAlreadyPresent(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(-11)
	// Unset component revision
	compRev := snap.R(0)
	si := createTestSnapInfoForComponent(c, snapName, snapRev, compName)
	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snap.R(-10),
		SnapID: "some-snap-id"}
	ssu := createTestSnapSetup(si, snapstate.Flags{})

	s.state.Lock()

	// state with some components around already
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", compName), snap.R(-3))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", compName), snap.R(-2))
	csi3 := snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", "othercomp"), snap.R(-13))
	compsSi1 := []*sequence.ComponentState{
		sequence.NewComponentState(csi1, snap.TestComponent),
		sequence.NewComponentState(csi2, snap.TestComponent),
		sequence.NewComponentState(csi3, snap.TestComponent),
	}
	csi4 := snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", compName), snap.R(-8))
	csi5 := snap.NewComponentSideInfo(naming.NewComponentRef("some-snap", "othercomp"), snap.R(-9))
	compsSi2 := []*sequence.ComponentState{
		sequence.NewComponentState(csi4, snap.TestComponent),
		sequence.NewComponentState(csi5, snap.TestComponent),
	}
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi, compsSi1),
				sequence.NewRevisionSideState(ssi2, compsSi2),
			}),
		Current: si.Revision,
	}
	snapstate.Set(s.state, snapName, snapst)

	t := s.state.NewTask("prepare-component", "task desc")
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	t.Set("component-setup", snapstate.NewComponentSetup(csi, snap.TestComponent, "path-to-component"))
	t.Set("snap-setup", ssu)

	s.state.NewChange("test change", "change desc").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	var csup snapstate.ComponentSetup
	t.Get("component-setup", &csup)
	// Revision should have been set to x9 (-9)
	c.Check(csup.CompSideInfo, DeepEquals, snap.NewComponentSideInfo(
		cref, snap.R(-9),
	))
	c.Check(t.Status(), Equals, state.DoneStatus)
}
