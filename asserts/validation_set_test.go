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

package asserts_test

import (
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

type validationSetSuite struct {
	ts     time.Time
	tsLine string
}

var _ = Suite(&validationSetSuite{})

func (vss *validationSetSuite) SetUpSuite(c *C) {
	vss.ts = time.Now().Truncate(time.Second).UTC()
	vss.tsLine = "timestamp: " + vss.ts.Format(time.RFC3339) + "\n"
}

const (
	validationSetExample = `type: validation-set
authority-id: brand-id1
series: 16
account-id: brand-id1
name: baz-3000-good
sequence: 2
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    presence: optional
    revision: 99
OTHER` + "TSLINE" +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
)

func (vss *validationSetSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(validationSetExample, "TSLINE", vss.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)

	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.ValidationSetType)
	_, ok := a.(asserts.SequenceMember)
	c.Assert(ok, Equals, true)
	valset := a.(*asserts.ValidationSet)
	c.Check(valset.AuthorityID(), Equals, "brand-id1")
	c.Check(valset.Timestamp(), Equals, vss.ts)
	c.Check(valset.Series(), Equals, "16")
	c.Check(valset.AccountID(), Equals, "brand-id1")
	c.Check(valset.Name(), Equals, "baz-3000-good")
	c.Check(valset.Sequence(), Equals, 2)
	snaps := valset.Snaps()
	c.Assert(snaps, DeepEquals, []*asserts.ValidationSetSnap{
		{
			Name:     "baz-linux",
			SnapID:   "bazlinuxidididididididididididid",
			Presence: asserts.PresenceOptional,
			Revision: 99,
		},
	})
	c.Check(snaps[0].SnapName(), Equals, "baz-linux")
	c.Check(snaps[0].ID(), Equals, "bazlinuxidididididididididididid")
}

func (vss *validationSetSuite) TestDecodeInvalid(c *C) {
	const validationSetErrPrefix = "assertion validation-set: "

	encoded := strings.Replace(validationSetExample, "TSLINE", vss.tsLine, 1)

	snapsStanza := encoded[strings.Index(encoded, "snaps:"):strings.Index(encoded, "timestamp:")]

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"account-id: brand-id1\n", "", `"account-id" header is mandatory`},
		{"account-id: brand-id1\n", "account-id: \n", `"account-id" header should not be empty`},
		{"account-id: brand-id1\n", "account-id: random\n", `authority-id and account-id must match, validation-set assertions are expected to be signed by the issuer account: "brand-id1" != "random"`},
		{"name: baz-3000-good\n", "", `"name" header is mandatory`},
		{"name: baz-3000-good\n", "name: \n", `"name" header should not be empty`},
		{"name: baz-3000-good\n", "name: baz/3000/good\n", `"name" primary key header cannot contain '/'`},
		{"name: baz-3000-good\n", "name: baz+3000+good\n", `"name" header contains invalid characters: "baz\+3000\+good"`},
		{vss.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{"sequence: 2\n", "", `"sequence" header is mandatory`},
		{"sequence: 2\n", "sequence: one\n", `"sequence" header is not an integer: one`},
		{"sequence: 2\n", "sequence: 0\n", `"sequence" must be >=1: 0`},
		{"sequence: 2\n", "sequence: -1\n", `"sequence" must be >=1: -1`},
		{"sequence: 2\n", "sequence: 00\n", `"sequence" header has invalid prefix zeros: 00`},
		{"sequence: 2\n", "sequence: 01\n", `"sequence" header has invalid prefix zeros: 01`},
		{"sequence: 2\n", "sequence: 010\n", `"sequence" header has invalid prefix zeros: 010`},
		{snapsStanza, "", `"snaps" header is mandatory`},
		{snapsStanza, "snaps: snap\n", `"snaps" header must be a list of maps`},
		{snapsStanza, "snaps:\n  - snap\n", `"snaps" header must be a list of maps`},
		{"name: baz-linux\n", "other: 1\n", `"name" of snap is mandatory`},
		{"name: baz-linux\n", "name: linux_2\n", `invalid snap name "linux_2"`},
		{"id: bazlinuxidididididididididididid\n", "id: 2\n", `"id" of snap "baz-linux" contains invalid characters: "2"`},
		{"    id: bazlinuxidididididididididididid\n", "", `"id" of snap "baz-linux" is mandatory`},
		{"OTHER", "  -\n    name: baz-linux\n    id: bazlinuxidididididididididididid\n", `cannot list the same snap "baz-linux" multiple times`},
		{"OTHER", "  -\n    name: baz-linux2\n    id: bazlinuxidididididididididididid\n", `cannot specify the same snap id "bazlinuxidididididididididididid" multiple times, specified for snaps "baz-linux" and "baz-linux2"`},
		{"presence: optional\n", "presence:\n      - opt\n", `"presence" of snap "baz-linux" must be a string`},
		{"presence: optional\n", "presence: no\n", `"presence" of snap "baz-linux" must be one of must be one of required|optional|invalid`},
		{"revision: 99\n", "revision: 0\n", `"revision" of snap "baz-linux" must be >=1: 0`},
		{"presence: optional\n", "presence: invalid\n", `cannot specify revision of snap "baz-linux" at the same time as stating its presence is invalid`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		invalid = strings.Replace(invalid, "OTHER", "", 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, validationSetErrPrefix+test.expectedErr)
	}
}

func (vss *validationSetSuite) TestSnapPresenceOptionalDefaultRequired(c *C) {
	encoded := strings.Replace(validationSetExample, "TSLINE", vss.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "    presence: optional\n", "", 1)

	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.ValidationSetType)
	valset := a.(*asserts.ValidationSet)
	snaps := valset.Snaps()
	c.Assert(snaps, HasLen, 1)
	c.Check(snaps[0].Presence, Equals, asserts.PresenceRequired)
}

func (vss *validationSetSuite) TestSnapRevisionOptional(c *C) {
	encoded := strings.Replace(validationSetExample, "TSLINE", vss.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)
	encoded = strings.Replace(encoded, "    revision: 99\n", "", 1)

	a := mylog.Check2(asserts.Decode([]byte(encoded)))

	c.Check(a.Type(), Equals, asserts.ValidationSetType)
	valset := a.(*asserts.ValidationSet)
	snaps := valset.Snaps()
	c.Assert(snaps, HasLen, 1)
	// 0 means unset
	c.Check(snaps[0].Revision, Equals, 0)
}

func (vss *validationSetSuite) TestIsValidValidationSetName(c *C) {
	names := []struct {
		name  string
		valid bool
	}{
		{"", false},
		{"abA", false},
		{"-a", false},
		{"1", true},
		{"a", true},
		{"ab", true},
		{"foo1-bar0", true},
	}

	for i, name := range names {
		c.Assert(asserts.IsValidValidationSetName(name.name), Equals, name.valid, Commentf("%d: %s", i, name.name))
	}
}

func (vss *validationSetSuite) TestValidationSetSequenceKey(c *C) {
	encoded := strings.Replace(validationSetExample, "TSLINE", vss.tsLine, 1)
	encoded = strings.Replace(encoded, "OTHER", "", 1)

	a := mylog.Check2(asserts.Decode([]byte(encoded)))


	_, ok := a.(asserts.SequenceMember)
	c.Assert(ok, Equals, true)

	valset := a.(*asserts.ValidationSet)

	c.Check(valset.SequenceKey(), Equals, "16/brand-id1/baz-3000-good")
}
