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
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	. "gopkg.in/check.v1"
)

var (
	_ = Suite(&systemUserSuite{})
)

type systemUserSuite struct {
	ts     time.Time
	tsLine string
}

func (s *systemUserSuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
}

const systemUserExample = "type: system-user\n" +
	"authority-id: canonical\n" +
	"brand-id: canonical\n" +
	"email: foo@example.com\n" +
	"series:\n" +
	"  - 16\n" +
	"models:\n" +
	"  - frobinator\n" +
	"name: Nice Guy\n" +
	"username: guy\n" +
	"password: $6$salt$hash\n" +
	"ssh-keys:\n" +
	"  - ssh-rsa AAAABcdefg\n" +
	"TSLINE" +
	"until: 2092-11-01T22:08:41+00:00\n" +
	"body-length: 0\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"AXNpZw=="

func (s *systemUserSuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(systemUserExample, "TSLINE", s.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SystemUserType)
	systemUser := a.(*asserts.SystemUser)
	c.Check(systemUser.BrandID(), Equals, "canonical")
	c.Check(systemUser.EMail(), Equals, "foo@example.com")
	c.Check(systemUser.Series(), DeepEquals, []string{"16"})
	c.Check(systemUser.Models(), DeepEquals, []string{"frobinator"})
	c.Check(systemUser.Name(), Equals, "Nice Guy")
	c.Check(systemUser.Username(), Equals, "guy")
	c.Check(systemUser.Password(), Equals, "$6$salt$hash")
	c.Check(systemUser.SSHKeys(), DeepEquals, []string{"ssh-rsa AAAABcdefg"})
	c.Check(systemUser.Timestamp(), Equals, s.ts)
	tv, err := time.Parse(time.RFC3339, "2092-11-01T22:08:41+00:00")
	c.Assert(err, IsNil)
	c.Check(systemUser.Until(), DeepEquals, tv)
}

const (
	systemUserErrPrefix = "assertion system-user: "
)

func (s *systemUserSuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(systemUserExample, "TSLINE", s.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: canonical\n", "", `"brand-id" header is mandatory`},
		{"brand-id: canonical\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"brand-id: canonical\n", "brand-id: something-else\n", `authority-id and brand-id must match, system-user assertions are expected to be signed by the brand: "canonical" != "something-else"`},
		{"email: foo@example.com\n", "", `"email" header is mandatory`},
		{"email: foo@example.com\n", "email: \n", `"email" header should not be empty`},
		{s.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{s.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, systemUserErrPrefix+test.expectedErr)
	}
}

func (s *systemUserSuite) TestCheckInconsistentTimestamp(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(accountExample, "TSLINE", s.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)

	headers := ex.Headers()
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	account, err := storeDB.Sign(asserts.AccountType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(account)
	c.Assert(err, ErrorMatches, "account assertion timestamp outside of signing key validity")
}

func (s *systemUserSuite) TestCheckUntrustedAuthority(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(accountExample, "TSLINE", s.tsLine, 1)))
	c.Assert(err, IsNil)

	storeDB, db := makeStoreAndCheckDB(c)
	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := ex.Headers()
	headers["timestamp"] = time.Now().Format(time.RFC3339)
	account, err := otherDB.Sign(asserts.AccountType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(account)
	c.Assert(err, ErrorMatches, `account assertion for "abc-123" is not signed by a directly trusted authority:.*`)
}
