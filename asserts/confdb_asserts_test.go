// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/confdb"
	. "gopkg.in/check.v1"
)

type confdbCtrlSuite struct{}

var _ = Suite(&confdbCtrlSuite{})

const (
	confdbControlExample = `type: confdb-control
brand-id: generic
model: generic-classic
serial: 03961d5d-26e5-443f-838d-6db046126bea
groups:
  -
    operator-id: john
    authentication:
      - operator-key
    views:
      - canonical/network/control-device
      - canonical/network/observe-device
  -
    operator-id: john
    authentication:
      - store
    views:
      - canonical/network/control-interfaces
  -
    operator-id: jane
    authentication:
      - store
      - operator-key
    views:
      - canonical/network/observe-interfaces
sign-key-sha3-384: t9yuKGLyiezBq_PXMJZsGdkTukmL7MgrgqXAlxxiZF4TYryOjZcy48nnjDmEHQDp

AXNpZw==`
)

func (s *confdbCtrlSuite) TestDecodeOK(c *C) {
	encoded := confdbControlExample

	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Assert(a, NotNil)
	c.Assert(a.Type(), Equals, asserts.ConfdbControlType)

	cc := a.(*asserts.ConfdbControl)
	c.Assert(cc.BrandID(), Equals, "generic")
	c.Assert(cc.Model(), Equals, "generic-classic")
	c.Assert(cc.Serial(), Equals, "03961d5d-26e5-443f-838d-6db046126bea")
	c.Assert(cc.AuthorityID(), Equals, "")

	operators := cc.Operators()

	john, ok := operators["john"]
	c.Assert(ok, Equals, true)
	c.Assert(john.ID, Equals, "john")
	c.Assert(len(john.Groups), Equals, 2)

	g := john.Groups[0]
	c.Assert(g.Authentication, DeepEquals, []confdb.AuthenticationMethod{"operator-key"})
	c.Assert(g.Views, DeepEquals, []string{"canonical/network/control-device", "canonical/network/observe-device"})

	g = john.Groups[1]
	c.Assert(g.Authentication, DeepEquals, []confdb.AuthenticationMethod{"store"})
	c.Assert(g.Views, DeepEquals, []string{"canonical/network/control-interfaces"})

	jane, ok := operators["jane"]
	c.Assert(ok, Equals, true)
	c.Assert(jane.ID, Equals, "jane")
	c.Assert(len(jane.Groups), Equals, 1)

	g = jane.Groups[0]
	c.Assert(g.Authentication, DeepEquals, []confdb.AuthenticationMethod{"operator-key", "store"})
	c.Assert(g.Views, DeepEquals, []string{"canonical/network/observe-interfaces"})
}

func (s *confdbCtrlSuite) TestDecodeInvalid(c *C) {
	encoded := confdbControlExample
	const validationSetErrPrefix = "assertion confdb-control: "

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"brand-id: generic\n", "", `"brand-id" header is mandatory`},
		{"brand-id: generic\n", "brand-id: \n", `"brand-id" header should not be empty`},
		{"brand-id: generic\n", "brand-id: 456#\n", `"brand-id" header contains invalid characters: "456#"`},
		{"model: generic-classic\n", "", `"model" header is mandatory`},
		{"model: generic-classic\n", "model: \n", `"model" header should not be empty`},
		{"model: generic-classic\n", "model: #\n", `"model" header contains invalid characters: "#"`},
		{"serial: 03961d5d-26e5-443f-838d-6db046126bea\n", "", `"serial" header is mandatory`},
		{"serial: 03961d5d-26e5-443f-838d-6db046126bea\n", "serial: \n", `"serial" header should not be empty`},
		{"groups:", "groups: foo\nviews:", `"groups" header must be a list`},
		{"groups:", "views:", `"groups" stanza is mandatory`},
		{"groups:", "groups:\n  - bar", `group at position 1: must be a map`},
		{"    operator-id: jane\n", "", `group at position 3: "operator-id" field is mandatory`},
		{
			"operator-id: jane\n",
			"operator-id: \n",
			`group at position 3: "operator-id" field should not be empty`,
		},
		{
			"operator-id: jane\n",
			"operator-id: @op\n",
			`group at position 3: invalid "operator-id" @op`,
		},
		{
			"    authentication:\n      - store",
			"    authentication: abcd",
			`group at position 2: "authentication" field must be a list of strings`,
		},
		{
			"    authentication:\n      - store",
			"    foo: bar",
			`group at position 2: "authentication" must be provided`,
		},
		{
			"    views:\n      - canonical/network/control-interfaces",
			"    views: abcd",
			`group at position 2: "views" field must be a list of strings`,
		},
		{
			"    views:\n      - canonical/network/control-interfaces",
			"    foo: bar",
			`group at position 2: "views" must be provided`,
		},
		{
			"      - operator-key",
			"      - foo-bar",
			"group at position 1: invalid authentication method: foo-bar",
		},
		{
			"canonical/network/control-interfaces",
			"canonical",
			`group at position 2: "canonical" must be in the format account/confdb/view`,
		},
	}

	for i, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Assert(err, ErrorMatches, validationSetErrPrefix+test.expectedErr, Commentf("test %d/%d failed", i+1, len(invalidTests)))
	}
}
