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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func (s *snapmgrTestSuite) mockComponentInfos(c *C, snapName string, compNames []string, compRevs []snap.Revision) {
	cis := make([]*snap.ComponentInfo, len(compNames))
	for i, comp := range compNames {
		componentYaml := fmt.Sprintf(`component: %s+%s
type: standard
version: 1.0
`, snapName, comp)
		ci, err := snap.InfoFromComponentYaml([]byte(componentYaml))
		c.Assert(err, IsNil)
		cis[i] = ci
	}

	s.AddCleanup(snapstate.MockReadComponentInfo(func(
		compMntDir string, snapInfo *snap.Info, csi *snap.ComponentSideInfo,
	) (*snap.ComponentInfo, error) {
		for i, ci := range cis {
			if strings.HasSuffix(compMntDir, "/"+ci.Component.ComponentName+"/"+compRevs[i].String()) {
				if csi != nil {
					ci.ComponentSideInfo = *csi
				}
				return ci, nil
			}
		}
		panic("component info not found")
	}))
}

func (s *snapmgrTestSuite) TestComponentHelpers(c *C) {
	defer snapstate.MockSnapReadInfo(snap.ReadInfo)()

	const snapName = "mysnap"
	const compName = "mycomp"
	const compName2 = "mycomp2"
	snapRev := snap.R(1)
	snapRev2 := snap.R(2)
	compRev := snap.R(33)

	s.state.Lock()
	defer s.state.Unlock()

	const snapYaml = `name: mysnap
version: 1
components:
  mycomp:
    type: standard
  mycomp2:
    type: standard
`

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
					[]*sequence.ComponentState{sequence.NewComponentState(csi2, snap.StandardComponent), sequence.NewComponentState(csi, snap.StandardComponent)})}),
		Current: snapRev,
	}
	snaptest.MockSnap(c, snapYaml, ssi)
	snaptest.MockSnap(c, snapYaml, ssi2)
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, true)
	c.Check(snapSt.IsCurrentComponentRevInAnyNonCurrentSeq(cref), Equals, false)
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
	c.Check(snapSt.CurrentComponentSideInfos(), DeepEquals, []*snap.ComponentSideInfo{csi2, csi})

	comps, err := snapSt.CurrentComponentInfos()
	c.Assert(err, IsNil)
	c.Check(comps, testutil.DeepUnsortedMatches, []*snap.ComponentInfo{foundCi, foundCi2})
	c.Check(snapSt.HasActiveComponents(), Equals, true)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi2, nil),
				sequence.NewRevisionSideState(ssi, []*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)}),
			}),
		Current: snapRev2,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsComponentInCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsCurrentComponentRevInAnyNonCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, true)
	c.Check(snapSt.CurrentComponentSideInfo(cref), IsNil)
	c.Check(snapSt.CurrentComponentSideInfo(cref2), IsNil)
	foundCi, err = snapSt.CurrentComponentInfo(cref)
	c.Check(err, ErrorMatches, "snap has no current revision")
	c.Check(foundCi, IsNil)

	comps, err = snapSt.CurrentComponentInfos()
	c.Assert(err, IsNil)
	c.Check(comps, HasLen, 0)
	c.Check(snapSt.CurrentComponentSideInfos(), HasLen, 0)

	comps, err = snapSt.ComponentInfosForRevision(ssi2.Revision)
	c.Assert(err, IsNil)
	c.Check(comps, HasLen, 0)
	c.Check(snapSt.HasActiveComponents(), Equals, false)

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
	c.Check(snapSt.IsCurrentComponentRevInAnyNonCurrentSeq(cref), Equals, false)
	c.Check(snapSt.IsComponentRevPresent(csi), Equals, false)
	c.Check(snapSt.CurrentComponentSideInfo(cref), IsNil)
	c.Check(snapSt.CurrentComponentSideInfo(cref2), IsNil)

	comps, err = snapSt.CurrentComponentInfos()
	c.Assert(err, IsNil)
	c.Check(comps, HasLen, 0)
	c.Check(snapSt.CurrentComponentSideInfos(), HasLen, 0)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi2, []*sequence.ComponentState{sequence.NewComponentState(csi2, snap.StandardComponent)}),
				sequence.NewRevisionSideState(ssi, []*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)}),
			}),
		Current: snapRev,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsCurrentComponentRevInAnyNonCurrentSeq(cref), Equals, false)

	foundCi, err = snapSt.CurrentComponentInfo(cref)
	c.Check(err, IsNil)

	snapSt.Current = snapRev2
	foundCi2, err = snapSt.CurrentComponentInfo(cref2)
	c.Check(err, IsNil)

	snapSt.Current = snapRev

	comps, err = snapSt.CurrentComponentInfos()
	c.Assert(err, IsNil)
	c.Check(comps, testutil.DeepUnsortedMatches, []*snap.ComponentInfo{foundCi})
	c.Check(snapSt.CurrentComponentSideInfos(), DeepEquals, []*snap.ComponentSideInfo{csi})

	comps, err = snapSt.ComponentInfosForRevision(snapRev2)
	c.Assert(err, IsNil)
	c.Check(comps, testutil.DeepUnsortedMatches, []*snap.ComponentInfo{foundCi2})

	snapSt = &snapstate.SnapState{}

	_, err = snapSt.CurrentComponentInfos()
	c.Assert(err, testutil.ErrorIs, snapstate.ErrNoCurrent)

	snapSt = &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi2, []*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)}),
				sequence.NewRevisionSideState(ssi, []*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)}),
			}),
		Current: snapRev,
	}
	snapstate.Set(s.state, snapName, snapSt)

	c.Check(snapSt.IsCurrentComponentRevInAnyNonCurrentSeq(cref), Equals, true)
}

func (s *snapmgrTestSuite) TestComponentEnforcedValidationSet(c *C) {
	expectedErr := `cannot remove component "mysnap+mycomp" as it is required by an enforcing validation set`
	s.testComponentRemoveValidationSet(c, "mysnap", "3wdHCAVyZEmYsCMFDE9qt92UV8rC8Wdk", expectedErr)
}

func (s *snapmgrTestSuite) TestComponentUnenforcedValidationSet(c *C) {
	s.testComponentRemoveValidationSet(c, "othersnap", "otherIDVyZEmYsCMFDE9qt92UV8rC8Wdk", "")
}

func (s *snapmgrTestSuite) testComponentRemoveValidationSet(c *C, targetSnapName, targetSnapID, expectedErrorMsg string) {
	defer snapstate.MockSnapReadInfo(snap.ReadInfo)()

	const enforcedSnapName = "mysnap"
	const enforcedSnapID = "3wdHCAVyZEmYsCMFDE9qt92UV8rC8Wdk"

	const compName = "mycomp"
	snapRev := snap.R(1)
	compRev := snap.R(33)

	snapYaml := fmt.Sprintf(`name: %s
version: 1
components:
  %s:
    type: standard
`, targetSnapName, compName)

	s.state.Lock()
	defer s.state.Unlock()

	ssi := &snap.SideInfo{RealName: targetSnapName, Revision: snapRev,
		SnapID: targetSnapID}
	cref := naming.NewComponentRef(targetSnapName, compName)
	csi := snap.NewComponentSideInfo(cref, compRev)

	s.mockComponentInfos(c, targetSnapName, []string{compName}, []snap.Revision{compRev})

	snapSt := &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromRevisionSideInfos(
			[]*sequence.RevisionSideState{
				sequence.NewRevisionSideState(ssi,
					[]*sequence.ComponentState{sequence.NewComponentState(csi, snap.StandardComponent)})}),
		Current: snapRev,
	}
	compSt := snapSt.CurrentComponentState(cref)

	snaptest.MockSnap(c, snapYaml, ssi)
	snapstate.Set(s.state, targetSnapName, snapSt)

	headers := map[string]any{
		"series":     "16",
		"account-id": "developer",
		"name":       "my-set",
		"sequence":   "1",
		"timestamp":  time.Now().Format(time.RFC3339),
		"snaps": []any{
			map[string]any{
				"name":     enforcedSnapName,
				"id":       enforcedSnapID,
				"presence": "required",
				"components": map[string]any{
					compName: map[string]any{
						"presence": "required",
					},
				},
			},
		},
	}

	privKey, _ := assertstest.GenerateKey(1024)
	signingDB := assertstest.NewSigningDB("developer", privKey)
	assertion, err := signingDB.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)

	validSet := assertion.(*asserts.ValidationSet)

	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	info.SideInfo = *ssi

	// Set up enforced validation set mocking
	snapstate.MockEnforcedValidationSets(func(st *state.State, vs ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		vss := snapasserts.NewValidationSets()

		err := vss.Add(validSet)
		if err != nil {
			return nil, err
		}

		return vss, nil
	})

	_, err = snapstate.RemoveComponentTasks(s.state, snapSt, compSt, info, nil, "")

	if expectedErrorMsg != "" {
		c.Assert(err, NotNil)
		c.Assert(err.Error(), Equals, expectedErrorMsg)
	} else {
		c.Assert(err, IsNil)
	}
}
