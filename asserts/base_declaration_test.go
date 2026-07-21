// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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

	"github.com/snapcore/snapd/asserts"
)

type baseDeclSuite struct{}

var _ = Suite(&baseDeclSuite{})

const baseDecl = `type: base-declaration
authority-id: canonical
series: 16
plugs:
  interface1:
    deny-installation: false
    allow-auto-connection:
      slot-snap-type:
        - app
      slot-publisher-id:
        - acme
      slot-attributes:
        a1: /foo/.*
      plug-attributes:
        b1: B1
    deny-auto-connection:
      slot-attributes:
        a1: !A1
      plug-attributes:
        b1: !B1
  interface2:
    allow-installation: true
    allow-connection:
      plug-attributes:
        a2: A2
      slot-attributes:
        b2: B2
    deny-connection:
      slot-snap-id:
        - snapidsnapidsnapidsnapidsnapid01
        - snapidsnapidsnapidsnapidsnapid02
      plug-attributes:
        a2: !A2
      slot-attributes:
        b2: !B2
slots:
  interface3:
    deny-installation: false
    allow-auto-connection:
      plug-snap-type:
        - app
      plug-publisher-id:
        - acme
      slot-attributes:
        c1: /foo/.*
      plug-attributes:
        d1: C1
    deny-auto-connection:
      slot-attributes:
        c1: !C1
      plug-attributes:
        d1: !D1
  interface4:
    allow-connection:
      plug-attributes:
        c2: C2
      slot-attributes:
        d2: D2
    deny-connection:
      plug-snap-id:
        - snapidsnapidsnapidsnapidsnapid01
        - snapidsnapidsnapidsnapidsnapid02
      plug-attributes:
        c2: !D2
      slot-attributes:
        d2: !D2
    allow-installation:
      slot-snap-type:
        - app
      slot-attributes:
        e1: E1
timestamp: 2016-09-29T19:50:49Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==
`

func (s *baseDeclSuite) TestDecodeOK(c *C) {
	a, err := asserts.Decode([]byte(baseDecl))
	c.Assert(err, IsNil)
	baseDecl := a.(*asserts.BaseDeclaration)
	c.Check(baseDecl.Series(), Equals, "16")
	ts, err := time.Parse(time.RFC3339, "2016-09-29T19:50:49Z")
	c.Assert(err, IsNil)
	c.Check(baseDecl.Timestamp().Equal(ts), Equals, true)

	c.Check(baseDecl.PlugRule("interfaceX"), IsNil)
	c.Check(baseDecl.SlotRule("interfaceX"), IsNil)

	plug := emptyAttrerObject{}
	slot := emptyAttrerObject{}

	plugRule1 := baseDecl.PlugRule("interface1")
	c.Assert(plugRule1, NotNil)
	c.Assert(plugRule1.DenyInstallation, HasLen, 1)
	c.Check(plugRule1.DenyInstallation[0].PlugAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(plugRule1.AllowAutoConnection, HasLen, 1)
	c.Check(plugRule1.AllowAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "b1".*`)
	c.Check(plugRule1.AllowAutoConnection[0].SlotSnapTypes, DeepEquals, []string{"app"})
	c.Check(plugRule1.AllowAutoConnection[0].SlotPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(plugRule1.DenyAutoConnection, HasLen, 1)
	c.Check(plugRule1.DenyAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "a1".*`)
	c.Check(plugRule1.DenyAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "b1".*`)
	plugRule2 := baseDecl.PlugRule("interface2")
	c.Assert(plugRule2, NotNil)
	c.Assert(plugRule2.AllowInstallation, HasLen, 1)
	c.Check(plugRule2.AllowInstallation[0].PlugAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Assert(plugRule2.AllowConnection, HasLen, 1)
	c.Check(plugRule2.AllowConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.AllowConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "b2".*`)
	c.Assert(plugRule2.DenyConnection, HasLen, 1)
	c.Check(plugRule2.DenyConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "a2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "b2".*`)
	c.Check(plugRule2.DenyConnection[0].SlotSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})

	slotRule3 := baseDecl.SlotRule("interface3")
	c.Assert(slotRule3, NotNil)
	c.Assert(slotRule3.DenyInstallation, HasLen, 1)
	c.Check(slotRule3.DenyInstallation[0].SlotAttributes, Equals, asserts.NeverMatchAttributes)
	c.Assert(slotRule3.AllowAutoConnection, HasLen, 1)
	c.Check(slotRule3.AllowAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "d1".*`)
	c.Check(slotRule3.AllowAutoConnection[0].PlugSnapTypes, DeepEquals, []string{"app"})
	c.Check(slotRule3.AllowAutoConnection[0].PlugPublisherIDs, DeepEquals, []string{"acme"})
	c.Assert(slotRule3.DenyAutoConnection, HasLen, 1)
	c.Check(slotRule3.DenyAutoConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "c1".*`)
	c.Check(slotRule3.DenyAutoConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "d1".*`)
	slotRule4 := baseDecl.SlotRule("interface4")
	c.Assert(slotRule4, NotNil)
	c.Assert(slotRule4.AllowConnection, HasLen, 1)
	c.Check(slotRule4.AllowConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.AllowConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "d2".*`)
	c.Assert(slotRule4.DenyConnection, HasLen, 1)
	c.Check(slotRule4.DenyConnection[0].PlugAttributes.Check(plug, nil), ErrorMatches, `attribute "c2".*`)
	c.Check(slotRule4.DenyConnection[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "d2".*`)
	c.Check(slotRule4.DenyConnection[0].PlugSnapIDs, DeepEquals, []string{"snapidsnapidsnapidsnapidsnapid01", "snapidsnapidsnapidsnapidsnapid02"})
	c.Assert(slotRule4.AllowInstallation, HasLen, 1)
	c.Check(slotRule4.AllowInstallation[0].SlotAttributes.Check(slot, nil), ErrorMatches, `attribute "e1".*`)
	c.Check(slotRule4.AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"app"})

}

func (s *baseDeclSuite) TestBaseDeclarationCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := map[string]any{
		"series":    "16",
		"timestamp": time.Now().Format(time.RFC3339),
	}
	baseDecl, err := otherDB.Sign(asserts.BaseDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(baseDecl)
	c.Assert(err, ErrorMatches, `base-declaration assertion for series 16 is not signed by a directly trusted authority: other`)
}

const (
	baseDeclErrPrefix = "assertion base-declaration: "
)

func (s *baseDeclSuite) TestDecodeInvalid(c *C) {
	tsLine := "timestamp: 2016-09-29T19:50:49Z\n"

	encoded := "type: base-declaration\n" +
		"authority-id: canonical\n" +
		"series: 16\n" +
		"plugs:\n  interface1: true\n" +
		"slots:\n  interface2: true\n" +
		tsLine +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"series: 16\n", "", `"series" header is mandatory`},
		{"series: 16\n", "series: \n", `"series" header should not be empty`},
		{"plugs:\n  interface1: true\n", "plugs: \n", `"plugs" header must be a map`},
		{"plugs:\n  interface1: true\n", "plugs:\n  intf1:\n    foo: bar\n", `plug rule for interface "intf1" must specify at least one of.*`},
		{"slots:\n  interface2: true\n", "slots: \n", `"slots" header must be a map`},
		{"slots:\n  interface2: true\n", "slots:\n  intf1:\n    foo: bar\n", `slot rule for interface "intf1" must specify at least one of.*`},
		{tsLine, "", `"timestamp" header is mandatory`},
		{tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, baseDeclErrPrefix+test.expectedErr)
	}
}

func (s *baseDeclSuite) TestBuiltinBaseDeclaration(c *C) {
	const headers = `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
plugs:
  network: true
slots:
  network:
    allow-installation:
      slot-snap-type:
        - core
`

	err := asserts.InitBuiltinBaseDeclaration([]byte(headers))
	c.Assert(err, IsNil)

	baseDecl := asserts.BuiltinBaseDeclaration()
	c.Assert(baseDecl, NotNil)

	cont, _ := baseDecl.Signature()
	c.Check(strings.HasPrefix(string(cont), strings.TrimSpace(headers)), Equals, true)
	c.Check(strings.Contains(string(cont), "timestamp:"), Equals, true)

	c.Check(baseDecl.AuthorityID(), Equals, "canonical")
	c.Check(baseDecl.Series(), Equals, "16")
	c.Check(baseDecl.PlugRule("network").AllowAutoConnection[0].SlotAttributes, Equals, asserts.AlwaysMatchAttributes)
	c.Check(baseDecl.SlotRule("network").AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"core"})

	enc := asserts.Encode(baseDecl)
	decoded, err := asserts.Decode(enc)
	c.Assert(err, IsNil)
	c.Check(decoded.Type(), Equals, asserts.BaseDeclarationType)
}

func (s *baseDeclSuite) TestBuiltinInitErrors(c *C) {
	defer asserts.InitBuiltinBaseDeclaration(nil)

	tests := []struct {
		headers string
		err     string
	}{
		{"", `header entry missing ':' separator: ""`},
		{"type: foo\n", `the builtin base-declaration "type" header is not set to expected value "base-declaration"`},
		{"type: base-declaration\n", `the builtin base-declaration "authority-id" header is not set to expected value "canonical"`},
		{"type: base-declaration\nauthority-id: canonical", `the builtin base-declaration "series" header is not set to expected value "16"`},
		{"type: base-declaration\nauthority-id: canonical\nseries: 16\nrevision: zzz", `cannot assemble the builtin base-declaration: "revision" header is not an integer: zzz`},
		{"type: base-declaration\nauthority-id: canonical\nseries: 16\nplugs: foo", `cannot assemble the builtin base-declaration: "plugs" header must be a map`},
	}

	for _, t := range tests {
		err := asserts.InitBuiltinBaseDeclaration([]byte(t.headers))
		c.Check(err, ErrorMatches, t.err, Commentf(t.headers))
	}
}
