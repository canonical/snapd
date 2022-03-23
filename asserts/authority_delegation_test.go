// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package asserts_test

import (
	"fmt"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

type authorityDelegationSuite struct {
	assertionsLines string
	since, until    time.Time
	validEncoded    string
}

var _ = Suite(&authorityDelegationSuite{})

func (s *authorityDelegationSuite) SetUpSuite(c *C) {
	c.Skip("authority-delegation disabled")

	s.assertionsLines = `assertions:
  -
    type: snap-revision
    headers:
      snap-id:
        - snap-id-1
        - snap-id-2
      provenance: prov-key1
    since: 2022-01-12T00:00:00.0Z
    until: 2032-01-01T00:00:00.0Z
`
	var err error
	s.since, err = time.Parse(time.RFC3339, "2022-01-12T00:00:00.0Z")
	c.Assert(err, IsNil)
	s.until, err = time.Parse(time.RFC3339, "2032-01-01T00:00:00.0Z")
	c.Assert(err, IsNil)

	s.validEncoded = `type: authority-delegation
authority-id: canonical
account-id: canonical
delegate-id: acc-id1
` + s.assertionsLines + "sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func (s *authorityDelegationSuite) TestDecodeOK(c *C) {
	encoded := s.validEncoded
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AuthorityDelegationType)
	ad := a.(*asserts.AuthorityDelegation)
	c.Check(ad.AccountID(), Equals, "canonical")
	c.Check(ad.DelegateID(), Equals, "acc-id1")
}

func (s *authorityDelegationSuite) TestPrerequisites(c *C) {
	encoded := s.validEncoded
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	prereqs := a.Prerequisites()
	c.Check(prereqs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"canonical"}},
		{Type: asserts.AccountType, PrimaryKey: []string{"acc-id1"}},
	})
}

func (s *authorityDelegationSuite) TestAuthorityDelegationCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := map[string]interface{}{
		"account-id":  "canonical",
		"delegate-id": "other",
		"assertions": []interface{}{
			map[string]interface{}{
				"type":  "snap-declaration",
				"since": time.Now().Format(time.RFC3339),
			},
		},
	}
	ad, err := otherDB.Sign(asserts.AuthorityDelegationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(ad)
	c.Assert(err, ErrorMatches, `authority-delegation assertion for "canonical" is not signed by a directly trusted authority: other`)
}

func (s *authorityDelegationSuite) TestAuthorityDelegationCheckAccountReferences(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := map[string]interface{}{
		"account-id":  "other",
		"delegate-id": "other2",
		"assertions": []interface{}{
			map[string]interface{}{
				"type":  "model",
				"since": time.Now().Format(time.RFC3339),
			},
		},
	}
	ad, err := storeDB.Sign(asserts.AuthorityDelegationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(ad)
	c.Assert(err, ErrorMatches, `authority-delegation assertion for \"other\" does not have a matching account assertion`)

	otherAcct := assertstest.NewAccount(storeDB, "other", map[string]interface{}{
		"account-id": "other",
	}, "")
	c.Assert(db.Add(otherAcct), IsNil)

	err = db.Check(ad)
	c.Assert(err, ErrorMatches, `authority-delegation assertion for \"other\" does not have a matching account assertion for delegated \"other2\"`)
}

func (s *authorityDelegationSuite) TestAuthorityDelegationCheckHappy(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := map[string]interface{}{
		"account-id":  "other",
		"delegate-id": "other2",
		"assertions": []interface{}{
			map[string]interface{}{
				"type":  "model",
				"since": time.Now().Format(time.RFC3339),
			},
		},
	}
	ad, err := storeDB.Sign(asserts.AuthorityDelegationType, headers, nil, "")
	c.Assert(err, IsNil)

	otherAcct := assertstest.NewAccount(storeDB, "other", map[string]interface{}{
		"account-id": "other",
	}, "")
	c.Assert(db.Add(otherAcct), IsNil)
	other2Acct := assertstest.NewAccount(storeDB, "other2", map[string]interface{}{
		"account-id": "other2",
	}, "")
	c.Assert(db.Add(other2Acct), IsNil)

	err = db.Check(ad)
	c.Check(err, IsNil)
}

func (s *authorityDelegationSuite) TestMatchingConstraints(c *C) {
	// XXX test a bit more once AssertionConstraints has some accessors
	encoded := s.validEncoded
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	ad := a.(*asserts.AuthorityDelegation)
	storeDB, _ := makeStoreAndCheckDB(c)

	headers := makeSnapRevisionHeaders(map[string]interface{}{
		"snap-id":    "snap-id-1",
		"provenance": "prov-key1",
	})
	snapRevWProvenance, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	acs := ad.MatchingConstraints(snapRevWProvenance)
	c.Check(acs, HasLen, 1)
	// probe since-until
	ac := acs[0]
	tests := []struct {
		earliest time.Time
		latest   time.Time
		valid    bool
	}{
		{s.since, s.until, true},
		{s.until, s.until.AddDate(0, 3, 0), false},
		{s.since.AddDate(0, -2, 0), s.since.AddDate(0, -2, 0), false},
	}
	for _, t := range tests {
		c.Check(asserts.IsValidAssumingCurTimeWithin(ac, t.earliest, t.latest), Equals, t.valid)
	}
	c.Check(ac.Check(snapRevWProvenance), IsNil)
	c.Check(ac.Check(storeDB.TrustedAccount), ErrorMatches, `assertion "account" does not match constraint for assertion type "snap-revision"`)

	// no provenance => no match
	headers = makeSnapRevisionHeaders(map[string]interface{}{
		"snap-id": "snap-id-1",
	})
	snapRevWoProvenance, err := storeDB.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	acs = ad.MatchingConstraints(snapRevWoProvenance)
	c.Check(acs, HasLen, 0)

	twoAssertConstraints := strings.Replace(encoded, s.assertionsLines, s.assertionsLines+`  -
    type: snap-revision
    headers:
      snap-id:
        - snap-id-1
    since: 2022-01-26T00:00:00.0Z
    until: 2032-01-26T00:00:00.0Z
`, 1)
	a, err = asserts.Decode([]byte(twoAssertConstraints))
	c.Assert(err, IsNil)
	ad2 := a.(*asserts.AuthorityDelegation)

	acs = ad2.MatchingConstraints(snapRevWProvenance)
	c.Check(acs, HasLen, 2)
	acs = ad2.MatchingConstraints(snapRevWoProvenance)
	c.Check(acs, HasLen, 1)
}

// TODO: on-store... constraints

const authDelegErrPrefix = "assertion authority-delegation: "

func (s *authorityDelegationSuite) TestDecodeInvalid(c *C) {
	encoded := s.validEncoded
	const hdrs = `headers:
      snap-id:
        - snap-id-1
        - snap-id-2
      provenance: prov-key1
`

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: canonical\n", "", `"account-id" header is mandatory`},
		{"account-id: canonical\n", "account-id: \n", `"account-id" header should not be empty`},
		{"delegate-id: acc-id1\n", "", `"delegate-id" header is mandatory`},
		{"delegate-id: acc-id1\n", "delegate-id: \n", `"delegate-id" header should not be empty`},
		{s.assertionsLines, "", `assertions constraints are mandatory`},
		{s.assertionsLines, "assertions: \n", "assertions constraints must be a list of maps"},
		{s.assertionsLines, "assertions: foo\n", "assertions constraints must be a list of maps"},
		{s.assertionsLines, "assertions:\n  foo: bar\n", "assertions constraints must be a list of maps"},
		{s.assertionsLines, "assertions:\n  - foo\n", "assertions constraints must be a list of maps"},
		{"    type: snap-revision\n", "", `"type" constraint is mandatory`},
		{"type: snap-revision", "type: ", `"type" constraint should not be empty`},
		{"type: snap-revision", "type: foo", `"foo" is not a valid assertion type`},
		{"type: snap-revision", "type: 1", `"1" is not a valid assertion type`},
		{hdrs, "headers: \n", `"headers" constraint must be a map`},
		{hdrs, "headers: foo\n", `"headers" constraint must be a map`},
		{hdrs, `headers:
      provenance: $FOO
`, `cannot compile headers constraint:.*`},
		{"    since: 2022-01-12T00:00:00.0Z\n", "", `"since" constraint is mandatory`},
		{"    until: 2032-01-01T00:00:00.0Z", "    until: 2012-01-01T00:00:00.0Z", `'until' time cannot be before 'since' time`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, authDelegErrPrefix+test.expectedErr)
	}
}

func (s *authorityDelegationSuite) TestDecodeDeviceScope(c *C) {
	// XXX: for now we fail on device scope constraints
	// to avoid misinterpreting assertions until it is properly implemented
	encoded := s.validEncoded
	sinceFrag := "    since: 2022-01-12T00:00:00.0Z\n"
	for _, deviceScoping := range []string{
		"on-store: store1",
		"on-brand: brand-id-1",
		"on-model: brand-id-1/model1",
	} {
		withDeviceScope := strings.Replace(encoded, sinceFrag, fmt.Sprintf("    %s\n", deviceScoping)+sinceFrag, 1)
		_, err := asserts.Decode([]byte(withDeviceScope))
		c.Check(err, ErrorMatches, `assertion authority-delegation: device scope constraints not yet implemented`)
	}
}
