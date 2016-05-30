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
	"validation: certified\n" +
	"TSLINE" +
	"body-length: 0" +
	"\n\n" +
	"openpgp c2ln"

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
	c.Check(account.IsCertified(), Equals, true)
}

func (s *accountSuite) TestIsCertified(c *C) {
	tests := []struct {
		value       string
		isCertified bool
	}{
		{"certified", true},
		{"unproven", false},
		{"nonsense", false},
	}

	template := strings.Replace(accountExample, "TSLINE", s.tsLine, 1)
	for _, test := range tests {
		encoded := strings.Replace(
			template,
			"validation: certified\n",
			fmt.Sprintf("validation: %s\n", test.value),
			1,
		)
		assert, err := asserts.Decode([]byte(encoded))
		c.Assert(err, IsNil)
		account := assert.(*asserts.Account)
		c.Check(account.IsCertified(), Equals, test.isCertified)
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
		{"validation: certified\n", "", `"validation" header is mandatory`},
		{"validation: certified\n", "validation: \n", `"validation" header should not be empty`},
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

	signingKeyID, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "canonical")

	headers := ex.Headers()
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	account, err := accSignDB.Sign(asserts.AccountType, headers, nil, signingKeyID)
	c.Assert(err, IsNil)

	err = db.Check(account)
	c.Assert(err, ErrorMatches, "account assertion timestamp outside of signing key validity")
}
