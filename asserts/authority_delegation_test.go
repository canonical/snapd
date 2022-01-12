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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
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
        - snap-id1
        - snap-id2
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

// TODO: on-store... constraints
