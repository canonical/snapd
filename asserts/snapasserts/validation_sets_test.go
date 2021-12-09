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
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
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
	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1)),
	}
	err := valsets.CheckInstalledSnaps(snaps, nil)
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

	snapA := snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1))
	snapAlocal := snapasserts.NewInstalledSnap("snap-a", "", snap.R("x2"))
	snapB := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(3))
	snapBinvRev := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(8))
	snapBlocal := snapasserts.NewInstalledSnap("snap-b", "", snap.R("x3"))
	snapC := snapasserts.NewInstalledSnap("snap-c", "mysnapcccccccccccccccccccccccccc", snap.R(2))
	snapCinvRev := snapasserts.NewInstalledSnap("snap-c", "mysnapcccccccccccccccccccccccccc", snap.R(99))
	snapD := snapasserts.NewInstalledSnap("snap-d", "mysnapdddddddddddddddddddddddddd", snap.R(2))
	snapDrev99 := snapasserts.NewInstalledSnap("snap-d", "mysnapdddddddddddddddddddddddddd", snap.R(99))
	snapDlocal := snapasserts.NewInstalledSnap("snap-d", "", snap.R("x3"))
	snapE := snapasserts.NewInstalledSnap("snap-e", "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee", snap.R(2))
	// extra snap, not referenced by any validation set
	snapZ := snapasserts.NewInstalledSnap("snap-z", "mysnapzzzzzzzzzzzzzzzzzzzzzzzzzz", snap.R(1))

	tests := []struct {
		snaps            []*snapasserts.InstalledSnap
		expectedInvalid  map[string][]string
		expectedMissing  map[string][]string
		expectedWrongRev map[string]map[snap.Revision][]string
	}{
		{
			// required snaps not installed
			snaps: nil,
			expectedMissing: map[string][]string{
				"snap-b": {"acme/fooname"},
				"snap-d": {"acme/barname"},
			},
		},
		{
			// required snaps not installed
			snaps: []*snapasserts.InstalledSnap{
				snapZ,
			},
			expectedMissing: map[string][]string{
				"snap-b": {"acme/fooname"},
				"snap-d": {"acme/barname"},
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
			expectedInvalid: map[string][]string{
				"snap-a": {"acme/booname", "acme/fooname"},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname and acme/booname validation-sets, snapB missing, snap-a presence is invalid
				snapA,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99},
			expectedInvalid: map[string][]string{
				"snap-a": {"acme/booname", "acme/fooname"},
			},
			expectedMissing: map[string][]string{
				"snap-b": {"acme/fooname"},
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
			expectedWrongRev: map[string]map[snap.Revision][]string{
				"snap-c": {
					snap.R(2): {"acme/fooname"},
				},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set but wrong revision
				snapBinvRev,
				// covered by acme/barname validation-set.
				snapD},
			expectedWrongRev: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set
				snapB,
				// covered by acme/barname validation-set. snap-d not installed.
				snapE},
			expectedMissing: map[string][]string{
				"snap-d": {"acme/barname"},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// required snaps from acme/fooname are not installed.
				// covered by acme/barname validation-set
				snapDrev99,
				snapE},
			expectedMissing: map[string][]string{
				"snap-b": {"acme/fooname"},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, required missing.
				snapC,
				// covered by acme/barname validation-set, required missing.
				snapE},
			expectedMissing: map[string][]string{
				"snap-b": {"acme/fooname"},
				"snap-d": {"acme/barname"},
			},
		},
		// local snaps
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set.
				snapB,
				// covered by acme/barname validation-set, local snap-d.
				snapDlocal},
			// all fine
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, snap-a is invalid.
				snapAlocal,
				snapB,
				// covered by acme/barname validation-set.
				snapD},
			expectedInvalid: map[string][]string{
				"snap-a": {"acme/booname", "acme/fooname"},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, snap-b is wrong rev (local).
				snapBlocal,
				// covered by acme/barname validation-set.
				snapD},
			expectedWrongRev: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
			},
		},
	}

	checkSets := func(snapsToValidationSets map[string][]string, vs map[string]*asserts.ValidationSet) {
		for _, vsetKeys := range snapsToValidationSets {
			for _, key := range vsetKeys {
				vset, ok := vs[key]
				c.Assert(ok, Equals, true)
				c.Assert(vset.AccountID()+"/"+vset.Name(), Equals, key)
			}
		}
	}

	for i, tc := range tests {
		err := valsets.CheckInstalledSnaps(tc.snaps, nil)
		if err == nil {
			c.Assert(tc.expectedInvalid, IsNil)
			c.Assert(tc.expectedMissing, IsNil)
			c.Assert(tc.expectedWrongRev, IsNil)
			continue
		}
		verr, ok := err.(*snapasserts.ValidationSetsValidationError)
		c.Assert(ok, Equals, true, Commentf("#%d", i))
		c.Assert(tc.expectedInvalid, DeepEquals, verr.InvalidSnaps, Commentf("#%d", i))
		c.Assert(tc.expectedMissing, DeepEquals, verr.MissingSnaps, Commentf("#%d", i))
		c.Assert(tc.expectedWrongRev, DeepEquals, verr.WrongRevisionSnaps, Commentf("#%d", i))
		checkSets(verr.InvalidSnaps, verr.Sets)
	}
}

func (s *validationSetsSuite) TestCheckInstalledSnapsIgnoreValidation(c *C) {
	// require: snapB rev 3, snapC rev 2.
	// invalid: snapA
	vs := assertstest.FakeAssertion(map[string]interface{}{
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

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(vs), IsNil)

	snapA := snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1))
	snapB := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(3))
	snapBinvRev := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(8))

	// sanity check
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapA, snapB}, nil), ErrorMatches, "validation sets assertions are not met:\n"+
		"- invalid snaps:\n"+
		"  - snap-a \\(invalid for sets acme/fooname\\)")
	// snapA is invalid but ignore-validation is set so it's ok
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapA, snapB}, map[string]bool{"snap-a": true}), IsNil)

	// sanity check
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapBinvRev}, nil), ErrorMatches, "validation sets assertions are not met:\n"+
		"- snaps at wrong revisions:\n"+
		"  - snap-b \\(required at revision 3 by sets acme/fooname\\)")
	// snapB is at the wrong revision, but ignore-validation is set so it's ok
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapBinvRev}, map[string]bool{"snap-b": true}), IsNil)
}

func (s *validationSetsSuite) TestCheckInstalledSnapsErrorFormat(c *C) {
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
		},
	}).(*asserts.ValidationSet)
	vs2 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "barname",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-b",
				"id":       "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"revision": "5",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(vs1), IsNil)
	c.Assert(valsets.Add(vs2), IsNil)

	snapA := snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1))
	snapBlocal := snapasserts.NewInstalledSnap("snap-b", "", snap.R("x3"))

	tests := []struct {
		snaps    []*snapasserts.InstalledSnap
		errorMsg string
	}{
		{
			nil,
			"validation sets assertions are not met:\n" +
				"- missing required snaps:\n" +
				"  - snap-b \\(required by sets acme/barname,acme/fooname\\)",
		},
		{
			[]*snapasserts.InstalledSnap{snapA},
			"validation sets assertions are not met:\n" +
				"- missing required snaps:\n" +
				"  - snap-b \\(required by sets acme/barname,acme/fooname\\)\n" +
				"- invalid snaps:\n" +
				"  - snap-a \\(invalid for sets acme/fooname\\)",
		},
		{
			[]*snapasserts.InstalledSnap{snapBlocal},
			"validation sets assertions are not met:\n" +
				"- snaps at wrong revisions:\n" +
				"  - snap-b \\(required at revision 3 by sets acme/fooname, at revision 5 by sets acme/barname\\)",
		},
	}

	for i, tc := range tests {
		err := valsets.CheckInstalledSnaps(tc.snaps, nil)
		c.Assert(err, NotNil, Commentf("#%d", i))
		c.Assert(err, ErrorMatches, tc.errorMsg, Commentf("#%d: ", i))
	}
}

func (s *validationSetsSuite) TestSortByRevision(c *C) {
	revs := []snap.Revision{snap.R(10), snap.R(4), snap.R(5), snap.R(-1)}

	sort.Sort(snapasserts.ByRevision(revs))
	c.Assert(revs, DeepEquals, []snap.Revision{snap.R(-1), snap.R(4), snap.R(5), snap.R(10)})
}

func (s *validationSetsSuite) TestCheckPresenceRequired(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]interface{}{
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
			map[string]interface{}{
				"name":     "other-snap",
				"id":       "123456ididididididididididididid",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]interface{}{
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
			map[string]interface{}{
				"name":     "other-snap",
				"id":       "123456ididididididididididididid",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	// my-snap required but no specific revision set.
	valset3 := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl3",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	// no validation sets
	vsKeys, _, err := valsets.CheckPresenceRequired(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(vsKeys, HasLen, 0)

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)
	c.Assert(valsets.Add(valset3), IsNil)

	// sanity
	c.Assert(valsets.Conflict(), IsNil)

	vsKeys, rev, err := valsets.CheckPresenceRequired(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(rev, DeepEquals, snap.Revision{N: 7})
	c.Check(vsKeys, DeepEquals, []string{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2", "16/account-id/my-snap-ctl3/1"})

	vsKeys, rev, err = valsets.CheckPresenceRequired(naming.NewSnapRef("my-snap", "mysnapididididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(rev, DeepEquals, snap.Revision{N: 7})
	c.Check(vsKeys, DeepEquals, []string{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2", "16/account-id/my-snap-ctl3/1"})

	// other-snap is not required
	vsKeys, rev, err = valsets.CheckPresenceRequired(naming.Snap("other-snap"))
	c.Assert(err, ErrorMatches, `unexpected presence "invalid" for snap "other-snap"`)
	pr, ok := err.(*snapasserts.PresenceConstraintError)
	c.Assert(ok, Equals, true)
	c.Check(pr.SnapName, Equals, "other-snap")
	c.Check(pr.Presence, Equals, asserts.PresenceInvalid)
	c.Check(rev, DeepEquals, snap.Revision{N: 0})
	c.Check(vsKeys, HasLen, 0)

	// unknown snap is not required
	vsKeys, rev, err = valsets.CheckPresenceRequired(naming.NewSnapRef("unknown-snap", "00000000idididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(rev, DeepEquals, snap.Revision{N: 0})
	c.Check(vsKeys, HasLen, 0)

	// just one set, required but no revision specified
	valsets = snapasserts.NewValidationSets()
	c.Assert(valsets.Add(valset3), IsNil)
	vsKeys, rev, err = valsets.CheckPresenceRequired(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(rev, DeepEquals, snap.Revision{N: 0})
	c.Check(vsKeys, DeepEquals, []string{"16/account-id/my-snap-ctl3/1"})
}

func (s *validationSetsSuite) TestIsPresenceInvalid(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]interface{}{
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
				"presence": "invalid",
			},
			map[string]interface{}{
				"name":     "other-snap",
				"id":       "123456ididididididididididididid",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]interface{}{
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
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	// no validation sets
	vsKeys, err := valsets.CheckPresenceInvalid(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(vsKeys, HasLen, 0)

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	// sanity
	c.Assert(valsets.Conflict(), IsNil)

	// invalid in two sets
	vsKeys, err = valsets.CheckPresenceInvalid(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(vsKeys, DeepEquals, []string{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2"})

	vsKeys, err = valsets.CheckPresenceInvalid(naming.NewSnapRef("my-snap", "mysnapididididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(vsKeys, DeepEquals, []string{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2"})

	// other-snap isn't invalid
	vsKeys, err = valsets.CheckPresenceInvalid(naming.Snap("other-snap"))
	c.Assert(err, ErrorMatches, `unexpected presence "optional" for snap "other-snap"`)
	pr, ok := err.(*snapasserts.PresenceConstraintError)
	c.Assert(ok, Equals, true)
	c.Check(pr.SnapName, Equals, "other-snap")
	c.Check(pr.Presence, Equals, asserts.PresenceOptional)
	c.Check(vsKeys, HasLen, 0)

	vsKeys, err = valsets.CheckPresenceInvalid(naming.NewSnapRef("other-snap", "123456ididididididididididididid"))
	c.Assert(err, ErrorMatches, `unexpected presence "optional" for snap "other-snap"`)
	c.Check(vsKeys, HasLen, 0)

	// unknown snap isn't invalid
	vsKeys, err = valsets.CheckPresenceInvalid(naming.NewSnapRef("unknown-snap", "00000000idididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(vsKeys, HasLen, 0)
}
