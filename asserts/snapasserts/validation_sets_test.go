// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package snapasserts_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
)

type validationSetsSuite struct{}

var _ = Suite(&validationSetsSuite{})

func (s *validationSetsSuite) TestAddFromSameSequence(c *C) {
	mySnapAt7Valset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt8Valset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "8",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	err := valsets.Add(mySnapAt7Valset)
	c.Assert(err, IsNil)
	err = valsets.Add(mySnapAt8Valset)
	c.Check(err, ErrorMatches, `cannot add a second validation-set under "account-id/my-snap-ctl"`)
}

func (s *validationSetsSuite) TestIntersections(c *C) {
	mySnapAt7Valset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt7Valset2 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt8Valset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-other",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "8",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt8OptValset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-opt",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "optional",
				"revision": "8",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapInvalidValset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-inv",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt7OptValset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-opt2",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "optional",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapReqValset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-req-only",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapOptValset := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-opt-only",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	tests := []struct {
		sets        []*asserts.ValidationSet
		conflictErr string
	}{
		{[]*asserts.ValidationSet{mySnapAt7Valset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt7Valset2}, ""},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8Valset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-other\)`},
		{[]*asserts.ValidationSet{mySnapAt8Valset, mySnapAt8OptValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8OptValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-opt\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapInvalidValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at revision 7 \(account-id/my-snap-ctl\)`},
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapAt7Valset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at revision 7 \(account-id/my-snap-ctl\)`},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapInvalidValset}, ""},
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapAt8OptValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7OptValset, mySnapAt8OptValset}, ""}, // no conflict but interpreted as invalid
		{[]*asserts.ValidationSet{mySnapAt7OptValset, mySnapAt8OptValset, mySnapAt7Valset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl,account-id/my-snap-ctl-opt2\), 8 \(account-id/my-snap-ctl-opt\)`},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapInvalidValset, mySnapAt7Valset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-opt\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapReqValset}, ""},
		{[]*asserts.ValidationSet{mySnapReqValset, mySnapAt7Valset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapReqValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapReqValset, mySnapAt7OptValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl-opt2\), 8 \(account-id/my-snap-ctl-opt\) or required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapAt7OptValset, mySnapReqValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl-opt2\), 8 \(account-id/my-snap-ctl-opt\) or required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapReqValset, mySnapInvalidValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapReqValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8Valset, mySnapOptValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-other\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapOptValset}, ""},
		{[]*asserts.ValidationSet{mySnapOptValset, mySnapAt7Valset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapOptValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapOptValset, mySnapAt7OptValset}, ""}, // no conflict but interpreted as invalid
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapOptValset}, ""},
		{[]*asserts.ValidationSet{mySnapOptValset, mySnapInvalidValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8Valset, mySnapReqValset, mySnapInvalidValset}, `(?ms)validation sets are in conflict:.*cannot constrain snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-other\) or at any revision \(account-id/my-snap-ctl-req-only\)`},
	}

	for _, t := range tests {
		valsets := snapasserts.NewValidationSets()
		cSets := make(map[string]*asserts.ValidationSet)
		for _, valset := range t.sets {
			err := valsets.Add(valset)
			c.Assert(err, IsNil)
			// mySnapOptValset never influcens an outcome
			if valset != mySnapOptValset {
				cSets[fmt.Sprintf("%s/%s", valset.AccountID(), valset.Name())] = valset
			}
		}
		err := valsets.Conflict()
		if t.conflictErr == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.conflictErr)
			ce := err.(*snapasserts.ValidationSetsConflictError)
			c.Check(ce.Sets, DeepEquals, cSets)
		}
	}
}

func (s *validationSetsSuite) TestCheckInstalledSnapsNoValidationSets(c *C) {
	valsets := snapasserts.NewValidationSets()
	snaps := []*snapasserts.InstalledSnap{{SnapID: "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", Revision: snap.R(1)}}
	err := valsets.CheckInstalledSnaps(snaps)
	c.Assert(err, IsNil)
}

func (s *validationSetsSuite) TestCheckInstalledSnaps(c *C) {
	// require: snapB rev 3, snapC rev 2.
	// invalid: snapA
	vs1 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "fooname",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-a",
				"id":       "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"presence": "invalid",
			},
			map[string]interface{}{
				"name":     "snap-b",
				"id":       "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"revision": "3",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "snap-c",
				"id":       "mysnapcccccccccccccccccccccccccc",
				"revision": "2",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	// require: snapD any rev
	// optional: snapE any rev
	vs2 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "barname",
		"sequence":     "3",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-d",
				"id":       "mysnapdddddddddddddddddddddddddd",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "snap-e",
				"id":       "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	// optional: snapE any rev
	// note: since it only has an optional snap, acme/bazname is not expected
	// not be invalid by any of the checks.
	vs3 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "bazname",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-e",
				"id":       "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	// invalid: snapA
	vs4 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "booname",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-a",
				"id":       "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(vs1), IsNil)
	c.Assert(valsets.Add(vs2), IsNil)
	c.Assert(valsets.Add(vs3), IsNil)
	c.Assert(valsets.Add(vs4), IsNil)

	snapA := &snapasserts.InstalledSnap{Name: "snap-a", SnapID: "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", Revision: snap.R(1)}
	snapB := &snapasserts.InstalledSnap{Name: "snap-b", SnapID: "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", Revision: snap.R(3)}
	snapBinvRev := &snapasserts.InstalledSnap{Name: "snap-b", SnapID: "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", Revision: snap.R(8)}
	snapC := &snapasserts.InstalledSnap{Name: "snap-c", SnapID: "mysnapcccccccccccccccccccccccccc", Revision: snap.R(2)}
	snapCinvRev := &snapasserts.InstalledSnap{Name: "snap-c", SnapID: "mysnapcccccccccccccccccccccccccc", Revision: snap.R(99)}
	snapD := &snapasserts.InstalledSnap{Name: "snap-d", SnapID: "mysnapdddddddddddddddddddddddddd", Revision: snap.R(2)}
	snapDrev99 := &snapasserts.InstalledSnap{Name: "snap-d", SnapID: "mysnapdddddddddddddddddddddddddd", Revision: snap.R(99)}
	snapE := &snapasserts.InstalledSnap{Name: "snap-e", SnapID: "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee", Revision: snap.R(2)}
	// extra snap, not referenced by any validation set
	snapZ := &snapasserts.InstalledSnap{Name: "snap-z", SnapID: "mysnapzzzzzzzzzzzzzzzzzzzzzzzzzz", Revision: snap.R(1)}

	tests := []struct {
		snaps            []*snapasserts.InstalledSnap
		expectedInvalid  map[string]map[string]bool
		expectedMissing  map[string]map[string]bool
		expectedWrongRev map[string]map[string]bool
	}{
		{
			// required snaps not installed
			snaps: nil,
			expectedMissing: map[string]map[string]bool{
				"snap-b": {"acme/fooname": true},
				"snap-d": {"acme/barname": true},
			},
		},
		{
			// required snaps not installed
			snaps: []*snapasserts.InstalledSnap{
				snapZ,
			},
			expectedMissing: map[string]map[string]bool{
				"snap-b": {"acme/fooname": true},
				"snap-d": {"acme/barname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set
				snapB,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99},
			// ale fine
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set and acme/booname, snap-a presence is invalid
				snapA,
				snapB,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99},
			expectedInvalid: map[string]map[string]bool{
				"snap-a": {"acme/fooname": true, "acme/booname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname and acme/booname validation-sets, snapB missing, snap-a presence is invalid
				snapA,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99},
			expectedInvalid: map[string]map[string]bool{
				"snap-a": {"acme/fooname": true, "acme/booname": true},
			},
			expectedMissing: map[string]map[string]bool{
				"snap-b": {"acme/fooname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set
				snapB,
				snapC,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapD},
			// ale fine
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, snap-c optional but wrong revision
				snapB,
				snapCinvRev,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapD},
			expectedWrongRev: map[string]map[string]bool{
				"snap-c": {"acme/fooname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set but wrong revision
				snapBinvRev,
				// covered by acme/barname validation-set.
				snapD},
			expectedWrongRev: map[string]map[string]bool{
				"snap-b": {"acme/fooname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set
				snapB,
				// covered by acme/barname validation-set. snap-d not installed.
				snapE},
			expectedMissing: map[string]map[string]bool{
				"snap-d": {"acme/barname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// required snaps from acme/fooname are not installed.
				// covered by acme/barname validation-set
				snapDrev99,
				snapE},
			expectedMissing: map[string]map[string]bool{
				"snap-b": {"acme/fooname": true},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, required missing.
				snapC,
				// covered by acme/barname validation-set, required missing.
				snapE},
			expectedMissing: map[string]map[string]bool{
				"snap-b": {"acme/fooname": true},
				"snap-d": {"acme/barname": true},
			},
		},
	}

	f := func(in map[string]map[string]*asserts.ValidationSet) map[string]map[string]bool {
		if len(in) == 0 {
			return nil
		}
		res := make(map[string]map[string]bool)
		for snapName, sets := range in {
			for setKey := range sets {
				if res[snapName] == nil {
					res[snapName] = make(map[string]bool)
				}
				res[snapName][setKey] = true
			}
		}
		return res
	}

	for i, tc := range tests {
		err := valsets.CheckInstalledSnaps(tc.snaps)
		if err == nil {
			c.Assert(tc.expectedInvalid, IsNil)
			c.Assert(tc.expectedMissing, IsNil)
			c.Assert(tc.expectedWrongRev, IsNil)
			continue
		}
		verr, ok := err.(*snapasserts.ValidationSetsValidationError)
		c.Assert(ok, Equals, true, Commentf("#%d", i))
		gotInvalid := f(verr.InvalidSnaps)
		gotMissing := f(verr.MissingSnaps)
		gotWrongRev := f(verr.WrongRevisionSnaps)

		c.Assert(tc.expectedInvalid, DeepEquals, gotInvalid, Commentf("#%d", i))
		c.Assert(tc.expectedMissing, DeepEquals, gotMissing, Commentf("#%d", i))
		c.Assert(tc.expectedWrongRev, DeepEquals, gotWrongRev, Commentf("#%d", i))
	}
}
