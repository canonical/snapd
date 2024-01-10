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
		sequence.NewRevisionSideInfo(si1, []*snap.ComponentSideInfo{
			snap.NewComponentSideInfo(naming.NewComponentRef("mysnap", "mycomp"),
				snap.R(7)),
		}),
		sequence.NewRevisionSideInfo(si2, []*snap.ComponentSideInfo{
			snap.NewComponentSideInfo(naming.NewComponentRef("othersnap", "othercomp1"),
				snap.R(11)),
			snap.NewComponentSideInfo(naming.NewComponentRef("othersnap", "othercomp2"), snap.R(14)),
		}),
	})
	marshaled, err = json.Marshal(seq)
	c.Assert(err, IsNil)
	c.Check(string(marshaled), Equals, `[{"name":"mysnap","snap-id":"snapid","revision":"7","components":[{"component":{"snap-name":"mysnap","component-name":"mycomp"},"revision":"7"}]},{"name":"othersnap","snap-id":"otherid","revision":"11","components":[{"component":{"snap-name":"othersnap","component-name":"othercomp1"},"revision":"11"},{"component":{"snap-name":"othersnap","component-name":"othercomp2"},"revision":"14"}]}]`)

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
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{
			sequence.NewRevisionSideInfo(ssi, []*snap.ComponentSideInfo{csi}),
			sequence.NewRevisionSideInfo(ssi2, nil)})

	c.Check(seq.SideInfos(), DeepEquals, []*snap.SideInfo{ssi, ssi2})
}

func (s *sequenceTestSuite) TestAddComponentForRevision(c *C) {
	const snapName = "foo"
	snapRev := snap.R(1)
	const compName1 = "comp1"
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName1), snap.R(2))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName1), snap.R(3))

	ssi := &snap.SideInfo{RealName: snapName,
		Revision: snap.R(1), SnapID: "some-snap-id"}
	comps := []*snap.ComponentSideInfo{csi1, csi2}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{sequence.NewRevisionSideInfo(ssi, comps)})
	c.Assert(seq.AddComponentForRevision(snapRev, csi1), IsNil)
	// Not re-appended
	c.Assert(seq.Revisions[0].Components, DeepEquals, comps)

	csi3 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, "other-comp"), snap.R(1))
	c.Assert(seq.AddComponentForRevision(snapRev, csi3), IsNil)
	comps = []*snap.ComponentSideInfo{csi1, csi2, csi3}
	c.Assert(seq.Revisions[0].Components, DeepEquals, comps)

	c.Assert(seq.AddComponentForRevision(snap.R(2), csi3), Equals, sequence.ErrSnapRevNotInSequence)
}

func (s *sequenceTestSuite) TestRemoveComponentForRevision(c *C) {
	const snapName = "foo"
	snapRev := snap.R(1)
	const compName1 = "comp1"
	const compName2 = "comp2"
	csi1 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName1), snap.R(2))
	csi2 := snap.NewComponentSideInfo(naming.NewComponentRef(snapName, compName2), snap.R(3))

	ssi := &snap.SideInfo{RealName: snapName,
		Revision: snap.R(1), SnapID: "some-snap-id"}
	comps := []*snap.ComponentSideInfo{csi1, csi2}
	seq := snapstatetest.NewSequenceFromRevisionSideInfos(
		[]*sequence.RevisionSideState{sequence.NewRevisionSideInfo(ssi, comps)})

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
	c.Assert(removed, DeepEquals, csi1)
	c.Assert(seq.Revisions[0].Components, DeepEquals, []*snap.ComponentSideInfo{csi2})
}
