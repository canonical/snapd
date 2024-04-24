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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	. "gopkg.in/check.v1"
)

func (s *snapmgrTestSuite) mockComponentInfos(c *C, snapName string, compNames []string, compRevs []snap.Revision) {
	cis := make([]*snap.ComponentInfo, len(compNames))
	for i, comp := range compNames {
		componentYaml := fmt.Sprintf(`component: %s+%s
type: test
version: 1.0
`, snapName, comp)
		ci, err := snap.InfoFromComponentYaml([]byte(componentYaml))
		c.Assert(err, IsNil)
		cis[i] = ci
	}

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info,
	) (*snap.ComponentInfo, error) {
		for i, ci := range cis {
			if strings.HasSuffix(compMntDir, "/"+ci.Component.ComponentName+"/"+compRevs[i].String()) {
				return ci, nil
			}
		}
		panic("component info not found")
	}))
}

func (s *snapmgrTestSuite) TestComponentHelpers(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	s.state.Lock()
	defer s.state.Unlock()

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev,
		SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2,
		SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, compRev)
	s.mockComponentInfos(c, snapName, []string{compName, compName2},
		[]snap.Revision{compRev, compRev})

	snapSt := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi,
					[]*sequence.ComponentState{sequence.NewComponentState(csi2, snap.TestComponent), sequence.NewComponentState(csi, snap.TestComponent)})}),
		Current: snapRev,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, true)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, true)
	foundCsi := snapSt.CurrentComponentSideInfo(cref)
	c.Check(foundCsi, DeepEquals, csi)
	foundCsi2 := snapSt.CurrentComponentSideInfo(cref2)
	c.Check(foundCsi2, DeepEquals, csi2)
	foundCi, err := snapSt.CurrentComponentInfo(cref)
	c.Check(err, IsNil)
	c.Check(foundCi, NotNil)
	foundCi2, err := snapSt.CurrentComponentInfo(cref2)
	c.Check(err, IsNil)
	c.Check(foundCi2, NotNil)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi2, nil),
				sequence.NewRevisionSideState(ssi, []*sequence.ComponentState{sequence.NewComponentState(csi, snap.TestComponent)}),
			}),
		Current: snapRev2,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, true)
	c.Check(snapSt.CurrentComponentSideInfo(cref), IsNil)
	c.Check(snapSt.CurrentComponentSideInfo(cref2), IsNil)
	foundCi, err = snapSt.CurrentComponentInfo(cref)
	c.Check(err, ErrorMatches, "snap has no current revision")
	c.Check(foundCi, IsNil)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi2, nil),
				sequence.NewRevisionSideState(ssi, nil),
			}),
		Current: snapRev2,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, false)
	c.Check(snapSt.CurrentComponentSideInfo(cref), IsNil)
	c.Check(snapSt.CurrentComponentSideInfo(cref2), IsNil)
}
