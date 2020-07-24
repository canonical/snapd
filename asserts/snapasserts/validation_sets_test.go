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
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8Valset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-other\)`},
		{[]*asserts.ValidationSet{mySnapAt8Valset, mySnapAt8OptValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8OptValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-opt\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapInvalidValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at revision 7 \(account-id/my-snap-ctl\)`},
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapAt7Valset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at revision 7 \(account-id/my-snap-ctl\)`},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapInvalidValset}, ""},
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapAt8OptValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7OptValset, mySnapAt8OptValset}, ""}, // no conflict but interpreted as invalid
		{[]*asserts.ValidationSet{mySnapAt7OptValset, mySnapAt8OptValset, mySnapAt7Valset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl,account-id/my-snap-ctl-opt2\), 8 \(account-id/my-snap-ctl-opt\)`},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapInvalidValset, mySnapAt7Valset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-opt\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapReqValset}, ""},
		{[]*asserts.ValidationSet{mySnapReqValset, mySnapAt7Valset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapReqValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapReqValset, mySnapAt7OptValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl-opt2\), 8 \(account-id/my-snap-ctl-opt\) or required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapAt7OptValset, mySnapReqValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl-opt2\), 8 \(account-id/my-snap-ctl-opt\) or required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapReqValset, mySnapInvalidValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapReqValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at any revision \(account-id/my-snap-ctl-req-only\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8Valset, mySnapOptValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-other\)`},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapOptValset}, ""},
		{[]*asserts.ValidationSet{mySnapOptValset, mySnapAt7Valset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapOptValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt8OptValset, mySnapOptValset, mySnapAt7OptValset}, ""}, // no conflict but interpreted as invalid
		{[]*asserts.ValidationSet{mySnapInvalidValset, mySnapOptValset}, ""},
		{[]*asserts.ValidationSet{mySnapOptValset, mySnapInvalidValset}, ""},
		{[]*asserts.ValidationSet{mySnapAt7Valset, mySnapAt8Valset, mySnapReqValset, mySnapInvalidValset}, `(?ms)validation sets are in conflict:.*cannot constraint snap "my-snap" as both invalid \(account-id/my-snap-ctl-inv\) and required at different revisions 7 \(account-id/my-snap-ctl\), 8 \(account-id/my-snap-ctl-other\) or at any revision \(account-id/my-snap-ctl-req-only\)`},
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
