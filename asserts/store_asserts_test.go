// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

var _ = Suite(&storeSuite{})

type storeSuite struct {
	ts           time.Time
	tsLine       string
	validExample string
}

func (s *storeSuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
	s.validExample = "type: store\n" +
		"authority-id: canonical\n" +
		"store: store1\n" +
		"operator-id: op-id1\n" +
		"url: https://store.example.com\n" +
		"location: upstairs\n" +
		s.tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij\n" +
		"\n" +
		"AXNpZw=="
}

func (s *storeSuite) TestDecodeOK(c *C) {
	a := mylog.Check2(asserts.Decode([]byte(s.validExample)))

	c.Check(a.Type(), Equals, asserts.StoreType)
	store := a.(*asserts.Store)

	c.Check(store.OperatorID(), Equals, "op-id1")
	c.Check(store.Store(), Equals, "store1")
	c.Check(store.URL().String(), Equals, "https://store.example.com")
	c.Check(store.Location(), Equals, "upstairs")
	c.Check(store.Timestamp().Equal(s.ts), Equals, true)
	c.Check(store.FriendlyStores(), HasLen, 0)
}

var storeErrPrefix = "assertion store: "

func (s *storeSuite) TestDecodeInvalidHeaders(c *C) {
	tests := []struct{ original, invalid, expectedErr string }{
		{"store: store1\n", "", `"store" header is mandatory`},
		{"store: store1\n", "store: \n", `"store" header should not be empty`},
		{"operator-id: op-id1\n", "", `"operator-id" header is mandatory`},
		{"operator-id: op-id1\n", "operator-id: \n", `"operator-id" header should not be empty`},
		{"url: https://store.example.com\n", "url:\n  - foo\n", `"url" header must be a string`},
		{"location: upstairs\n", "location:\n  - foo\n", `"location" header must be a string`},
		{s.tsLine, "", `"timestamp" header is mandatory`},
		{s.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{s.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
		{"url: https://store.example.com\n", "friendly-stores: foo\n", `"friendly-stores" header must be a list of strings`},
	}

	for _, test := range tests {
		invalid := strings.Replace(s.validExample, test.original, test.invalid, 1)
		_ := mylog.Check2(asserts.Decode([]byte(invalid)))
		c.Check(err, ErrorMatches, storeErrPrefix+test.expectedErr)
	}
}

func (s *storeSuite) TestURLOptional(c *C) {
	tests := []string{"", "url: \n"}
	for _, test := range tests {
		encoded := strings.Replace(s.validExample, "url: https://store.example.com\n", test, 1)
		assert := mylog.Check2(asserts.Decode([]byte(encoded)))

		store := assert.(*asserts.Store)
		c.Check(store.URL(), IsNil)
	}
}

func (s *storeSuite) TestURL(c *C) {
	tests := []struct {
		url string
		err string
	}{
		// Valid URLs.
		{"http://example.com/", ""},
		{"https://example.com/", ""},
		{"https://example.com/some/path/", ""},
		{"https://example.com:443/", ""},
		{"https://example.com:1234/", ""},
		{"https://user:pass@example.com/", ""},
		{"https://token@example.com/", ""},

		// Invalid URLs.
		{"://example.com", `"url" header must be a valid URL`},
		{"example.com", `"url" header scheme must be "https" or "http"`},
		{"//example.com", `"url" header scheme must be "https" or "http"`},
		{"ftp://example.com", `"url" header scheme must be "https" or "http"`},
		{"mailto:someone@example.com", `"url" header scheme must be "https" or "http"`},
		{"https://", `"url" header must have a host`},
		{"https:///", `"url" header must have a host`},
		{"https:///some/path", `"url" header must have a host`},
		{"https://example.com/?foo=bar", `"url" header must not have a query`},
		{"https://example.com/#fragment", `"url" header must not have a fragment`},
	}

	for _, test := range tests {
		encoded := strings.Replace(
			s.validExample, "url: https://store.example.com\n",
			fmt.Sprintf("url: %s\n", test.url), 1)
		assert := mylog.Check2(asserts.Decode([]byte(encoded)))
		if test.err != "" {
			c.Assert(err, NotNil)
			c.Check(err.Error(), Equals, storeErrPrefix+test.err+": "+test.url)
		} else {

			c.Check(assert.(*asserts.Store).URL().String(), Equals, test.url)
		}
	}
}

func (s *storeSuite) TestLocationOptional(c *C) {
	encoded := strings.Replace(s.validExample, "location: upstairs\n", "", 1)
	_ := mylog.Check2(asserts.Decode([]byte(encoded)))
	c.Check(err, IsNil)
}

func (s *storeSuite) TestLocation(c *C) {
	for _, test := range []string{"foo", "bar", ""} {
		encoded := strings.Replace(
			s.validExample, "location: upstairs\n",
			fmt.Sprintf("location: %s\n", test), 1)
		assert := mylog.Check2(asserts.Decode([]byte(encoded)))

		store := assert.(*asserts.Store)
		c.Check(store.Location(), Equals, test)
	}
}

func (s *storeSuite) TestCheckAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	// Add account for operator.
	operator := assertstest.NewAccount(storeDB, "op-id1", nil, "")
	mylog.Check(db.Add(operator))


	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	storeHeaders := map[string]interface{}{
		"store":       "store1",
		"operator-id": operator.HeaderString("account-id"),
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	// store signed by some other account fails.
	store := mylog.Check2(otherDB.Sign(asserts.StoreType, storeHeaders, nil, ""))

	mylog.Check(db.Check(store))
	c.Assert(err, ErrorMatches, `store assertion "store1" is not signed by a directly trusted authority: other`)

	// but succeeds when signed by a trusted authority.
	store = mylog.Check2(storeDB.Sign(asserts.StoreType, storeHeaders, nil, ""))

	mylog.Check(db.Check(store))

}

func (s *storeSuite) TestFriendlyStores(c *C) {
	encoded := strings.Replace(s.validExample, "url: https://store.example.com\n", `friendly-stores:
  - store1
  - store2
  - store3
`, 1)
	assert := mylog.Check2(asserts.Decode([]byte(encoded)))

	store := assert.(*asserts.Store)
	c.Check(store.URL(), IsNil)
	c.Check(store.FriendlyStores(), DeepEquals, []string{"store1", "store2", "store3"})
}

func (s *storeSuite) TestCheckOperatorAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	store := mylog.Check2(storeDB.Sign(asserts.StoreType, map[string]interface{}{
		"store":       "store1",
		"operator-id": "op-id1",
		"timestamp":   time.Now().Format(time.RFC3339),
	}, nil, ""))

	mylog.

		// No account for operator op-id1 yet, so Check fails.
		Check(db.Check(store))
	c.Assert(err, ErrorMatches, `store assertion "store1" does not have a matching account assertion for the operator "op-id1"`)

	// Add the op-id1 account.
	operator := assertstest.NewAccount(storeDB, "op-id1", map[string]interface{}{"account-id": "op-id1"}, "")
	mylog.Check(db.Add(operator))

	mylog.

		// Now the operator exists so Check succeeds.
		Check(db.Check(store))

}

func (s *storeSuite) TestPrerequisites(c *C) {
	assert := mylog.Check2(asserts.Decode([]byte(s.validExample)))

	c.Assert(assert.Prerequisites(), DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{"op-id1"}},
	})
}
