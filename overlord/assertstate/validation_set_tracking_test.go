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

package assertstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/state"
)

type validationSetTrackingSuite struct {
	st          *state.State
	dev1Signing *assertstest.SigningDB
	dev1acct    *asserts.Account
}

var _ = Suite(&validationSetTrackingSuite{})

func (s *validationSetTrackingSuite) SetUpTest(c *C) {
	s.st = state.New(nil)

	s.st.Lock()
	defer s.st.Unlock()
	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(storeSigning.StoreAccountKey("")), IsNil)
	assertstate.ReplaceDB(s.st, db)

	s.dev1acct = assertstest.NewAccount(storeSigning, "developer1", nil, "")
	c.Assert(storeSigning.Add(s.dev1acct), IsNil)

	dev1PrivKey, _ = assertstest.GenerateKey(752)
	acct1Key := assertstest.NewAccountKey(storeSigning, s.dev1acct, nil, dev1PrivKey.PublicKey(), "")

	assertstatetest.AddMany(s.st, storeSigning.StoreAccountKey(""), s.dev1acct, acct1Key)

	s.dev1Signing = assertstest.NewSigningDB(s.dev1acct.AccountID(), dev1PrivKey)
	c.Check(s.dev1Signing, NotNil)
	c.Assert(storeSigning.Add(acct1Key), IsNil)
}

func (s *validationSetTrackingSuite) TestUpdate(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	for k, v := range all {
		c.Check(k, Equals, "foo/bar")
		c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "bar", Mode: assertstate.Enforce, PinnedAt: 1, Current: 2})
	}

	tr = assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Monitor,
		PinnedAt:  2,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	for k, v := range all {
		c.Check(k, Equals, "foo/bar")
		c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "bar", Mode: assertstate.Monitor, PinnedAt: 2, Current: 3})
	}

	tr = assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "baz",
		Mode:      assertstate.Enforce,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 2)

	var gotFirst, gotSecond bool
	for k, v := range all {
		if k == "foo/bar" {
			gotFirst = true
			c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "bar", Mode: assertstate.Monitor, PinnedAt: 2, Current: 3})
		} else {
			gotSecond = true
			c.Check(k, Equals, "foo/baz")
			c.Check(v, DeepEquals, &assertstate.ValidationSetTracking{AccountID: "foo", Name: "baz", Mode: assertstate.Enforce, PinnedAt: 0, Current: 3})
		}
	}
	c.Check(gotFirst, Equals, true)
	c.Check(gotSecond, Equals, true)
}

// there is a more extensive test for forget in assertstate_test.go.
func (s *validationSetTrackingSuite) TestForget(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// delete non-existing one is fine
	assertstate.ForgetValidationSet(s.st, "foo", "bar")
	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Monitor,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)

	// forget existing one
	assertstate.ForgetValidationSet(s.st, "foo", "bar")
	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)
}

func (s *validationSetTrackingSuite) TestGet(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	err := assertstate.GetValidationSet(s.st, "foo", "bar", nil)
	c.Assert(err, ErrorMatches, `internal error: tr is nil`)

	tr := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	var res assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(s.st, "foo", "bar", &res)
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, tr)

	// non-existing
	err = assertstate.GetValidationSet(s.st, "foo", "baz", &res)
	c.Assert(err, Equals, state.ErrNoState)
}

func (s *validationSetTrackingSuite) mockAssert(c *C, name, sequence, presence string) asserts.Assertion {
	snaps := []interface{}{map[string]interface{}{
		"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzz",
		"name":     "snap-b",
		"presence": presence,
	}}
	headers := map[string]interface{}{
		"authority-id": s.dev1acct.AccountID(),
		"account-id":   s.dev1acct.AccountID(),
		"name":         name,
		"series":       "16",
		"sequence":     sequence,
		"revision":     "5",
		"timestamp":    "2030-11-06T09:16:26Z",
		"snaps":        snaps,
	}
	as, err := s.dev1Signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	return as
}

func (s *validationSetTrackingSuite) TestEnforcedValidationSets(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	tr = assertstate.ValidationSetTracking{
		AccountID: s.dev1acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   3,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	tr = assertstate.ValidationSetTracking{
		AccountID: s.dev1acct.AccountID(),
		Name:      "baz",
		Mode:      assertstate.Monitor,
		Current:   5,
	}
	assertstate.UpdateValidationSet(s.st, &tr)

	vs1 := s.mockAssert(c, "foo", "2", "required")
	c.Assert(assertstate.Add(s.st, vs1), IsNil)

	vs2 := s.mockAssert(c, "bar", "1", "invalid")
	c.Assert(assertstate.Add(s.st, vs2), IsNil)

	vs3 := s.mockAssert(c, "baz", "5", "invalid")
	c.Assert(assertstate.Add(s.st, vs3), IsNil)

	valsets, err := assertstate.EnforcedValidationSets(s.st)
	c.Assert(err, IsNil)

	// foo and bar are in conflict, use this as an indirect way of checking
	// that both were added to valsets.
	// XXX: switch to CheckPresenceInvalid / CheckPresenceRequired once available.
	err = valsets.Conflict()
	c.Check(err, ErrorMatches, `validation sets are in conflict:\n- cannot constrain snap "snap-b" as both invalid \(.*/bar\) and required at any revision \(.*/foo\)`)
}

func (s *validationSetTrackingSuite) TestAddToValidationSetsHistory(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	tr1 := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.st, &tr1)
	tr2 := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "baz",
		Mode:      assertstate.Monitor,
		Current:   4,
	}
	assertstate.UpdateValidationSet(s.st, &tr2)

	c.Assert(assertstate.AddCurrentTrackingToValidationSetsHistory(s.st), IsNil)
	top, err := assertstate.ValidationSetsHistoryTop(s.st)
	c.Assert(err, IsNil)
	c.Check(top, DeepEquals, map[string]*assertstate.ValidationSetTracking{
		"foo/bar": {
			AccountID: "foo",
			Name:      "bar",
			Mode:      assertstate.Enforce,
			PinnedAt:  1,
			Current:   2,
		},
		"foo/baz": {
			AccountID: "foo",
			Name:      "baz",
			Mode:      assertstate.Monitor,
			Current:   4,
		},
	})

	// adding unchanged validation set tracking doesn't create another entry
	c.Assert(assertstate.AddCurrentTrackingToValidationSetsHistory(s.st), IsNil)
	top2, err := assertstate.ValidationSetsHistoryTop(s.st)
	c.Assert(err, IsNil)
	c.Check(top, DeepEquals, top2)
	vshist, err := assertstate.ValidationSetsHistory(s.st)
	c.Assert(err, IsNil)
	c.Check(vshist, HasLen, 1)

	tr3 := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "boo",
		Mode:      assertstate.Enforce,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.st, &tr3)
	c.Assert(assertstate.AddCurrentTrackingToValidationSetsHistory(s.st), IsNil)

	vshist, err = assertstate.ValidationSetsHistory(s.st)
	c.Assert(err, IsNil)
	// the history now has 2 entries
	c.Check(vshist, HasLen, 2)

	top3, err := assertstate.ValidationSetsHistoryTop(s.st)
	c.Assert(err, IsNil)
	c.Check(top3, DeepEquals, map[string]*assertstate.ValidationSetTracking{
		"foo/bar": {
			AccountID: "foo",
			Name:      "bar",
			Mode:      assertstate.Enforce,
			PinnedAt:  1,
			Current:   2,
		},
		"foo/baz": {
			AccountID: "foo",
			Name:      "baz",
			Mode:      assertstate.Monitor,
			Current:   4,
		},
		"foo/boo": {
			AccountID: "foo",
			Name:      "boo",
			Mode:      assertstate.Enforce,
			Current:   2,
		},
	})
}

func (s *validationSetTrackingSuite) TestAddToValidationSetsHistoryRemovesOldEntries(c *C) {
	restore := assertstate.MockMaxValidationSetsHistorySize(4)
	defer restore()

	s.st.Lock()
	defer s.st.Unlock()

	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 0)

	for i := 1; i <= 6; i++ {
		tr := assertstate.ValidationSetTracking{
			AccountID: "foo",
			Name:      "bar",
			Mode:      assertstate.Enforce,
			Current:   i,
		}
		assertstate.UpdateValidationSet(s.st, &tr)

		c.Assert(assertstate.AddCurrentTrackingToValidationSetsHistory(s.st), IsNil)
	}

	vshist, err := assertstate.ValidationSetsHistory(s.st)
	c.Assert(err, IsNil)

	// two first entries got dropped
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{
		{
			"foo/bar": {
				AccountID: "foo",
				Name:      "bar",
				Mode:      assertstate.Enforce,
				Current:   3,
			},
		},
		{
			"foo/bar": {
				AccountID: "foo",
				Name:      "bar",
				Mode:      assertstate.Enforce,
				Current:   4,
			},
		},
		{
			"foo/bar": {
				AccountID: "foo",
				Name:      "bar",
				Mode:      assertstate.Enforce,
				Current:   5,
			},
		},
		{
			"foo/bar": {
				AccountID: "foo",
				Name:      "bar",
				Mode:      assertstate.Enforce,
				Current:   6,
			},
		},
	})
}

func (s *validationSetTrackingSuite) TestRestoreValidationSetsTrackingNoHistory(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(assertstate.RestoreValidationSetsTracking(s.st), Equals, state.ErrNoState)
}

func (s *validationSetTrackingSuite) TestRestoreValidationSetsTracking(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	tr1 := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   2,
	}
	assertstate.UpdateValidationSet(s.st, &tr1)

	c.Assert(assertstate.AddCurrentTrackingToValidationSetsHistory(s.st), IsNil)

	all, err := assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)

	tr2 := assertstate.ValidationSetTracking{
		AccountID: "foo",
		Name:      "baz",
		Mode:      assertstate.Enforce,
		Current:   5,
	}
	assertstate.UpdateValidationSet(s.st, &tr2)

	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	// two validation sets are now tracked
	c.Check(all, DeepEquals, map[string]*assertstate.ValidationSetTracking{
		"foo/bar": &tr1,
		"foo/baz": &tr2,
	})

	// restore
	c.Assert(assertstate.RestoreValidationSetsTracking(s.st), IsNil)

	// and we're back at one validation set being tracked
	all, err = assertstate.ValidationSets(s.st)
	c.Assert(err, IsNil)
	c.Check(all, DeepEquals, map[string]*assertstate.ValidationSetTracking{
		"foo/bar": &tr1,
	})
}
