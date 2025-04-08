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
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type validationSetsSuite struct{}

var _ = Suite(&validationSetsSuite{})

func (s *validationSetsSuite) TestAddFromSameSequence(c *C) {
	mySnapAt7Valset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt8Valset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
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
	mySnapAt7Valset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt7Valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt8Valset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-other",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "8",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt8OptValset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-opt",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "optional",
				"revision": "8",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapInvalidValset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-inv",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapAt7OptValset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-opt2",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "optional",
				"revision": "7",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapReqValset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-req-only",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	mySnapOptValset := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl-opt-only",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
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
		snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1), nil),
	}
	err := valsets.CheckInstalledSnaps(snaps, nil)
	c.Assert(err, IsNil)
}

func (s *validationSetsSuite) TestCheckInstalledSnaps(c *C) {
	// require: snapB rev 3, snapC rev 2.
	// invalid: snapA
	vs1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "fooname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-a",
				"id":       "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"presence": "invalid",
			},
			map[string]any{
				"name":     "snap-b",
				"id":       "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"revision": "3",
				"presence": "required",
			},
			map[string]any{
				"name":     "snap-c",
				"id":       "mysnapcccccccccccccccccccccccccc",
				"revision": "2",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	// require: snapD any rev
	// optional: snapE any rev
	vs2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "barname",
		"sequence":     "3",
		"snaps": []any{
			map[string]any{
				"name":     "snap-d",
				"id":       "mysnapdddddddddddddddddddddddddd",
				"presence": "required",
			},
			map[string]any{
				"name":     "snap-e",
				"id":       "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	// optional: snapE any rev
	// note: since it only has an optional snap, acme/bazname is not expected
	// not be invalid by any of the checks.
	vs3 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "bazname",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "snap-e",
				"id":       "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	// invalid: snapA
	vs4 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "booname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-a",
				"id":       "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	vs5 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "huhname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-f",
				"id":       "mysnapffffffffffffffffffffffffff",
				"revision": "4",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	vs6 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "duhname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-f",
				"id":       "mysnapffffffffffffffffffffffffff",
				"revision": "4",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	vs7 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "bahname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-f",
				"id":       "mysnapffffffffffffffffffffffffff",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(vs1), IsNil)
	c.Assert(valsets.Add(vs2), IsNil)
	c.Assert(valsets.Add(vs3), IsNil)
	c.Assert(valsets.Add(vs4), IsNil)
	c.Assert(valsets.Add(vs5), IsNil)
	c.Assert(valsets.Add(vs6), IsNil)
	c.Assert(valsets.Add(vs7), IsNil)

	snapA := snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1), nil)
	snapAlocal := snapasserts.NewInstalledSnap("snap-a", "", snap.R("x2"), nil)
	snapB := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(3), nil)
	snapBinvRev := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(8), nil)
	snapBlocal := snapasserts.NewInstalledSnap("snap-b", "", snap.R("x3"), nil)
	snapC := snapasserts.NewInstalledSnap("snap-c", "mysnapcccccccccccccccccccccccccc", snap.R(2), nil)
	snapCinvRev := snapasserts.NewInstalledSnap("snap-c", "mysnapcccccccccccccccccccccccccc", snap.R(99), nil)
	snapD := snapasserts.NewInstalledSnap("snap-d", "mysnapdddddddddddddddddddddddddd", snap.R(2), nil)
	snapDrev99 := snapasserts.NewInstalledSnap("snap-d", "mysnapdddddddddddddddddddddddddd", snap.R(99), nil)
	snapDlocal := snapasserts.NewInstalledSnap("snap-d", "", snap.R("x3"), nil)
	snapE := snapasserts.NewInstalledSnap("snap-e", "mysnapeeeeeeeeeeeeeeeeeeeeeeeeee", snap.R(2), nil)
	snapF := snapasserts.NewInstalledSnap("snap-f", "mysnapffffffffffffffffffffffffff", snap.R(4), nil)
	// extra snap, not referenced by any validation set
	snapZ := snapasserts.NewInstalledSnap("snap-z", "mysnapzzzzzzzzzzzzzzzzzzzzzzzzzz", snap.R(1), nil)

	tests := []struct {
		snaps            []*snapasserts.InstalledSnap
		expectedInvalid  map[string][]string
		expectedMissing  map[string]map[snap.Revision][]string
		expectedWrongRev map[string]map[snap.Revision][]string
	}{
		{
			// required snaps not installed
			snaps: nil,
			expectedMissing: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
				"snap-d": {
					snap.R(0): {"acme/barname"},
				},
				"snap-f": {
					snap.R(0): {"acme/bahname"},
					snap.R(4): {"acme/duhname", "acme/huhname"},
				},
			},
		},
		{
			// required snaps not installed
			snaps: []*snapasserts.InstalledSnap{
				snapZ,
			},
			expectedMissing: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
				"snap-d": {
					snap.R(0): {"acme/barname"},
				},
				"snap-f": {
					snap.R(0): {"acme/bahname"},
					snap.R(4): {"acme/duhname", "acme/huhname"},
				},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set
				snapB,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			// ale fine
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set and acme/booname, snap-a presence is invalid
				snapA,
				snapB,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			expectedInvalid: map[string][]string{
				"snap-a": {"acme/booname", "acme/fooname"},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname and acme/booname validation-sets, snapB missing, snap-a presence is invalid
				snapA,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapDrev99,
				snapF,
			},
			expectedInvalid: map[string][]string{
				"snap-a": {"acme/booname", "acme/fooname"},
			},
			expectedMissing: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set
				snapB,
				snapC,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapD,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			// ale fine
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, snap-c optional but wrong revision
				snapB,
				snapCinvRev,
				// covered by acme/barname validation-set. snap-e not installed but optional
				snapD,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
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
				snapD,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
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
				snapE,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			expectedMissing: map[string]map[snap.Revision][]string{
				"snap-d": {
					snap.R(0): {"acme/barname"},
				},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// required snaps from acme/fooname are not installed.
				// covered by acme/barname validation-set
				snapDrev99,
				snapE,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			expectedMissing: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, required missing.
				snapC,
				// covered by acme/barname validation-set, required missing.
				snapE,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			expectedMissing: map[string]map[snap.Revision][]string{
				"snap-b": {
					snap.R(3): {"acme/fooname"},
				},
				"snap-d": {
					snap.R(0): {"acme/barname"},
				},
			},
		},
		// local snaps
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set.
				snapB,
				// covered by acme/barname validation-set, local snap-d.
				snapDlocal,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
			// all fine
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, snap-a is invalid.
				snapAlocal,
				snapB,
				// covered by acme/barname validation-set.
				snapD,
				snapF,
			},
			expectedInvalid: map[string][]string{
				"snap-a": {"acme/booname", "acme/fooname"},
			},
		},
		{
			snaps: []*snapasserts.InstalledSnap{
				// covered by acme/fooname validation-set, snap-b is wrong rev (local).
				snapBlocal,
				// covered by acme/barname validation-set.
				snapD,
				// covered by acme/duhname and acme/huhname
				snapF,
			},
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

	expectedSets := make(map[string]*asserts.ValidationSet, 7)
	for _, vs := range []*asserts.ValidationSet{vs1, vs2, vs3, vs4, vs5, vs6, vs7} {
		expectedSets[fmt.Sprintf("%s/%s", vs.AccountID(), vs.Name())] = vs
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
		c.Assert(verr.InvalidSnaps, DeepEquals, tc.expectedInvalid, Commentf("#%d", i))
		c.Assert(verr.MissingSnaps, DeepEquals, tc.expectedMissing, Commentf("#%d", i))
		c.Assert(verr.WrongRevisionSnaps, DeepEquals, tc.expectedWrongRev, Commentf("#%d", i))
		c.Assert(verr.Sets, DeepEquals, expectedSets)
		checkSets(verr.InvalidSnaps, verr.Sets)
	}
}

func (s *validationSetsSuite) TestCheckInstalledSnapsWithComponents(c *C) {
	sets := map[string][]any{
		"one": {
			map[string]any{
				"name":     "snap-a",
				"id":       snaptest.AssertedSnapID("snap-a"),
				"presence": "optional",
				"revision": "11",
				"components": map[string]any{
					"comp-1": map[string]any{
						"presence": "required",
						"revision": "2",
					},
					"comp-2": map[string]any{
						"presence": "optional",
						"revision": "3",
					},
				},
			},
		},
		"two": {
			map[string]any{
				"name":     "snap-a",
				"id":       snaptest.AssertedSnapID("snap-a"),
				"presence": "optional",
				"revision": "11",
				"components": map[string]any{
					"comp-2": map[string]any{
						"presence": "required",
						"revision": "3",
					},
				},
			},
		},
		"three": {
			map[string]any{
				"name":     "snap-a",
				"id":       snaptest.AssertedSnapID("snap-a"),
				"presence": "optional",
				"components": map[string]any{
					"comp-2": map[string]any{
						"presence": "required",
					},
				},
			},
		},
		"four": {
			map[string]any{
				"name":     "snap-a",
				"id":       snaptest.AssertedSnapID("snap-a"),
				"presence": "required",
				"revision": "13",
				"components": map[string]any{
					"comp-3": map[string]any{
						"presence": "required",
						"revision": "1",
					},
					"comp-4": map[string]any{
						"presence": "required",
						"revision": "2",
					},
					"comp-5": map[string]any{
						"presence": "optional",
						"revision": "3",
					},
				},
			},
		},
	}

	assertions := make(map[string]*asserts.ValidationSet)
	for name, set := range sets {
		headers := map[string]any{
			"type":         "validation-set",
			"authority-id": "acme",
			"series":       "16",
			"account-id":   "acme",
			"name":         name,
			"sequence":     "1",
			"snaps":        set,
		}
		assertions[name] = assertstest.FakeAssertion(headers).(*asserts.ValidationSet)
	}

	type test struct {
		summary          string
		assertions       []string
		installed        []*snapasserts.InstalledSnap
		verr             *snapasserts.ValidationSetsValidationError
		ignoreValidation map[string]bool
	}

	cases := []test{
		{
			summary:    "required component is installed for optional snap",
			assertions: []string{"one"},
			installed: []*snapasserts.InstalledSnap{
				snapasserts.NewInstalledSnap("snap-a", snaptest.AssertedSnapID("snap-a"), snap.R(11), []snapasserts.InstalledComponent{
					{
						ComponentRef: naming.NewComponentRef("snap-a", "comp-1"),
						Revision:     snap.R(2),
					},
				}),
				snapasserts.NewInstalledSnap("snap-b", snaptest.AssertedSnapID("snap-b"), snap.R(4), nil),
			},
			verr: nil,
		},
		{
			summary:    "required component is missing for optional snap that is installed",
			assertions: []string{"one"},
			installed: []*snapasserts.InstalledSnap{
				snapasserts.NewInstalledSnap("snap-a", snaptest.AssertedSnapID("snap-a"), snap.R(11), nil),
				snapasserts.NewInstalledSnap("snap-b", snaptest.AssertedSnapID("snap-b"), snap.R(4), nil),
			},
			verr: &snapasserts.ValidationSetsValidationError{
				Sets: map[string]*asserts.ValidationSet{
					"acme/one": assertions["one"],
				},
				ComponentErrors: map[string]*snapasserts.ValidationSetsComponentValidationError{
					"snap-a": {
						MissingComponents: map[string]map[snap.Revision][]string{
							"comp-1": {
								snap.R(2): {"acme/one"},
							},
						},
					},
				},
			},
		},
		{
			summary:    "required component is missing for optional snap that is not installed",
			assertions: []string{"one"},
			installed: []*snapasserts.InstalledSnap{
				snapasserts.NewInstalledSnap("snap-b", snaptest.AssertedSnapID("snap-b"), snap.R(4), nil),
			},
		},
		{
			summary:    "required component is missing for optional snap that is not installed",
			assertions: []string{"one"},
			installed: []*snapasserts.InstalledSnap{
				snapasserts.NewInstalledSnap("snap-b", snaptest.AssertedSnapID("snap-b"), snap.R(4), nil),
			},
		},
		{
			summary:    "missing required component that is optional in one set and required in another",
			assertions: []string{"one", "two"},
			installed: []*snapasserts.InstalledSnap{
				snapasserts.NewInstalledSnap("snap-a", snaptest.AssertedSnapID("snap-a"), snap.R(11), []snapasserts.InstalledComponent{
					{
						ComponentRef: naming.NewComponentRef("snap-a", "comp-1"),
						Revision:     snap.R(2),
					},
				}),
				snapasserts.NewInstalledSnap("snap-b", snaptest.AssertedSnapID("snap-b"), snap.R(4), nil),
			},
			verr: &snapasserts.ValidationSetsValidationError{
				Sets: map[string]*asserts.ValidationSet{
					"acme/one": assertions["one"],
					"acme/two": assertions["two"],
				},
				ComponentErrors: map[string]*snapasserts.ValidationSetsComponentValidationError{
					"snap-a": {
						MissingComponents: map[string]map[snap.Revision][]string{
							"comp-2": {
								snap.R(3): {"acme/one", "acme/two"},
							},
						},
					},
				},
			},
		},
		{
			summary:    "missing required component that is optional in one set and required in another (unspecified revision)",
			assertions: []string{"one", "three"},
			installed: []*snapasserts.InstalledSnap{
				snapasserts.NewInstalledSnap("snap-a", snaptest.AssertedSnapID("snap-a"), snap.R(11), []snapasserts.InstalledComponent{
					{
						ComponentRef: naming.NewComponentRef("snap-a", "comp-1"),
						Revision:     snap.R(2),
					},
				}),
				snapasserts.NewInstalledSnap("snap-b", snaptest.AssertedSnapID("snap-b"), snap.R(4), nil),
			},
			verr: &snapasserts.ValidationSetsValidationError{
				Sets: map[string]*asserts.ValidationSet{
					"acme/one":   assertions["one"],
					"acme/three": assertions["three"],
				},
				ComponentErrors: map[string]*snapasserts.ValidationSetsComponentValidationError{
					"snap-a": {
						MissingComponents: map[string]map[snap.Revision][]string{
							"comp-2": {
								snap.R(0): {"acme/three"},
								snap.R(3): {"acme/one"},
							},
						},
					},
				},
			},
		},
		{
			summary:    "missing required snap and required component",
			assertions: []string{"four"},
			verr: &snapasserts.ValidationSetsValidationError{
				Sets: map[string]*asserts.ValidationSet{
					"acme/four": assertions["four"],
				},
				ComponentErrors: map[string]*snapasserts.ValidationSetsComponentValidationError{
					"snap-a": {
						MissingComponents: map[string]map[snap.Revision][]string{
							"comp-3": {
								snap.R(1): {"acme/four"},
							},
							"comp-4": {
								snap.R(2): {"acme/four"},
							},
						},
					},
				},
				MissingSnaps: map[string]map[snap.Revision][]string{
					"snap-a": {
						snap.R(13): {"acme/four"},
					},
				},
			},
		},
	}

	for _, tc := range cases {
		prefix := fmt.Sprintf("test case: %s", tc.summary)

		valSets := snapasserts.NewValidationSets()
		for _, set := range tc.assertions {
			err := valSets.Add(assertions[set])
			c.Assert(err, IsNil, Commentf("%s: %s", prefix, err))
		}

		c.Check(valSets.Conflict(), IsNil, Commentf(prefix))

		err := valSets.CheckInstalledSnaps(tc.installed, tc.ignoreValidation)
		if tc.verr == nil {
			c.Assert(err, IsNil, Commentf("%s: unexpected error: %v", prefix, err))
		} else {
			verr, ok := err.(*snapasserts.ValidationSetsValidationError)
			c.Assert(ok, Equals, true, Commentf("%s: expected ValidationSetsValidationError, got: %v", prefix, err))
			c.Check(verr, DeepEquals, tc.verr, Commentf(prefix))
		}
	}
}

func (s *validationSetsSuite) TestValidationSetsValidationErrorStringWithComponents(c *C) {
	verr := &snapasserts.ValidationSetsValidationError{
		Sets: map[string]*asserts.ValidationSet{
			"acme/one":   nil, // can be nil for simplicity, not used in the test
			"acme/two":   nil,
			"acme/three": nil,
		},
		ComponentErrors: map[string]*snapasserts.ValidationSetsComponentValidationError{
			"snap-a": {
				MissingComponents: map[string]map[snap.Revision][]string{
					"comp-3": {
						snap.R(0): {"acme/one", "acme/two"},
					},
					"comp-4": {
						snap.R(2): {"acme/one"},
					},
				},
				InvalidComponents: map[string][]string{
					"comp-5": {"acme/two"},
				},
				WrongRevisionComponents: map[string]map[snap.Revision][]string{
					"comp-6": {
						snap.R(3): {"acme/three"},
					},
				},
			},
		},
		MissingSnaps: map[string]map[snap.Revision][]string{
			"snap-a": {
				snap.R(0): {"acme/one"},
			},
		},
	}

	const expected = `validation sets assertions are not met:
- missing required snaps:
  - snap-a (required at any revision by sets acme/one)
- missing required components:
  - snap-a+comp-3 (required at any revision by sets acme/one,acme/two)
  - snap-a+comp-4 (required at revision 2 by sets acme/one)
- invalid components:
  - snap-a+comp-5 (invalid for sets acme/two)
- components at wrong revisions:
  - snap-a+comp-6 (required at revision 3 by sets acme/three)`

	c.Check(verr.Error(), Equals, expected)
}

func (s *validationSetsSuite) TestCheckInstalledSnapsIgnoreValidation(c *C) {
	// require: snapB rev 3, snapC rev 2.
	// invalid: snapA
	vs := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "fooname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-a",
				"id":       "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"presence": "invalid",
			},
			map[string]any{
				"name":     "snap-b",
				"id":       "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"revision": "3",
				"presence": "required",
			},
			map[string]any{
				"name":     "snap-c",
				"id":       "mysnapcccccccccccccccccccccccccc",
				"revision": "2",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(vs), IsNil)

	snapA := snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1), nil)
	snapB := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(3), nil)
	snapBinvRev := snapasserts.NewInstalledSnap("snap-b", "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb", snap.R(8), nil)

	// validity check
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapA, snapB}, nil), ErrorMatches, "validation sets assertions are not met:\n"+
		"- invalid snaps:\n"+
		"  - snap-a \\(invalid for sets acme/fooname\\)")
	// snapA is invalid but ignore-validation is set so it's ok
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapA, snapB}, map[string]bool{"snap-a": true}), IsNil)

	// validity check
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapBinvRev}, nil), ErrorMatches, "validation sets assertions are not met:\n"+
		"- snaps at wrong revisions:\n"+
		"  - snap-b \\(required at revision 3 by sets acme/fooname\\)")
	// snapB is at the wrong revision, but ignore-validation is set so it's ok
	c.Check(valsets.CheckInstalledSnaps([]*snapasserts.InstalledSnap{snapBinvRev}, map[string]bool{"snap-b": true}), IsNil)
}

func (s *validationSetsSuite) TestCheckInstalledSnapsErrorFormat(c *C) {
	vs1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "fooname",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-a",
				"id":       "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa",
				"presence": "invalid",
			},
			map[string]any{
				"name":     "snap-b",
				"id":       "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"revision": "3",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)
	vs2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "acme",
		"series":       "16",
		"account-id":   "acme",
		"name":         "barname",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "snap-b",
				"id":       "mysnapbbbbbbbbbbbbbbbbbbbbbbbbbb",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(vs1), IsNil)
	c.Assert(valsets.Add(vs2), IsNil)

	// not strictly important, but ensures test data makes sense and avoids confusing results
	c.Assert(valsets.Conflict(), IsNil)

	snapA := snapasserts.NewInstalledSnap("snap-a", "mysnapaaaaaaaaaaaaaaaaaaaaaaaaaa", snap.R(1), nil)
	snapBlocal := snapasserts.NewInstalledSnap("snap-b", "", snap.R("x3"), nil)

	tests := []struct {
		snaps    []*snapasserts.InstalledSnap
		errorMsg string
	}{
		{
			nil,
			"validation sets assertions are not met:\n" +
				"- missing required snaps:\n" +
				"  - snap-b \\(required at any revision by sets acme/barname, at revision 3 by sets acme/fooname\\)",
		},
		{
			[]*snapasserts.InstalledSnap{snapA},
			"validation sets assertions are not met:\n" +
				"- missing required snaps:\n" +
				"  - snap-b \\(required at any revision by sets acme/barname, at revision 3 by sets acme/fooname\\)\n" +
				"- invalid snaps:\n" +
				"  - snap-a \\(invalid for sets acme/fooname\\)",
		},
		{
			[]*snapasserts.InstalledSnap{snapBlocal},
			"validation sets assertions are not met:\n" +
				"- snaps at wrong revisions:\n" +
				"  - snap-b \\(required at revision 3 by sets acme/fooname\\)",
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
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       "123456ididididididididididididid",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       "123456ididididididididididididid",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	// my-snap required but no specific revision set.
	valset3 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl3",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	// no validation sets
	presence, err := valsets.Presence(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(presence.Constrained(), Equals, false)

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)
	c.Assert(valsets.Add(valset3), IsNil)

	// validity
	c.Assert(valsets.Conflict(), IsNil)

	presence, err = valsets.Presence(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(presence.Revision, DeepEquals, snap.Revision{N: 7})
	c.Check(presence.Presence, DeepEquals, asserts.PresenceRequired)
	c.Check(presence.Sets, DeepEquals, snapasserts.ValidationSetKeySlice{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2", "16/account-id/my-snap-ctl3/1"})

	presence, err = valsets.Presence(naming.NewSnapRef("my-snap", "mysnapididididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(presence.Revision, DeepEquals, snap.Revision{N: 7})
	c.Check(presence.Presence, DeepEquals, asserts.PresenceRequired)
	c.Check(presence.Sets, DeepEquals, snapasserts.ValidationSetKeySlice{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2", "16/account-id/my-snap-ctl3/1"})

	// unknown snap is not required
	presence, err = valsets.Presence(naming.NewSnapRef("unknown-snap", "00000000idididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(presence.Constrained(), Equals, false)

	// just one set, required but no revision specified
	valsets = snapasserts.NewValidationSets()
	c.Assert(valsets.Add(valset3), IsNil)
	presence, err = valsets.Presence(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(presence.Revision, DeepEquals, snap.Revision{N: 0})
	c.Check(presence.Presence, DeepEquals, asserts.PresenceRequired)
	c.Check(presence.Sets, DeepEquals, snapasserts.ValidationSetKeySlice{"16/account-id/my-snap-ctl3/1"})
}

func (s *validationSetsSuite) TestIsPresenceInvalid(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "invalid",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       "123456ididididididididididididid",
				"presence": "optional",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	// no validation sets
	presence, err := valsets.Presence(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(presence.Constrained(), Equals, false)

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	// validity
	c.Assert(valsets.Conflict(), IsNil)

	// invalid in two sets
	presence, err = valsets.Presence(naming.Snap("my-snap"))
	c.Assert(err, IsNil)
	c.Check(presence.Sets, DeepEquals, snapasserts.ValidationSetKeySlice{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2"})

	presence, err = valsets.Presence(naming.NewSnapRef("my-snap", "mysnapididididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(presence.Sets, DeepEquals, snapasserts.ValidationSetKeySlice{"16/account-id/my-snap-ctl/1", "16/account-id/my-snap-ctl2/2"})

	// unknown snap isn't constrained
	presence, err = valsets.Presence(naming.NewSnapRef("unknown-snap", "00000000idididididididididididid"))
	c.Assert(err, IsNil)
	c.Check(presence.Constrained(), Equals, false)
}

func (s *validationSetsSuite) TestParseValidationSet(c *C) {
	for _, tc := range []struct {
		input    string
		errMsg   string
		account  string
		name     string
		sequence int
	}{
		{
			input:   "foo/bar",
			account: "foo",
			name:    "bar",
		},
		{
			input:    "foo/bar=9",
			account:  "foo",
			name:     "bar",
			sequence: 9,
		},
		{
			input:  "foo",
			errMsg: `cannot parse validation set "foo": expected a single account/name`,
		},
		{
			input:  "foo/bar/baz",
			errMsg: `cannot parse validation set "foo/bar/baz": expected a single account/name`,
		},
		{
			input:  "",
			errMsg: `cannot parse validation set "": expected a single account/name`,
		},
		{
			input:  "foo=1",
			errMsg: `cannot parse validation set "foo=1": expected a single account/name`,
		},
		{
			input:  "foo/bar=x",
			errMsg: `cannot parse validation set "foo/bar=x": invalid sequence: strconv.Atoi: parsing "x": invalid syntax`,
		},
		{
			input:  "foo=bar=",
			errMsg: `cannot parse validation set "foo=bar=": expected account/name=seq`,
		},
		{
			input:  "$foo/bar",
			errMsg: `cannot parse validation set "\$foo/bar": invalid account ID "\$foo"`,
		},
		{
			input:  "foo/$bar",
			errMsg: `cannot parse validation set "foo/\$bar": invalid validation set name "\$bar"`,
		},
	} {
		account, name, seq, err := snapasserts.ParseValidationSet(tc.input)
		if tc.errMsg != "" {
			c.Assert(err, ErrorMatches, tc.errMsg)
		} else {
			c.Assert(err, IsNil)
		}
		c.Check(account, Equals, tc.account)
		c.Check(name, Equals, tc.name)
		c.Check(seq, Equals, tc.sequence)
	}
}

func (s *validationSetsSuite) TestValidationSetKeyFormat(c *C) {
	series, acc, name := "a", "b", "c"
	sequence := 1

	valSet := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": acc,
		"series":       series,
		"account-id":   acc,
		"name":         name,
		"sequence":     strconv.Itoa(sequence),
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valSetKey := snapasserts.NewValidationSetKey(valSet)
	c.Assert(valSetKey.String(), Equals, fmt.Sprintf("%s/%s/%s/%d", series, acc, name, sequence))
}

func (s *validationSetsSuite) TestValidationSetKeySliceSort(c *C) {
	valSets := snapasserts.ValidationSetKeySlice([]snapasserts.ValidationSetKey{"1/a/a/1", "1/a/b/1", "1/a/b/2", "2/a/a/1", "2/a/a/2", "a/a/a/1"})
	rand.Shuffle(len(valSets), func(x, y int) {
		valSets[x], valSets[y] = valSets[y], valSets[x]
	})

	sort.Sort(valSets)
	c.Assert(valSets, DeepEquals, snapasserts.ValidationSetKeySlice([]snapasserts.ValidationSetKey{"1/a/a/1", "1/a/b/1", "1/a/b/2", "2/a/a/1", "2/a/a/2", "a/a/a/1"}))
}

func (s *validationSetsSuite) TestValidationSetKeySliceCommaSeparated(c *C) {
	valSets := snapasserts.ValidationSetKeySlice([]snapasserts.ValidationSetKey{"1/a/a/1", "1/a/b/1", "1/a/b/2", "2/a/a/1"})
	c.Assert(valSets.CommaSeparated(), Equals, "1/a/a/1,1/a/b/1,1/a/b/2,2/a/a/1")
}

func (s *validationSetsSuite) TestValidationSetKeyComponents(c *C) {
	valsetKey := snapasserts.NewValidationSetKey(assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"series":       "a",
		"authority-id": "b",
		"account-id":   "b",
		"name":         "c",
		"sequence":     "13",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet))
	c.Assert(valsetKey.Components(), DeepEquals, []string{"a", "b", "c", "13"})
}

func (s *validationSetsSuite) TestRevisions(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       snaptest.AssertedSnapID("my-snap"),
				"presence": "optional",
				"revision": "10",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "required",
			},
			// invalid snap should not be present in the result of (*ValidationSets).Revisions()
			map[string]any{
				"name":     "invalid-snap",
				"id":       snaptest.AssertedSnapID("invalid-snap"),
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "optional",
				"revision": "11",
			},
			map[string]any{
				"name":     "another-snap",
				"id":       snaptest.AssertedSnapID("another-snap"),
				"presence": "required",
				"revision": "12",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	// no validation sets
	revisions, err := valsets.Revisions()
	c.Assert(err, IsNil)
	c.Check(revisions, HasLen, 0)

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	// validity
	c.Assert(valsets.Conflict(), IsNil)

	revisions, err = valsets.Revisions()
	c.Assert(err, IsNil)
	c.Check(revisions, HasLen, 3)

	c.Check(revisions, DeepEquals, map[string]snap.Revision{
		"my-snap":      snap.R(10),
		"other-snap":   snap.R(11),
		"another-snap": snap.R(12),
	})
}

func (s *validationSetsSuite) TestCanBePresent(c *C) {
	var snaps []*asserts.ValidationSetSnap
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       snaptest.AssertedSnapID("my-snap"),
				"presence": "invalid",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	snaps = append(snaps, valset1.Snaps()...)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "optional",
			},
			map[string]any{
				"name":     "another-snap",
				"id":       snaptest.AssertedSnapID("another-snap"),
				"presence": "invalid",
			},
		},
	}).(*asserts.ValidationSet)

	snaps = append(snaps, valset2.Snaps()...)

	valsets := snapasserts.NewValidationSets()

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	// validity
	c.Assert(valsets.Conflict(), IsNil)

	for _, sn := range snaps {
		c.Check(valsets.CanBePresent(sn), Equals, sn.Presence != asserts.PresenceInvalid)
	}
}

func (s *validationSetsSuite) TestKeys(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps":        []any{},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps":        []any{},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	c.Check(valsets.Keys(), DeepEquals, []snapasserts.ValidationSetKey{
		"16/account-id/my-snap-ctl/1",
		"16/account-id/my-snap-ctl2/2",
	})
}

func (s *validationSetsSuite) TestEmpty(c *C) {
	a := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps":        []any{},
	}).(*asserts.ValidationSet)

	vsets := snapasserts.NewValidationSets()
	c.Assert(vsets.Empty(), Equals, true)
	vsets.Add(a)
	c.Assert(vsets.Empty(), Equals, false)
}

func (s *validationSetsSuite) TestRequiredSnapNames(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       snaptest.AssertedSnapID("my-snap"),
				"presence": "invalid",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "optional",
			},
			map[string]any{
				"name":     "another-snap",
				"id":       snaptest.AssertedSnapID("another-snap"),
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	c.Check(valsets.RequiredSnaps(), testutil.DeepUnsortedMatches, []string{
		"other-snap",
		"another-snap",
	})
}

func (s *validationSetsSuite) TestRevisionsConflict(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       snaptest.AssertedSnapID("my-snap"),
				"presence": "required",
				"revision": "10",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       snaptest.AssertedSnapID("my-snap"),
				"presence": "required",
				"revision": "11",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()
	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	_, err := valsets.Revisions()
	c.Assert(err, testutil.ErrorIs, &snapasserts.ValidationSetsConflictError{})
}

func (s *validationSetsSuite) TestComponentConflicts(c *C) {
	type test struct {
		summary string
		sets    []*asserts.ValidationSet
		message string
	}

	cases := []test{{
		summary: "component revision conflict",
		sets: []*asserts.ValidationSet{assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "one",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "required",
						"revision": "10",
						"components": map[string]any{
							"comp-2": map[string]any{
								"presence": "optional",
								"revision": "3",
							},
						},
					},
				},
			}).(*asserts.ValidationSet), assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "two",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "required",
						"revision": "10",
						"components": map[string]any{
							"comp-2": map[string]any{
								"presence": "required",
								"revision": "2",
							},
						},
					},
				},
			}).(*asserts.ValidationSet),
		},
		message: `cannot constrain component "snap-a+comp-2" at different revisions 2 (account-id/two), 3 (account-id/one)`,
	}, {
		summary: "component presence conflict",
		sets: []*asserts.ValidationSet{assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "one",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "optional",
						"components": map[string]any{
							"comp-2": map[string]any{
								"presence": "invalid",
							},
						},
					},
				},
			}).(*asserts.ValidationSet), assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "two",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "optional",
						"components": map[string]any{
							"comp-2": map[string]any{
								"presence": "required",
							},
						},
					},
				},
			}).(*asserts.ValidationSet),
		},
		message: `cannot constrain component "snap-a+comp-2" as both invalid (account-id/one) and required at any revision (account-id/two)`,
	}, {
		summary: "component presence conflict and snap presence conflict",
		sets: []*asserts.ValidationSet{assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "one",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "required",
						"components": map[string]any{
							"comp-2": map[string]any{
								"presence": "invalid",
							},
						},
					},
				},
			}).(*asserts.ValidationSet), assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "two",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "optional",
						"components": map[string]any{
							"comp-2": map[string]any{
								"presence": "required",
							},
						},
					},
				},
			}).(*asserts.ValidationSet), assertstest.FakeAssertion(
			map[string]any{
				"type":         "validation-set",
				"authority-id": "account-id",
				"series":       "16",
				"account-id":   "account-id",
				"name":         "three",
				"sequence":     "1",
				"snaps": []any{
					map[string]any{
						"name":     "snap-a",
						"id":       snaptest.AssertedSnapID("snap-a"),
						"presence": "invalid",
					},
				},
			}).(*asserts.ValidationSet),
		},
		message: `cannot constrain component "snap-a+comp-2" as both invalid (account-id/one) and required at any revision (account-id/two)`,
	}}

	for _, tc := range cases {
		prefix := fmt.Sprintf("test case: %s", tc.summary)

		valsets := snapasserts.NewValidationSets()
		for _, set := range tc.sets {
			c.Check(valsets.Add(set), IsNil, Commentf(prefix))
		}

		err := valsets.Conflict()
		c.Check(strings.Count(err.Error(), tc.message), Equals, 1, Commentf(prefix))
	}
}

func (s *validationSetsSuite) TestValidationSetsConflictErrorIs(c *C) {
	err := &snapasserts.ValidationSetsConflictError{}

	c.Check(err.Is(&snapasserts.ValidationSetsConflictError{}), Equals, true)
	c.Check(err.Is(errors.New("other error")), Equals, false)

	wrapped := fmt.Errorf("wrapped error: %w", err)
	c.Check(wrapped, testutil.ErrorIs, &snapasserts.ValidationSetsConflictError{})
}

func (s *validationSetsSuite) TestSets(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps":        []any{},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps":        []any{},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	sets := valsets.Sets()
	c.Assert(sets, testutil.DeepUnsortedMatches, []*asserts.ValidationSet{valset1, valset2})
}

func (s *validationSetsSuite) TestSnapConstrained(c *C) {
	valset1 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "my-snap",
				"id":       snaptest.AssertedSnapID("my-snap"),
				"presence": "invalid",
			},
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valset2 := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "my-snap-ctl2",
		"sequence":     "2",
		"snaps": []any{
			map[string]any{
				"name":     "other-snap",
				"id":       snaptest.AssertedSnapID("other-snap"),
				"presence": "optional",
			},
			map[string]any{
				"name":     "another-snap",
				"id":       snaptest.AssertedSnapID("another-snap"),
				"presence": "required",
			},
		},
	}).(*asserts.ValidationSet)

	valsets := snapasserts.NewValidationSets()

	c.Assert(valsets.Add(valset1), IsNil)
	c.Assert(valsets.Add(valset2), IsNil)

	for _, name := range []string{"my-snap", "other-snap", "another-snap"} {
		c.Check(valsets.SnapConstrained(&asserts.ModelSnap{
			Name: name,
		}), Equals, true)
	}

	c.Check(valsets.SnapConstrained(&asserts.ModelSnap{
		Name: "unknown-snap",
	}), Equals, false)
}

func (s *validationSetsSuite) TestSnapPresence(c *C) {
	one := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "one",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-1",
				"id":       snaptest.AssertedSnapID("snap-1"),
				"presence": "invalid",
			},
			map[string]any{
				"name":     "snap-2",
				"id":       snaptest.AssertedSnapID("snap-2"),
				"presence": "required",
			},
			map[string]any{
				"name":     "snap-3",
				"id":       snaptest.AssertedSnapID("snap-3"),
				"presence": "optional",
				"components": map[string]any{
					"comp-4": map[string]any{
						"presence": "required",
					},
				},
			},
		},
	}).(*asserts.ValidationSet)

	two := assertstest.FakeAssertion(map[string]any{
		"type":         "validation-set",
		"authority-id": "account-id",
		"series":       "16",
		"account-id":   "account-id",
		"name":         "two",
		"sequence":     "1",
		"snaps": []any{
			map[string]any{
				"name":     "snap-2",
				"id":       snaptest.AssertedSnapID("snap-2"),
				"presence": "optional",
				"revision": "2",
				"components": map[string]any{
					"comp-2": map[string]any{
						"presence": "required",
						"revision": "22",
					},
					"comp-3": map[string]any{
						"presence": "invalid",
					},
				},
			},
		},
	}).(*asserts.ValidationSet)

	sets := snapasserts.NewValidationSets()

	c.Assert(sets.Add(one), IsNil)
	c.Assert(sets.Add(two), IsNil)

	onePresence, err := sets.Presence(naming.Snap("snap-1"))
	c.Assert(err, IsNil)

	oneExpected := snapasserts.NewSnapPresenceConstraints(snapasserts.PresenceConstraint{
		Presence: asserts.PresenceInvalid,
		Revision: snap.R(-1),
		Sets:     []snapasserts.ValidationSetKey{"16/account-id/one/1"},
	}, make(map[string]snapasserts.PresenceConstraint))
	c.Check(onePresence, DeepEquals, oneExpected)
	c.Check(onePresence.Constrained(), Equals, true)

	twoPresence, err := sets.Presence(naming.Snap("snap-2"))
	c.Assert(err, IsNil)

	twoExpected := snapasserts.NewSnapPresenceConstraints(snapasserts.PresenceConstraint{
		Presence: asserts.PresenceRequired,
		Revision: snap.R(2),
		Sets:     []snapasserts.ValidationSetKey{"16/account-id/one/1", "16/account-id/two/1"},
	}, map[string]snapasserts.PresenceConstraint{
		"comp-2": {
			Presence: asserts.PresenceRequired,
			Revision: snap.R(22),
			Sets:     []snapasserts.ValidationSetKey{"16/account-id/two/1"},
		},
		"comp-3": {
			Presence: asserts.PresenceInvalid,
			Revision: snap.R(-1),
			Sets:     []snapasserts.ValidationSetKey{"16/account-id/two/1"},
		},
	})
	c.Check(twoPresence, DeepEquals, twoExpected)
	c.Check(twoPresence.Constrained(), Equals, true)

	c.Check(twoExpected.Component("comp-2"), DeepEquals, snapasserts.PresenceConstraint{
		Presence: asserts.PresenceRequired,
		Revision: snap.R(22),
		Sets:     []snapasserts.ValidationSetKey{"16/account-id/two/1"},
	})

	c.Check(twoExpected.Component("comp-4"), DeepEquals, snapasserts.PresenceConstraint{
		Presence: asserts.PresenceOptional,
	})

	c.Check(twoExpected.RequiredComponents(), DeepEquals, map[string]snapasserts.PresenceConstraint{
		"comp-2": {
			Presence: asserts.PresenceRequired,
			Revision: snap.R(22),
			Sets:     []snapasserts.ValidationSetKey{"16/account-id/two/1"},
		},
	})

	threePresence, err := sets.Presence(naming.Snap("snap-3"))
	c.Assert(err, IsNil)

	threeExpected := snapasserts.NewSnapPresenceConstraints(snapasserts.PresenceConstraint{
		Presence: asserts.PresenceOptional,
		Revision: snap.R(0),
		Sets:     []snapasserts.ValidationSetKey{"16/account-id/one/1"},
	}, map[string]snapasserts.PresenceConstraint{
		"comp-4": {
			Presence: asserts.PresenceRequired,
			Sets:     []snapasserts.ValidationSetKey{"16/account-id/one/1"},
		},
	})
	c.Check(threePresence, DeepEquals, threeExpected)
	c.Check(threePresence.Constrained(), Equals, true)

	fourPresence, err := sets.Presence(naming.Snap("snap-4"))
	c.Assert(err, IsNil)

	fourExpected := snapasserts.NewSnapPresenceConstraints(snapasserts.PresenceConstraint{
		Presence: asserts.PresenceOptional,
	}, nil)
	c.Check(fourPresence, DeepEquals, fourExpected)
	c.Check(fourPresence.Constrained(), Equals, false)

	c.Check(fourExpected.Component("anything"), DeepEquals, snapasserts.PresenceConstraint{
		Presence: asserts.PresenceOptional,
	})
}
