// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

var (
	_ = Suite(&accountSuite{})
)

type accountSuite struct {
	ts     time.Time
	tsLine string
}

func (s *accountSuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
}

const accountExample = "type: account\n" +
	"authority-id: canonical\n" +
	"account-id: abc-123\n" +
	"display-name: Nice User\n" +
	"username: nice\n" +
	"validation: verified\n" +
	"TSLINE" +
	"body-length: 0\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"AXNpZw=="

func (s *accountSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(accountExample, "TSLINE", s.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.AccountType)
	account := a.(*asserts.Account)
	c.Check(account.AuthorityID(), Equals, "canonical")
	c.Check(account.Timestamp(), Equals, s.ts)
	c.Check(account.AccountID(), Equals, "abc-123")
	c.Check(account.DisplayName(), Equals, "Nice User")
	c.Check(account.Username(), Equals, "nice")
	c.Check(account.Validation(), Equals, "verified")
}

func (s *accountSuite) TestOptional(c *C) {
	encoded := strings.Replace(accountExample, "TSLINE", s.tsLine, 1)

	tests := []struct{ original, replacement string }{
		{"username: nice\n", ""},
		{"username: nice\n", "username: \n"},
	}

	for _, test := range tests {
		valid := strings.Replace(encoded, test.original, test.replacement, 1)
		_, err := asserts.Decode([]byte(valid))
		c.Check(err, IsNil)
	}
}

func (s *accountSuite) TestValidation(c *C) {
	tests := []struct {
		value      string
		isVerified bool
	}{
		{"certified", true}, // backward compat for hard-coded trusted assertions
		{"verified", true},
		{"unproven", false},
		{"nonsense", false},
	}

	template := strings.Replace(accountExample, "TSLINE", s.tsLine, 1)
	for _, test := range tests {
		encoded := strings.Replace(
			template,
			"validation: verified\n",
			fmt.Sprintf("validation: %s\n", test.value),
			1,
		)
		assert, err := asserts.Decode([]byte(encoded))
		c.Assert(err, IsNil)
		account := assert.(*asserts.Account)
		expected := test.value
		if test.isVerified {
			expected = "verified"
		}
		c.Check(account.Validation(), Equals, expected)
	}
}

const (
	accountErrPrefix = "assertion account: "
)

func (s *accountSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(accountExample, "TSLINE", s.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: abc-123\n", "", `"account-id" header is mandatory`},
		{"account-id: abc-123\n", "account-id: \n", `"account-id" header should not be empty`},
		{"display-name: Nice User\n", "", `"display-name" header is mandatory`},
		{"display-name: Nice User\n", "display-name: \n", `"display-name" header should not be empty`},
		{"username: nice\n", "username:\n  - foo\n  - bar\n", `"username" header must be a string`},
		{"validation: verified\n", "", `"validation" header is mandatory`},
		{"validation: verified\n", "validation: \n", `"validation" header should not be empty`},
		{s.tsLine, "", `"timestamp" header is mandatory`},
		{s.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{s.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, accountErrPrefix+test.expectedErr)
	}
}

func (s *accountSuite) TestCheckInconsistentTimestamp(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(accountExample, "TSLINE", s.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)

	headers := ex.Headers()
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	account, err := storeDB.Sign(asserts.AccountType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(account)
	c.Assert(err, ErrorMatches, `account assertion timestamp outside of signing key validity \(key valid since.*\)`)
}

func (s *accountSuite) TestCheckUntrustedAuthority(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(accountExample, "TSLINE", s.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := ex.Headers()
	// default to signing db's authority
	delete(headers, "authority-id")
	headers["timestamp"] = time.Now().Format(time.RFC3339)
	account, err := otherDB.Sign(asserts.AccountType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(account)
	c.Assert(err, ErrorMatches, `account assertion for "abc-123" is not signed by a directly trusted authority:.*`)
}
