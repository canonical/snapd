// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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

package sequence_test

import (
	"encoding/json"
	"testing"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type sequenceTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sequenceTestSuite{})

func (s *sequenceTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
}

func (s *sequenceTestSuite) TestSequenceSerialize(c *C) {
	si1 := &snap.SideInfo{RealName: "mysnap", SnapID: "snapid", Revision: snap.R(7)}
	si2 := &snap.SideInfo{RealName: "othersnap", SnapID: "otherid", Revision: snap.R(11)}

	// Without components
	seq := snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
		si1, si2,
	})
	marshaled, err := json.Marshal(seq)
	c.Assert(err, IsNil)
	c.Check(string(marshaled), Equals, `[{"name":"mysnap","snap-id":"snapid","revision":"7"},{"name":"othersnap","snap-id":"otherid","revision":"11"}]`)

	// Now check that unmarshaling is as expected
	var readSeq sequence.SnapSequence
	c.Check(json.Unmarshal(marshaled, &readSeq), IsNil)
	c.Check(readSeq, DeepEquals, seq)

	// With components
	seq = snapstatetest.NewSequenceFromRevisionSideInfos([]*sequence.RevisionSideState{
		sequence.NewRevisionSideState(si1, []*sequence.ComponentState{
			sequence.NewComponentState(snap.NewComponentSideInfo(naming.NewComponentRef("mysnap", "mycomp"), snap.R(7)), snap.TestComponent),
		}),
		sequence.NewRevisionSideState(si2, []*sequence.ComponentState{
			sequence.NewComponentState(snap.NewComponentSideInfo(naming.NewComponentRef("othersnap", "othercomp1"), snap.R(11)), snap.TestComponent),
			sequence.NewComponentState(snap.NewComponentSideInfo(naming.NewComponentRef("othersnap", "othercomp2"), snap.R(14)), snap.TestComponent),
		}),
	})
	marshaled, err = json.Marshal(seq)
	c.Assert(err, IsNil)
	c.Check(string(marshaled), Equals, `[{"name":"mysnap","snap-id":"snapid","revision":"7","components":[{"side-info":{"component":{"snap-name":"mysnap","component-name":"mycomp"},"revision":"7"},"type":"test"}]},{"name":"othersnap","snap-id":"otherid","revision":"11","components":[{"side-info":{"component":{"snap-name":"othersnap","component-name":"othercomp1"},"revision":"11"},"type":"test"},{"side-info":{"component":{"snap-name":"othersnap","component-name":"othercomp2"},"revision":"14"},"type":"test"}]}]`)

	// Now check that unmarshaling is as expected
	c.Check(json.Unmarshal(marshaled, &readSeq), IsNil)
	c.Check(readSeq, DeepEquals, seq)
}

func (s *sequenceTestSuite) TestSideInfos(c *C) {
	ssi := &snap.SideInfo{RealName: "foo", Revision: snap.R(1),
		SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: "foo", Revision: snap.R(2),
		SnapID: "some-snap-id"}
	csi := snap.NewComponentSideInfo(naming.NewComponentRef("foo", "comp"), snap.R(11))
	cs := sequence.NewComponentState(csi, snap.TestComponent)
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi, []*sequence.ComponentState{cs}),
			sequence.NewRevisionSideState(ssi2, nil)})

	c.Check(seq.SideInfos(), DeepEquals, []*snap.SideInfo{ssi, ssi2})
}

func (s *sequenceTestSuite) TestAddComponentForRevision(c *C) {
	const snapName = "foo"
	snapRev := snap.R(1)
	const compName1 = "comp1"
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName1), snap.R(2))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName1), snap.R(3))
	csi3 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(1))
	cs1 := sequence.NewComponentState(csi1, snap.TestComponent)
	cs2 := sequence.NewComponentState(csi2, snap.TestComponent)
	cs3 := sequence.NewComponentState(csi3, snap.TestComponent)

	ssi := &snap.SideInfo{RealName: snapName,
		Revision: snap.R(1), SnapID: "some-snap-id"}
	sliceCs1 := []*sequence.ComponentState{cs1}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{sequence.NewRevisionSideState(ssi, sliceCs1)})
	c.Assert(seq.AddComponentForRevision(snapRev, cs1), IsNil)
	// Not re-appended
	c.Assert(seq.Revisions[0].Components, DeepEquals, sliceCs1)

	// Replace component with different revision
	c.Assert(seq.AddComponentForRevision(snapRev, cs2), IsNil)
	c.Assert(seq.Revisions[0].Components, DeepEquals, []*sequence.ComponentState{cs2})

	c.Assert(seq.AddComponentForRevision(snapRev, cs3), IsNil)
	c.Assert(seq.Revisions[0].Components, DeepEquals, []*sequence.ComponentState{cs2, cs3})

	c.Assert(seq.AddComponentForRevision(snap.R(2), cs3), Equals, sequence.ErrSnapRevNotInSequence)
}

func (s *sequenceTestSuite) TestRemoveComponentForRevision(c *C) {
	const snapName = "foo"
	snapRev := snap.R(1)
	const compName1 = "comp1"
	const compName2 = "comp2"
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName1), snap.R(2))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName2), snap.R(3))
	cs1 := sequence.NewComponentState(csi1, snap.TestComponent)
	cs2 := sequence.NewComponentState(csi2, snap.TestComponent)

	ssi := &snap.SideInfo{RealName: snapName,
		Revision: snap.R(1), SnapID: "some-snap-id"}
	comps := []*sequence.ComponentState{cs1, cs2}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{sequence.NewRevisionSideState(ssi, comps)})

	// component not in sequence point
	removed := seq.RemoveComponentForRevision(snapRev, naming.NewComponentRef(snapName, "other-comp"))
	c.Assert(removed, IsNil)
	c.Assert(seq.Revisions[0].Components, DeepEquals, comps)

	// snap revision not in sequence
	removed = seq.RemoveComponentForRevision(snap.R(2), naming.NewComponentRef(snapName, compName1))
	c.Assert(removed, IsNil)
	c.Assert(seq.Revisions[0].Components, DeepEquals, comps)

	// component is removed
	removed = seq.RemoveComponentForRevision(snapRev, naming.NewComponentRef(snapName, compName1))
	c.Assert(removed, DeepEquals, cs1)
	c.Assert(seq.Revisions[0].Components, DeepEquals, []*sequence.ComponentState{cs2})
}

func (s *sequenceTestSuite) TestSequenceHelpers(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev, SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2, SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	compSt := sequence.NewComponentState(csi, snap.TestComponent)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, compRev)
	compSt2 := sequence.NewComponentState(csi2, snap.TestComponent)

	rev1Comps := []*sequence.ComponentState{compSt2, compSt}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi, rev1Comps)})

	c.Check(seq.IsComponentRevPresent(csi), Equals, true)
	foundCst := seq.ComponentStateForRev(0, cref)
	c.Check(foundCst, DeepEquals, compSt)
	foundCst2 := seq.ComponentStateForRev(0, cref2)
	c.Check(foundCst2, DeepEquals, compSt2)
	c.Check(seq.ComponentsForRevision(snapRev), DeepEquals, rev1Comps)
	c.Check(seq.CurrentlyHasComponents(0), Equals, true)

	rev1Comps = []*sequence.ComponentState{
		sequence.NewComponentState(csi, snap.TestComponent)}
	seq = snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi2, nil),
			sequence.NewRevisionSideState(ssi, rev1Comps),
		})

	c.Check(seq.IsComponentRevPresent(csi), Equals, true)
	c.Check(seq.ComponentStateForRev(0, cref), IsNil)
	c.Check(seq.ComponentStateForRev(0, cref2), IsNil)
	foundCst = seq.ComponentStateForRev(0, cref)
	c.Check(foundCst, IsNil)
	c.Check(seq.ComponentsForRevision(snapRev), DeepEquals, rev1Comps)
	c.Check(seq.ComponentsForRevision(snapRev2), IsNil)
	c.Check(seq.CurrentlyHasComponents(0), Equals, false)
	c.Check(seq.CurrentlyHasComponents(1), Equals, true)

	seq = snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi2, nil),
			sequence.NewRevisionSideState(ssi, nil),
		})

	c.Check(seq.IsComponentRevPresent(csi), Equals, false)
	c.Check(seq.ComponentStateForRev(0, cref), IsNil)
	c.Check(seq.ComponentStateForRev(1, cref2), IsNil)
	c.Check(seq.CurrentlyHasComponents(0), Equals, false)
	c.Check(seq.CurrentlyHasComponents(1), Equals, false)
}

func (s *sequenceTestSuite) TestKernelModulesComponentsForRev(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev, SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2, SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, compRev)

	rev1Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi2, snap.KernelModulesComponent),
		sequence.NewComponentState(csi, snap.TestComponent)}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi2, nil),
			sequence.NewRevisionSideState(ssi, rev1Comps),
		})

	c.Check(seq.ComponentsWithTypeForRev(snapRev, snap.KernelModulesComponent),
		DeepEquals, []*snap.ComponentSideInfo{csi2})
	c.Check(len(seq.ComponentsWithTypeForRev(snapRev2, snap.KernelModulesComponent)),
		Equals, 0)
}

func (s *sequenceTestSuite) TestIsComponentRevInRefSeqPtInAnyOtherSeqPt(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev, SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2, SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)

	rev1Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi, snap.TestComponent)}
	rev2Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi, snap.TestComponent)}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi, rev1Comps),
			sequence.NewRevisionSideState(ssi2, rev2Comps),
		})

	c.Assert(seq.IsComponentRevInRefSeqPtInAnyOtherSeqPt(
		naming.NewComponentRef(snapName, compName), 0),
		Equals, true)
	c.Assert(seq.IsComponentRevInRefSeqPtInAnyOtherSeqPt(
		naming.NewComponentRef(snapName, compName), 1),
		Equals, true)

	csi2 := snap.NewComponentSideInfo(cref, snap.R(5))
	rev3Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi2, snap.TestComponent)}
	seq = snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi, rev1Comps),
			sequence.NewRevisionSideState(ssi2, rev3Comps),
		})

	c.Assert(seq.IsComponentRevInRefSeqPtInAnyOtherSeqPt(
		naming.NewComponentRef(snapName, compName), 0),
		Equals, false)
	c.Assert(seq.IsComponentRevInRefSeqPtInAnyOtherSeqPt(
		naming.NewComponentRef(snapName, compName), 1),
		Equals, false)
}

func (s *sequenceTestSuite) TestLocalRevision(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(-1)
	snapRev2 := snap.R(-2)
	compRev := snap.R(-33)

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev, SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2, SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, snap.R(-45))
	cref3 := naming.NewComponentRef(snapName, compName)
	csi3 := snap.NewComponentSideInfo(cref3, snap.R(-2))
	cref4 := naming.NewComponentRef(snapName, compName)
	csi4 := snap.NewComponentSideInfo(cref4, snap.R(-34))

	rev1Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi2, snap.KernelModulesComponent),
		sequence.NewComponentState(csi, snap.TestComponent)}
	rev2Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi3, snap.TestComponent),
		sequence.NewComponentState(csi4, snap.TestComponent)}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi2, rev2Comps),
			sequence.NewRevisionSideState(ssi, rev1Comps),
		})

	c.Assert(seq.MinimumLocalRevision(), Equals, snap.R(-2))
	c.Assert(seq.MinimumLocalComponentRevision(compName), Equals, snap.R(-34))
	c.Assert(seq.MinimumLocalComponentRevision(compName2), Equals, snap.R(-45))
}

func (s *sequenceTestSuite) TestNoLocalRevision(c *C) {
	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	ssi := &snap.SideInfo{RealName: snapName, Revision: snapRev, SnapID: "some-snap-id"}
	ssi2 := &snap.SideInfo{RealName: snapName, Revision: snapRev2, SnapID: "some-snap-id"}
	cref := naming.NewComponentRef(snapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)
	cref2 := naming.NewComponentRef(snapName, compName2)
	csi2 := snap.NewComponentSideInfo(cref2, snap.R(45))
	cref3 := naming.NewComponentRef(snapName, compName)
	csi3 := snap.NewComponentSideInfo(cref3, snap.R(2))
	cref4 := naming.NewComponentRef(snapName, compName)
	csi4 := snap.NewComponentSideInfo(cref4, snap.R(34))

	rev1Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi2, snap.KernelModulesComponent),
		sequence.NewComponentState(csi, snap.TestComponent)}
	rev2Comps := []*sequence.ComponentState{
		sequence.NewComponentState(csi3, snap.TestComponent),
		sequence.NewComponentState(csi4, snap.TestComponent)}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideState(ssi2, rev2Comps),
			sequence.NewRevisionSideState(ssi, rev1Comps),
		})

	c.Assert(seq.MinimumLocalRevision(), Equals, snap.R(0))
	c.Assert(seq.MinimumLocalComponentRevision(compName), Equals, snap.R(0))
	c.Assert(seq.MinimumLocalComponentRevision(compName2), Equals, snap.R(0))
}
