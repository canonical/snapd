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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

type authorityDelegationSuite struct {
	assertionsLines string
	validEncoded    string
}

var _ = Suite(&authorityDelegationSuite{})

func (s *authorityDelegationSuite) SetUpSuite(c *C) {
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
				"type": "snap-declaration",
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
				"type": "model",
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
				"type": "model",
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
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, authDelegErrPrefix+test.expectedErr)
	}
}
