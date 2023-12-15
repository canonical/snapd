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
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

func (s *snapmgrTestSuite) TestIsComponentHelpers(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	s.state.Lock()
	defer s.state.Unlock()

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "snapidididididididididididididid"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2,
		SnapID: "snapidididididididididididididid"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, compRev)

	snapSt := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*snapstate.RevisionSideState{
				snapstate.NewRevisionSideInfo(ssi,
					[]*snap.ComponentSideInfo{csi2, csi})}),
		Current: snapRev,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, true)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, true)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*snapstate.RevisionSideState{
				snapstate.NewRevisionSideInfo(ssi2, nil),
				snapstate.NewRevisionSideInfo(ssi, []*snap.ComponentSideInfo{csi}),
			}),
		Current: snapRev2,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, true)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*snapstate.RevisionSideState{
				snapstate.NewRevisionSideInfo(ssi2, nil),
				snapstate.NewRevisionSideInfo(ssi, nil),
			}),
		Current: snapRev2,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, false)
}
