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

	"github.com/ubuntu-core/snappy/asserts"
	. "gopkg.in/check.v1"
)

var (
	_ = Suite(&identitySuite{})
)

type identitySuite struct {
	ts     time.Time
	tsLine string
}

func (ids *identitySuite) SetUpSuite(c *C) {
	ids.ts = time.Now().Truncate(time.Second).UTC()
	ids.tsLine = "timestamp: " + ids.ts.Format(time.RFC3339) + "\n"
}

const identityExample = "type: identity\n" +
	"authority-id: canonical\n" +
	"account-id: abc-123\n" +
	"display-name: Display Name\n" +
	"validation: certified\n" +
	"TSLINE" +
	"body-length: 0" +
	"\n\n" +
	"openpgp c2ln"

func (ids *identitySuite) TestDecodeOK(c *C) {
	encoded := strings.Replace(identityExample, "TSLINE", ids.tsLine, 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.IdentityType)
	identity := a.(*asserts.Identity)
	c.Check(identity.AuthorityID(), Equals, "canonical")
	c.Check(identity.Timestamp(), Equals, ids.ts)
	c.Check(identity.AccountID(), Equals, "abc-123")
	c.Check(identity.DisplayName(), Equals, "Display Name")
	c.Check(identity.IsCertified(), Equals, true)
}

func (ids *identitySuite) TestIsCertified(c *C) {
	tests := []struct {
		value       string
		isCertified bool
	}{
		{"certified", true},
		{"unproven", false},
		{"nonsense", false},
	}

	template := strings.Replace(identityExample, "TSLINE", ids.tsLine, 1)
	for _, test := range tests {
		encoded := strings.Replace(
			template,
			"validation: certified\n",
			fmt.Sprintf("validation: %s\n", test.value),
			1,
		)
		assert, err := asserts.Decode([]byte(encoded))
		c.Assert(err, IsNil)
		identity := assert.(*asserts.Identity)
		c.Check(identity.IsCertified(), Equals, test.isCertified)
	}
}

const (
	identityErrPrefix = "assertion identity: "
)

func (ids *identitySuite) TestDecodeInvalid(c *C) {
	encoded := strings.Replace(identityExample, "TSLINE", ids.tsLine, 1)

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"account-id: abc-123\n", "", `"account-id" header is mandatory`},
		{"display-name: Display Name\n", "", `"display-name" header is mandatory`},
		{"validation: certified\n", "", `"validation" header is mandatory`},
		{ids.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, identityErrPrefix+test.expectedErr)
	}
}

func (ids *identitySuite) TestCheckInconsistentTimestamp(c *C) {
	ex, err := asserts.Decode([]byte(strings.Replace(identityExample, "TSLINE", ids.tsLine, 1)))
	c.Assert(err, IsNil)

	signingKeyID, accSignDB, db := makeSignAndCheckDbWithAccountKey(c, "canonical")

	headers := ex.Headers()
	headers["timestamp"] = "2011-01-01T14:00:00Z"
	identity, err := accSignDB.Sign(asserts.IdentityType, headers, nil, signingKeyID)
	c.Assert(err, IsNil)

	err = db.Check(identity)
	c.Assert(err, ErrorMatches, "identity assertion timestamp outside of signing key validity")
}
