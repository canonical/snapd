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
	_ = Suite(&systemUserSuite{})
)

type systemUserSuite struct {
	until     time.Time
	untilLine string
	since     time.Time
	sinceLine string

	modelsLine string

	systemUserStr string
}

const systemUserExample = "type: system-user\n" +
	"authority-id: canonical\n" +
	"brand-id: canonical\n" +
	"email: foo@example.com\n" +
	"series:\n" +
	"  - 16\n" +
	"MODELSLINE\n" +
	"name: Nice Guy\n" +
	"username: guy\n" +
	"password: $6$salt$hash\n" +
	"ssh-keys:\n" +
	"  - ssh-rsa AAAABcdefg\n" +
	"SINCELINE\n" +
	"UNTILLINE\n" +
	"body-length: 0\n" +
	"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
	"\n\n" +
	"AXNpZw=="

func (s *systemUserSuite) SetUpTest(c *C) {
	s.since = time.Now().Truncate(time.Second)
	s.sinceLine = fmt.Sprintf("since: %s\n", s.since.Format(time.RFC3339))
	s.until = time.Now().AddDate(0, 1, 0).Truncate(time.Second)
	s.untilLine = fmt.Sprintf("until: %s\n", s.until.Format(time.RFC3339))
	s.modelsLine = "models:\n  - frobinator\n"
	s.systemUserStr = strings.Replace(systemUserExample, "UNTILLINE\n", s.untilLine, 1)
	s.systemUserStr = strings.Replace(s.systemUserStr, "SINCELINE\n", s.sinceLine, 1)
	s.systemUserStr = strings.Replace(s.systemUserStr, "MODELSLINE\n", s.modelsLine, 1)
}

func (s *systemUserSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(s.systemUserStr))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SystemUserType)
	systemUser := a.(*asserts.SystemUser)
	c.Check(systemUser.BrandID(), Equals, "canonical")
	c.Check(systemUser.Email(), Equals, "foo@example.com")
	c.Check(systemUser.Series(), DeepEquals, []string{"16"})
	c.Check(systemUser.Models(), DeepEquals, []string{"frobinator"})
	c.Check(systemUser.Name(), Equals, "Nice Guy")
	c.Check(systemUser.Username(), Equals, "guy")
	c.Check(systemUser.Password(), Equals, "$6$salt$hash")
	c.Check(systemUser.SSHKeys(), DeepEquals, []string{"ssh-rsa AAAABcdefg"})
	c.Check(systemUser.Since().Equal(s.since), Equals, true)
	c.Check(systemUser.Until().Equal(s.until), Equals, true)
}

func (s *systemUserSuite) TestDecodePasswd(c *C) {
	validTests := []struct{ original, valid string }{
		{"password: $6$salt$hash\n", "password: $6$rounds=9999$salt$hash\n"},
		{"password: $6$salt$hash\n", ""},
	}
	for _, test := range validTests {
		valid := strings.Replace(s.systemUserStr, test.original, test.valid, 1)
		_, err := asserts.Decode([]byte(valid))
		c.Check(err, IsNil)
	}
}

func (s *systemUserSuite) TestValidAt(c *C) {
	a, err := asserts.Decode([]byte(s.systemUserStr))
	c.Assert(err, IsNil)
	su := a.(*asserts.SystemUser)

	c.Check(su.ValidAt(su.Since()), Equals, true)
	c.Check(su.ValidAt(su.Since().AddDate(0, 0, -1)), Equals, false)
	c.Check(su.ValidAt(su.Since().AddDate(0, 0, 1)), Equals, true)

	c.Check(su.ValidAt(su.Until()), Equals, false)
	c.Check(su.ValidAt(su.Until().AddDate(0, -1, 0)), Equals, true)
	c.Check(su.ValidAt(su.Until().AddDate(0, 1, 0)), Equals, false)
}

func (s *systemUserSuite) TestValidAtRevoked(c *C) {
	// With since == until, i.e. system-user has been revoked.
	revoked := strings.Replace(s.systemUserStr, s.sinceLine, fmt.Sprintf("since: %s\n", s.until.Format(time.RFC3339)), 1)
	a, err := asserts.Decode([]byte(revoked))
	c.Assert(err, IsNil)
	su := a.(*asserts.SystemUser)

	c.Check(su.ValidAt(su.Since()), Equals, false)
	c.Check(su.ValidAt(su.Since().AddDate(0, 0, -1)), Equals, false)
	c.Check(su.ValidAt(su.Since().AddDate(0, 0, 1)), Equals, false)

	c.Check(su.ValidAt(su.Until()), Equals, false)
	c.Check(su.ValidAt(su.Until().AddDate(0, -1, 0)), Equals, false)
	c.Check(su.ValidAt(su.Until().AddDate(0, 1, 0)), Equals, false)
}

const (
	systemUserErrPrefix = "assertion system-user: "
)

func (s *systemUserSuite) TestDecodeInvalid(c *C) {
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: canonical\n", "", `"brand-id" header is mandatory`},
		{"brand-id: canonical\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"email: foo@example.com\n", "", `"email" header is mandatory`},
		{"email: foo@example.com\n", "email: \n", `"email" header should not be empty`},
		{"email: foo@example.com\n", "email: <alice!example.com>\n", `"email" header must be a RFC 5322 compliant email address: mail: missing @ in addr-spec`},
		{"email: foo@example.com\n", "email: no-mail\n", `"email" header must be a RFC 5322 compliant email address:.*`},
		{"series:\n  - 16\n", "series: \n", `"series" header must be a list of strings`},
		{"series:\n  - 16\n", "series: something\n", `"series" header must be a list of strings`},
		{"models:\n  - frobinator\n", "models: \n", `"models" header must be a list of strings`},
		{"models:\n  - frobinator\n", "models: something\n", `"models" header must be a list of strings`},
		{"ssh-keys:\n  - ssh-rsa AAAABcdefg\n", "ssh-keys: \n", `"ssh-keys" header must be a list of strings`},
		{"ssh-keys:\n  - ssh-rsa AAAABcdefg\n", "ssh-keys: something\n", `"ssh-keys" header must be a list of strings`},
		{"name: Nice Guy\n", "name:\n  - foo\n", `"name" header must be a string`},
		{"username: guy\n", "username:\n  - foo\n", `"username" header must be a string`},
		{"username: guy\n", "username: bäää\n", `"username" header contains invalid characters: "bäää"`},
		{"username: guy\n", "", `"username" header is mandatory`},
		{"password: $6$salt$hash\n", "password:\n  - foo\n", `"password" header must be a string`},
		{"password: $6$salt$hash\n", "password: cleartext\n", `"password" header invalid: hashed password must be of the form "\$integer-id\$salt\$hash", see crypt\(3\)`},
		{"password: $6$salt$hash\n", "password: $ni!$salt$hash\n", `"password" header must start with "\$integer-id\$", got "ni!"`},
		{"password: $6$salt$hash\n", "password: $3$salt$hash\n", `"password" header only supports \$id\$ values of 6 \(sha512crypt\) or higher`},
		{"password: $6$salt$hash\n", "password: $7$invalid-salt$hash\n", `"password" header has invalid chars in salt "invalid-salt"`},
		{"password: $6$salt$hash\n", "password: $8$salt$invalid-hash\n", `"password" header has invalid chars in hash "invalid-hash"`},
		{"password: $6$salt$hash\n", "password: $8$rounds=9999$hash\n", `"password" header invalid: missing hash field`},
		{"password: $6$salt$hash\n", "password: $8$rounds=xxx$salt$hash\n", `"password" header has invalid number of rounds:.*`},
		{"password: $6$salt$hash\n", "password: $8$rounds=1$salt$hash\n", `"password" header rounds parameter out of bounds: 1`},
		{"password: $6$salt$hash\n", "password: $8$rounds=1999999999$salt$hash\n", `"password" header rounds parameter out of bounds: 1999999999`},
		{s.sinceLine, "since: \n", `"since" header should not be empty`},
		{s.sinceLine, "since: 12:30\n", `"since" header is not a RFC3339 date: .*`},
		{s.untilLine, "until: \n", `"until" header should not be empty`},
		{s.untilLine, "until: 12:30\n", `"until" header is not a RFC3339 date: .*`},
		{s.untilLine, "until: 1002-11-01T22:08:41+00:00\n", `'until' time cannot be before 'since' time`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(s.systemUserStr, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, systemUserErrPrefix+test.expectedErr)
	}
}

func (s *systemUserSuite) TestUntilNoModels(c *C) {
	// no models is good for <1y
	su := strings.Replace(s.systemUserStr, s.modelsLine, "", -1)
	_, err := asserts.Decode([]byte(su))
	c.Check(err, IsNil)

	// but invalid for more than one year
	oneYearPlusOne := time.Now().AddDate(1, 0, 1).Truncate(time.Second)
	su = strings.Replace(su, s.untilLine, fmt.Sprintf("until: %s\n", oneYearPlusOne.Format(time.RFC3339)), -1)
	_, err = asserts.Decode([]byte(su))
	c.Check(err, ErrorMatches, systemUserErrPrefix+"'until' time cannot be more than 365 days in the future when no models are specified")
}

func (s *systemUserSuite) TestUntilWithModels(c *C) {
	// with models it can be valid forever
	oneYearPlusOne := time.Now().AddDate(10, 0, 1).Truncate(time.Second)
	su := strings.Replace(s.systemUserStr, s.untilLine, fmt.Sprintf("until: %s\n", oneYearPlusOne.Format(time.RFC3339)), -1)
	_, err := asserts.Decode([]byte(su))
	c.Check(err, IsNil)
}
