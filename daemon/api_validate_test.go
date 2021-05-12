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

package daemon_test

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

var _ = check.Suite(&apiValidationSetsSuite{})

type apiValidationSetsSuite struct {
	apiBaseSuite

	storeSigning              *assertstest.StoreStack
	dev1Signing               *assertstest.SigningDB
	dev1acct                  *asserts.Account
	acct1Key                  *asserts.AccountKey
	mockSeqFormingAssertionFn func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error)
}

type byName []*snapasserts.InstalledSnap

func (b byName) Len() int      { return len(b) }
func (b byName) Swap(i, j int) { b[i], b[j] = b[j], b[i] }
func (b byName) Less(i, j int) bool {
	return b[i].SnapName() < b[j].SnapName()
}

func (s *apiValidationSetsSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)
	d := s.daemon(c)

	restore := asserts.MockMaxSupportedFormat(asserts.ValidationSetType, 1)
	s.AddCleanup(restore)

	s.mockSeqFormingAssertionFn = nil

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)

	st := d.Overlord().State()
	st.Lock()
	snapstate.ReplaceStore(st, s)
	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
	st.Unlock()

	s.dev1acct = assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	c.Assert(s.storeSigning.Add(s.dev1acct), check.IsNil)

	// developer signing
	dev1PrivKey, _ := assertstest.GenerateKey(752)
	s.acct1Key = assertstest.NewAccountKey(s.storeSigning, s.dev1acct, nil, dev1PrivKey.PublicKey(), "")

	s.dev1Signing = assertstest.NewSigningDB(s.dev1acct.AccountID(), dev1PrivKey)
	c.Assert(s.storeSigning.Add(s.acct1Key), check.IsNil)

	d.Overlord().Loop()
	s.AddCleanup(func() { d.Overlord().Stop() })
}

func (s *apiValidationSetsSuite) mockValidationSetsTracking(st *state.State) {
	st.Set("validation-sets", map[string]interface{}{
		fmt.Sprintf("%s/foo", s.dev1acct.AccountID()): map[string]interface{}{
			"account-id": s.dev1acct.AccountID(),
			"name":       "foo",
			"mode":       assertstate.Enforce,
			"pinned-at":  9,
			"current":    99,
		},
		fmt.Sprintf("%s/baz", s.dev1acct.AccountID()): map[string]interface{}{
			"account-id": s.dev1acct.AccountID(),
			"name":       "baz",
			"mode":       assertstate.Monitor,
			"pinned-at":  0,
			"current":    2,
		},
	})
}

func (s *apiValidationSetsSuite) mockAssert(c *check.C, name, sequence string) asserts.Assertion {
	snaps := []interface{}{map[string]interface{}{
		"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzz",
		"name":     "snap-b",
		"presence": "required",
		"revision": "1",
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
	vs, err := s.dev1Signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, check.IsNil)
	return vs
}

func (s *apiValidationSetsSuite) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	s.pokeStateLock()
	return s.mockSeqFormingAssertionFn(assertType, sequenceKey, sequence, user)
}

func (s *apiValidationSetsSuite) TestQueryValidationSetsErrors(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		return nil, &asserts.NotFoundError{
			Type: assertType,
		}
	}

	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)
	st.Unlock()

	for i, tc := range []struct {
		validationSet string
		// sequence is normally an int, use string for passing invalid ones.
		sequence string
		message  string
		status   int
	}{
		{
			validationSet: "abc/Xfoo",
			message:       `invalid name "Xfoo"`,
			status:        400,
		},
		{
			validationSet: "Xfoo/bar",
			message:       `invalid account ID "Xfoo"`,
			status:        400,
		},
		{
			validationSet: "foo/foo",
			message:       "validation set not found",
			status:        404,
		},
		{
			validationSet: "foo/bar",
			sequence:      "1999",
			message:       "validation set not found",
			status:        404,
		},
		{
			validationSet: "foo/bar",
			sequence:      "x",
			message:       "invalid sequence argument",
			status:        400,
		},
		{
			validationSet: "foo/bar",
			sequence:      "-2",
			message:       "invalid sequence argument: -2",
			status:        400,
		},
	} {
		q := url.Values{}
		if tc.sequence != "" {
			q.Set("sequence", tc.sequence)
		}
		req, err := http.NewRequest("GET", fmt.Sprintf("/v2/validation-sets/%s?%s", tc.validationSet, q.Encode()), nil)
		c.Assert(err, check.IsNil)
		rsp := s.errorReq(c, req, nil)
		c.Check(rsp.Status, check.Equals, tc.status, check.Commentf("case #%d", i))
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.message)
	}
}

func (s *apiValidationSetsSuite) TestGetValidationSetsNone(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/validation-sets", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]daemon.ValidationSetResult)
	c.Check(res, check.HasLen, 0)
}

func (s *apiValidationSetsSuite) TestListValidationSets(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key)
	as := s.mockAssert(c, "foo", "9")
	err := assertstate.Add(st, as)
	c.Check(err, check.IsNil)
	as = s.mockAssert(c, "baz", "2")
	err = assertstate.Add(st, as)
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/validation-sets", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.([]daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, []daemon.ValidationSetResult{
		{
			AccountID: s.dev1acct.AccountID(),
			Name:      "baz",
			Mode:      "monitor",
			Sequence:  2,
			Valid:     false,
		},
		{
			AccountID: s.dev1acct.AccountID(),
			Name:      "foo",
			PinnedAt:  9,
			Mode:      "enforce",
			Sequence:  99,
			Valid:     false,
		},
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetOne(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		return nil, &asserts.NotFoundError{
			Type: assertType,
		}
	}

	st := s.d.Overlord().State()
	st.Lock()
	as := s.mockAssert(c, "baz", "2")
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key, as)
	s.mockValidationSetsTracking(st)
	st.Unlock()

	req, err := http.NewRequest("GET", fmt.Sprintf("/v2/validation-sets/%s/baz", s.dev1acct.AccountID()), nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: s.dev1acct.AccountID(),
		Name:      "baz",
		Mode:      "monitor",
		Sequence:  2,
		Valid:     false,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetPinned(c *check.C) {
	q := url.Values{}
	q.Set("sequence", "9")
	req, err := http.NewRequest("GET", fmt.Sprintf("/v2/validation-sets/%s/foo?%s", s.dev1acct.AccountID(), q.Encode()), nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	as := s.mockAssert(c, "foo", "9")
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key, as)
	s.mockValidationSetsTracking(st)
	st.Unlock()
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: s.dev1acct.AccountID(),
		Name:      "foo",
		PinnedAt:  9,
		Mode:      "enforce",
		Sequence:  99,
		Valid:     false,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetNotFound(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		return nil, &asserts.NotFoundError{
			Type: assertType,
		}
	}

	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/other", nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 404)
	res := rsp.Result.(*daemon.ErrorResult)
	c.Assert(res, check.NotNil)
	c.Check(string(res.Kind), check.Equals, "validation-set-not-found")
	c.Check(res.Value, check.DeepEquals, map[string]interface{}{
		"account-id": "foo",
		"name":       "other",
	})
}

var validationSetAssertion = []byte("type: validation-set\n" +
	"format: 1\n" +
	"authority-id: foo\n" +
	"account-id: foo\n" +
	"name: other\n" +
	"sequence: 2\n" +
	"revision: 5\n" +
	"series: 16\n" +
	"snaps:\n" +
	"  -\n" +
	"    id: yOqKhntON3vR7kwEbVPsILm7bUViPDzz\n" +
	"    name: snap-b\n" +
	"    presence: required\n" +
	"    revision: 1\n" +
	"timestamp: 2020-11-06T09:16:26Z\n" +
	"sign-key-sha3-384: 7bbncP0c4RcufwReeiylCe0S7IMCn-tHLNSCgeOVmV3K-7_MzpAHgJDYeOjldefE\n\n" +
	"AXNpZw==")

func (s *apiValidationSetsSuite) TestGetValidationSetLatestFromRemote(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		c.Assert(assertType, check.NotNil)
		c.Assert(assertType.Name, check.Equals, "validation-set")
		// no sequence number element, querying the latest
		c.Assert(sequenceKey, check.DeepEquals, []string{"16", "foo", "other"})
		c.Assert(sequence, check.Equals, 0)
		as, err := asserts.Decode(validationSetAssertion)
		c.Assert(err, check.IsNil)
		// sanity
		c.Assert(as.Type().Name, check.Equals, "validation-set")
		return as, nil
	}

	restore := daemon.MockCheckInstalledSnaps(func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap) error {
		c.Assert(vsets, check.NotNil)
		sort.Sort(byName(snaps))
		c.Assert(snaps, check.DeepEquals, []*snapasserts.InstalledSnap{
			{
				SnapRef:  naming.NewSnapRef("snap-a", "snapaid"),
				Revision: snap.R(2),
			},
			{
				SnapRef:  naming.NewSnapRef("snap-b", "snapbid"),
				Revision: snap.R(4),
			},
		})
		// nil indicates successful validation
		return nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/other", nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)

	snapstate.Set(st, "snap-a", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "snap-a", Revision: snap.R(2), SnapID: "snapaid"}},
		Current:  snap.R(2),
	})
	snapstate.Set(st, "snap-b", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "snap-b", Revision: snap.R(4), SnapID: "snapbid"}},
		Current:  snap.R(4),
	})

	st.Unlock()

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: "foo",
		Name:      "other",
		Sequence:  2,
		Valid:     true,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetLatestFromRemoteValidationFails(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		as, err := asserts.Decode(validationSetAssertion)
		c.Assert(err, check.IsNil)
		return as, nil
	}
	restore := daemon.MockCheckInstalledSnaps(func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap) error {
		return &snapasserts.ValidationSetsValidationError{}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/other", nil)
	c.Assert(err, check.IsNil)
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)

	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: "foo",
		Name:      "other",
		Sequence:  2,
		Valid:     false,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetLatestFromRemoteRealValidation(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		as, err := asserts.Decode(validationSetAssertion)
		c.Assert(err, check.IsNil)
		return as, nil
	}

	st := s.d.Overlord().State()

	for _, tc := range []struct {
		revision                 snap.Revision
		expectedValidationStatus bool
	}{
		// required at revision 1 per validationSetAssertion, so it's valid
		{snap.R(1), true},
		// but revision 2 is not valid
		{snap.R(2), false},
	} {
		st.Lock()
		snapstate.Set(st, "snap-b", &snapstate.SnapState{
			Active:   true,
			Sequence: []*snap.SideInfo{{RealName: "snap-b", Revision: tc.revision, SnapID: "yOqKhntON3vR7kwEbVPsILm7bUViPDzz"}},
			Current:  tc.revision,
		})
		st.Unlock()

		req, err := http.NewRequest("GET", "/v2/validation-sets/foo/other", nil)
		c.Assert(err, check.IsNil)
		rsp := s.syncReq(c, req, nil)
		c.Assert(rsp.Status, check.Equals, 200)

		res := rsp.Result.(daemon.ValidationSetResult)
		c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
			AccountID: "foo",
			Name:      "other",
			Sequence:  2,
			Valid:     tc.expectedValidationStatus,
		})
	}
}

func (s *apiValidationSetsSuite) TestGetValidationSetSpecificSequenceFromRemote(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		c.Assert(assertType, check.NotNil)
		c.Assert(assertType.Name, check.Equals, "validation-set")
		c.Assert(sequenceKey, check.DeepEquals, []string{"16", "foo", "other"})
		c.Assert(sequence, check.Equals, 2)
		as, err := asserts.Decode(validationSetAssertion)
		c.Assert(err, check.IsNil)
		return as, nil
	}

	restore := daemon.MockCheckInstalledSnaps(func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap) error {
		c.Assert(vsets, check.NotNil)
		sort.Sort(byName(snaps))
		c.Assert(snaps, check.DeepEquals, []*snapasserts.InstalledSnap{
			{
				SnapRef:  naming.NewSnapRef("snap-a", "snapaid"),
				Revision: snap.R(33),
			},
		})
		// nil indicates successful validation
		return nil
	})
	defer restore()

	q := url.Values{}
	q.Set("sequence", "2")
	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/other?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)

	snapstate.Set(st, "snap-a", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{{RealName: "snap-a", Revision: snap.R(33), SnapID: "snapaid"}},
		Current:  snap.R(33),
	})

	st.Unlock()

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: "foo",
		Name:      "other",
		Sequence:  2,
		Valid:     true,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetFromRemoteFallbackToLocalAssertion(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		// not found in the store
		return nil, &asserts.NotFoundError{
			Type: assertType,
		}
	}
	restore := daemon.MockCheckInstalledSnaps(func(vsets *snapasserts.ValidationSets, snaps []*snapasserts.InstalledSnap) error {
		// nil indicates successful validation
		return nil
	})
	defer restore()

	st := s.d.Overlord().State()
	st.Lock()
	// assertion available in the local db (from snap ack)
	vs := s.mockAssert(c, "bar", "2")
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key, vs)
	st.Unlock()

	q := url.Values{}
	q.Set("sequence", "2")
	req, err := http.NewRequest("GET", fmt.Sprintf("/v2/validation-sets/%s/bar?%s", s.dev1acct.AccountID(), q.Encode()), nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	res := rsp.Result.(daemon.ValidationSetResult)
	c.Check(res, check.DeepEquals, daemon.ValidationSetResult{
		AccountID: s.dev1acct.AccountID(),
		Name:      "bar",
		Sequence:  2,
		Valid:     true,
	})
}

func (s *apiValidationSetsSuite) TestGetValidationSetPinnedNotFound(c *check.C) {
	s.mockSeqFormingAssertionFn = func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
		return nil, &asserts.NotFoundError{
			Type: assertType,
		}
	}

	q := url.Values{}
	q.Set("sequence", "333")
	req, err := http.NewRequest("GET", "/v2/validation-sets/foo/bar?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)
	st.Unlock()

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 404)
	res := rsp.Result.(*daemon.ErrorResult)
	c.Assert(res, check.NotNil)
	c.Check(string(res.Kind), check.Equals, "validation-set-not-found")
	c.Check(res.Value, check.DeepEquals, map[string]interface{}{
		"account-id": "foo",
		"name":       "bar",
		"sequence":   333,
	})
}

func (s *apiValidationSetsSuite) TestApplyValidationSetMonitorModePinnedLocalOnly(c *check.C) {
	restore := daemon.MockValidationSetAssertionForMonitor(func(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *assertstate.ResolveOptions) (*asserts.ValidationSet, bool, error) {
		c.Assert(accountID, check.Equals, s.dev1acct.AccountID())
		c.Assert(name, check.Equals, "bar")
		c.Assert(sequence, check.Equals, 99)
		c.Assert(pinned, check.Equals, true)
		c.Assert(opts, check.NotNil)
		c.Check(opts.AllowLocalFallback, check.Equals, true)

		db := assertstate.DB(st)
		headers, err := asserts.HeadersFromPrimaryKey(asserts.ValidationSetType, []string{release.Series, accountID, name, fmt.Sprintf("%d", sequence)})
		c.Assert(err, check.IsNil)
		// validation set assertion available locally
		vs, err := db.Find(asserts.ValidationSetType, headers)
		c.Assert(err, check.IsNil)
		return vs.(*asserts.ValidationSet), true, nil
	})
	defer restore()

	st := s.d.Overlord().State()

	st.Lock()
	vs := s.mockAssert(c, "bar", "99")
	// add validation set assertion to the local db
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key, vs)
	st.Unlock()

	body := `{"action":"apply","mode":"monitor", "sequence":99}`
	req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s/bar", s.dev1acct.AccountID()), strings.NewReader(body))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)

	var tr assertstate.ValidationSetTracking

	// verify tracking information
	st.Lock()
	err = assertstate.GetValidationSet(st, s.dev1acct.AccountID(), "bar", &tr)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(tr, check.DeepEquals, assertstate.ValidationSetTracking{
		Mode:      assertstate.Monitor,
		AccountID: s.dev1acct.AccountID(),
		Name:      "bar",
		PinnedAt:  99,
		Current:   99,
		LocalOnly: true,
	})
}

func (s *apiValidationSetsSuite) TestApplyValidationSetMonitorModePinnedUnresolved(c *check.C) {
	restore := daemon.MockValidationSetAssertionForMonitor(func(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *assertstate.ResolveOptions) (*asserts.ValidationSet, bool, error) {
		c.Assert(accountID, check.Equals, s.dev1acct.AccountID())
		c.Assert(name, check.Equals, "bar")
		c.Assert(sequence, check.Equals, 99)
		c.Assert(pinned, check.Equals, true)

		snaps := []interface{}{map[string]interface{}{
			"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "snap-b",
			"presence": "required",
			"revision": "1",
		}}
		headers := map[string]interface{}{
			"authority-id": s.dev1acct.AccountID(),
			"account-id":   s.dev1acct.AccountID(),
			"name":         "bar",
			"series":       "16",
			"sequence":     "99",
			"revision":     "5",
			"timestamp":    "2030-11-06T09:16:26Z",
			"snaps":        snaps,
		}
		// validation set assertion coming from the store
		vs, err := s.dev1Signing.Sign(asserts.ValidationSetType, headers, nil, "")
		c.Assert(err, check.IsNil)
		return vs.(*asserts.ValidationSet), false, nil
	})
	defer restore()

	st := s.d.Overlord().State()

	st.Lock()
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key)
	st.Unlock()

	body := `{"action":"apply","mode":"monitor", "sequence":99}`
	req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s/bar", s.dev1acct.AccountID()), strings.NewReader(body))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)

	var tr assertstate.ValidationSetTracking

	// verify tracking information
	st.Lock()
	err = assertstate.GetValidationSet(st, s.dev1acct.AccountID(), "bar", &tr)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(tr, check.DeepEquals, assertstate.ValidationSetTracking{
		Mode:      assertstate.Monitor,
		AccountID: s.dev1acct.AccountID(),
		Name:      "bar",
		PinnedAt:  99,
		Current:   99,
	})
}

func (s *apiValidationSetsSuite) TestApplyValidationSetMonitorModeUnpinnedRefreshed(c *check.C) {
	snaps := []interface{}{map[string]interface{}{
		"id":       "yOqKhntON3vR7kwEbVPsILm7bUViPDzz",
		"name":     "snap-b",
		"presence": "required",
		"revision": "1",
	}}

	restore := daemon.MockValidationSetAssertionForMonitor(func(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *assertstate.ResolveOptions) (*asserts.ValidationSet, bool, error) {
		c.Assert(accountID, check.Equals, s.dev1acct.AccountID())
		c.Assert(name, check.Equals, "bar")
		c.Assert(sequence, check.Equals, 0)
		c.Assert(pinned, check.Equals, false)

		// new sequence
		headers := map[string]interface{}{
			"authority-id": s.dev1acct.AccountID(),
			"account-id":   s.dev1acct.AccountID(),
			"name":         "bar",
			"series":       "16",
			"sequence":     "2",
			"revision":     "1",
			"timestamp":    "2030-11-06T09:16:26Z",
			"snaps":        snaps,
		}
		// updated validation set assertion coming from the store
		vs, err := s.dev1Signing.Sign(asserts.ValidationSetType, headers, nil, "")
		c.Assert(err, check.IsNil)
		return vs.(*asserts.ValidationSet), false, nil
	})
	defer restore()

	st := s.d.Overlord().State()

	st.Lock()
	assertstatetest.AddMany(st, s.dev1acct, s.acct1Key)
	st.Unlock()

	headers := map[string]interface{}{
		"authority-id": s.dev1acct.AccountID(),
		"account-id":   s.dev1acct.AccountID(),
		"name":         "bar",
		"series":       "16",
		"sequence":     "1",
		"revision":     "1",
		"timestamp":    "2030-11-06T09:16:26Z",
		"snaps":        snaps,
	}
	vs, err := s.dev1Signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, check.IsNil)

	st.Lock()
	// add validation set assertion to the local db
	c.Assert(assertstate.Add(st, vs), check.IsNil)
	st.Unlock()

	body := `{"action":"apply","mode":"monitor"}`
	req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s/bar", s.dev1acct.AccountID()), strings.NewReader(body))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)

	var tr assertstate.ValidationSetTracking

	// verify tracking information
	st.Lock()
	err = assertstate.GetValidationSet(st, s.dev1acct.AccountID(), "bar", &tr)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(tr, check.DeepEquals, assertstate.ValidationSetTracking{
		Mode:      assertstate.Monitor,
		AccountID: s.dev1acct.AccountID(),
		Name:      "bar",
		Current:   2,
	})
}

func (s *apiValidationSetsSuite) TestApplyValidationSetMonitorModeError(c *check.C) {
	restore := daemon.MockValidationSetAssertionForMonitor(func(st *state.State, accountID, name string, sequence int, pinned bool, userID int, opts *assertstate.ResolveOptions) (*asserts.ValidationSet, bool, error) {
		return nil, false, fmt.Errorf("boom")
	})
	defer restore()

	body := `{"action":"apply","mode":"monitor"}`
	req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s/bar", s.dev1acct.AccountID()), strings.NewReader(body))
	c.Assert(err, check.IsNil)

	rsp := s.errorReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 400)
	c.Check(rsp.ErrorResult().Message, check.Equals, fmt.Sprintf(`cannot get validation set assertion for %s/bar: boom`, s.dev1acct.AccountID()))
}

func (s *apiValidationSetsSuite) TestForgetValidationSet(c *check.C) {
	st := s.d.Overlord().State()

	for i, sequence := range []int{0, 9} {
		st.Lock()
		s.mockValidationSetsTracking(st)
		st.Unlock()

		var body string
		if sequence != 0 {
			body = fmt.Sprintf(`{"action":"forget", "sequence":%d}`, sequence)
		} else {
			body = `{"action":"forget"}`
		}

		var tr assertstate.ValidationSetTracking

		st.Lock()
		// sanity, it exists before removing
		err := assertstate.GetValidationSet(st, s.dev1acct.AccountID(), "foo", &tr)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(tr.AccountID, check.Equals, s.dev1acct.AccountID())
		c.Check(tr.Name, check.Equals, "foo")

		req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s/foo", s.dev1acct.AccountID()), strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp := s.syncReq(c, req, nil)
		c.Assert(rsp.Status, check.Equals, 200, check.Commentf("case #%d", i))

		// after forget it's removed
		st.Lock()
		err = assertstate.GetValidationSet(st, s.dev1acct.AccountID(), "foo", &tr)
		st.Unlock()
		c.Assert(err, check.Equals, state.ErrNoState)

		// and forget again fails
		req, err = http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s/foo", s.dev1acct.AccountID()), strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp = s.errorReq(c, req, nil)
		c.Assert(rsp.Status, check.Equals, 404, check.Commentf("case #%d", i))
	}
}

func (s *apiValidationSetsSuite) TestApplyValidationSetsErrors(c *check.C) {
	st := s.d.Overlord().State()
	st.Lock()
	s.mockValidationSetsTracking(st)
	st.Unlock()

	for i, tc := range []struct {
		validationSet string
		mode          string
		// sequence is normally an int, use string for passing invalid ones.
		sequence string
		message  string
		status   int
	}{
		{
			validationSet: "0/zzz",
			mode:          "monitor",
			message:       `invalid account ID "0"`,
			status:        400,
		},
		{
			validationSet: "Xfoo/bar",
			mode:          "monitor",
			message:       `invalid account ID "Xfoo"`,
			status:        400,
		},
		{
			validationSet: "foo/Xabc",
			mode:          "monitor",
			message:       `invalid name "Xabc"`,
			status:        400,
		},
		{
			validationSet: "foo/bar",
			sequence:      "x",
			message:       "cannot decode request body into validation set action: invalid character 'x' looking for beginning of value",
			status:        400,
		},
		{
			validationSet: "foo/bar",
			mode:          "bad",
			message:       `invalid mode "bad"`,
			status:        400,
		},
		// XXX: enable when enforcing is implemented.
		{
			validationSet: "foo/bar",
			mode:          "enforce",
			message:       `invalid mode "enforce"`,
			status:        400,
		},
		{
			validationSet: "foo/bar",
			sequence:      "-1",
			mode:          "monitor",
			message:       `invalid sequence argument: -1`,
			status:        400,
		},
	} {
		var body string
		if tc.sequence != "" {
			body = fmt.Sprintf(`{"action":"apply","mode":"%s", "sequence":%s}`, tc.mode, tc.sequence)
		} else {
			body = fmt.Sprintf(`{"action":"apply","mode":"%s"}`, tc.mode)
		}
		req, err := http.NewRequest("POST", fmt.Sprintf("/v2/validation-sets/%s", tc.validationSet), strings.NewReader(body))
		c.Assert(err, check.IsNil)
		rsp := s.errorReq(c, req, nil)
		c.Check(rsp.Status, check.Equals, tc.status, check.Commentf("case #%d", i))
		c.Check(rsp.ErrorResult().Message, check.Matches, tc.message)
	}
}

func (s *apiValidationSetsSuite) TestApplyValidationSetUnsupportedAction(c *check.C) {
	body := `{"action":"baz","mode":"monitor"}`

	req, err := http.NewRequest("POST", "/v2/validation-sets/foo/bar", strings.NewReader(body))
	c.Assert(err, check.IsNil)

	rsp := s.errorReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.ErrorResult().Message, check.Matches, `unsupported action "baz"`)
}
