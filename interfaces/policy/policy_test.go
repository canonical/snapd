// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package policy_test

import (
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func TestPolicy(t *testing.T) { TestingT(t) }

type policySuite struct {
	baseDecl *asserts.BaseDeclaration

	plugSnap *snap.Info
	slotSnap *snap.Info

	plugDecl *asserts.SnapDeclaration
	slotDecl *asserts.SnapDeclaration

	randomSnap *snap.Info
	randomDecl *asserts.SnapDeclaration

	restoreSanitize func()
}

var _ = Suite(&policySuite{})

func (s *policySuite) SetUpSuite(c *C) {
	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	a, err := asserts.Decode([]byte(`type: base-declaration
authority-id: canonical
series: 16
plugs:
  base-plug-allow: true
  base-plug-not-allow:
    allow-connection: false
  base-plug-not-allow-slots:
    allow-connection:
      slot-attributes:
        s: S
  base-plug-not-allow-plugs:
    allow-connection:
      plug-attributes:
        p: P
  base-plug-deny:
    deny-connection: true
  same-plug-publisher-id:
    allow-connection:
      slot-publisher-id:
        - $PLUG_PUBLISHER_ID
  plug-plug-attr:
    allow-connection:
      slot-attributes:
        c: $PLUG(c)
  plug-slot-attr:
    allow-connection:
      plug-attributes:
        c: $SLOT(c)
  plug-or:
    allow-connection:
      -
        slot-attributes:
          s: S1
        plug-attributes:
          p: P1
      -
        slot-attributes:
          s: S2
        plug-attributes:
          p: P2
  plug-on-classic-true:
    allow-connection:
      on-classic: true
  plug-on-classic-distros:
    allow-connection:
      on-classic:
        - ubuntu
        - debian
  plug-on-classic-false:
    allow-connection:
      on-classic: false
  auto-base-plug-allow: true
  auto-base-plug-not-allow:
    allow-auto-connection: false
  auto-base-plug-not-allow-slots:
    allow-auto-connection:
      slot-attributes:
        s: S
  auto-base-plug-not-allow-plugs:
    allow-auto-connection:
      plug-attributes:
        p: P
  auto-base-plug-deny:
    deny-auto-connection: true
  auto-plug-or:
    allow-auto-connection:
      -
        slot-attributes:
          s: S1
        plug-attributes:
          p: P1
      -
        slot-attributes:
          s: S2
        plug-attributes:
          p: P2
  install-plug-attr-ok:
    allow-installation:
      plug-attributes:
        attr: ok
  install-plug-gadget-only:
    allow-installation:
      plug-snap-type:
        - gadget
  install-plug-base-deny-snap-allow:
    deny-installation:
      plug-attributes:
        attr: attrvalue
  install-plug-or:
    deny-installation:
      -
        plug-attributes:
          p: P1
      -
        plug-snap-type:
          - gadget
        plug-attributes:
          p: P2
  install-plug-on-classic-distros:
    allow-installation:
      on-classic:
        - ubuntu
        - debian
slots:
  base-slot-allow: true
  base-slot-not-allow:
    allow-connection: false
  base-slot-not-allow-slots:
    allow-connection:
      slot-attributes:
        s: S
  base-slot-not-allow-plugs:
    allow-connection:
      plug-attributes:
        p: P
  base-slot-deny:
    deny-connection: true
  base-deny-snap-slot-allow: false
  base-deny-snap-plug-allow: false
  base-allow-snap-slot-not-allow: true
  gadgethelp:
    allow-connection:
      plug-snap-type:
        - gadget
  same-slot-publisher-id:
    allow-connection:
      plug-publisher-id:
        - $SLOT_PUBLISHER_ID
  slot-slot-attr:
    allow-connection:
      plug-attributes:
        a:
          b: $SLOT(a.b)
  slot-plug-attr:
    allow-connection:
      slot-attributes:
        c: $PLUG(c)
  slot-plug-missing:
    allow-connection:
      plug-attributes:
        x: $MISSING
  slot-or:
    allow-connection:
      -
        slot-attributes:
          s: S1
        plug-attributes:
          p: P1
      -
        slot-attributes:
          s: S2
        plug-attributes:
          p: P2
  slot-on-classic-true:
    allow-connection:
      on-classic: true
  slot-on-classic-distros:
    allow-connection:
      on-classic:
        - ubuntu
        - debian
  slot-on-classic-false:
    allow-connection:
      on-classic: false
  auto-base-slot-allow: true
  auto-base-slot-not-allow:
    allow-auto-connection: false
  auto-base-slot-not-allow-slots:
    allow-auto-connection:
      slot-attributes:
        s: S
  auto-base-slot-not-allow-plugs:
    allow-auto-connection:
      plug-attributes:
        p: P
  auto-base-slot-deny:
    deny-auto-connection: true
  auto-base-deny-snap-slot-allow: false
  auto-base-deny-snap-plug-allow: false
  auto-base-allow-snap-slot-not-allow: true
  auto-slot-or:
    allow-auto-connection:
      -
        slot-attributes:
          s: S1
        plug-attributes:
          p: P1
      -
        slot-attributes:
          s: S2
        plug-attributes:
          p: P2
  install-slot-coreonly:
    allow-installation:
      slot-snap-type:
        - core
  install-slot-attr-ok:
    allow-installation:
      slot-attributes:
        attr: ok
  install-slot-attr-deny:
    deny-installation:
      slot-attributes:
        trust: trusted
  install-slot-base-deny-snap-allow:
    deny-installation:
      slot-attributes:
        have: true
  install-slot-or:
    deny-installation:
      -
        slot-attributes:
          p: P1
      -
        slot-snap-type:
          - gadget
        slot-attributes:
          p: P2
  install-slot-on-classic-distros:
    allow-installation:
      on-classic:
        - ubuntu
        - debian
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	s.baseDecl = a.(*asserts.BaseDeclaration)

	s.plugSnap = snaptest.MockInfo(c, `
name: plug-snap
version: 0
plugs:
   random:
   mismatchy:
     interface: bar

   base-plug-allow:
   base-plug-not-allow:
   base-plug-not-allow-slots:
   base-plug-not-allow-plugs:
   base-plug-deny:

   base-slot-allow:
   base-slot-not-allow:
   base-slot-not-allow-slots:
   base-slot-not-allow-plugs:
   base-slot-deny:

   auto-base-plug-allow:
   auto-base-plug-not-allow:
   auto-base-plug-not-allow-slots:
   auto-base-plug-not-allow-plugs:
   auto-base-plug-deny:

   auto-base-slot-allow:
   auto-base-slot-not-allow:
   auto-base-slot-not-allow-slots:
   auto-base-slot-not-allow-plugs:
   auto-base-slot-deny:

   snap-plug-allow:
   snap-plug-not-allow:
   snap-plug-deny:

   snap-slot-allow:
   snap-slot-not-allow:
   snap-slot-deny:

   base-deny-snap-slot-allow:
   base-deny-snap-plug-allow:
   base-allow-snap-slot-not-allow:

   snap-slot-deny-snap-plug-allow:

   auto-snap-plug-allow:
   auto-snap-plug-not-allow:
   auto-snap-plug-deny:

   auto-snap-slot-allow:
   auto-snap-slot-not-allow:
   auto-snap-slot-deny:

   auto-base-deny-snap-slot-allow:
   auto-base-deny-snap-plug-allow:
   auto-base-allow-snap-slot-not-allow:

   auto-snap-slot-deny-snap-plug-allow:

   gadgethelp:
   trustedhelp:

   precise-plug-snap-id:
   precise-slot-snap-id:

   checked-plug-publisher-id:
   checked-slot-publisher-id:

   same-plug-publisher-id:

   slot-slot-attr-mismatch:
     interface: slot-slot-attr
     a:
       b: []

   slot-slot-attr-match:
     interface: slot-slot-attr
     a:
       b: ["x", "y"]

   slot-plug-attr-mismatch:
     interface: slot-plug-attr
     c: "Z"

   slot-plug-attr-dynamic:
     interface: slot-plug-attr

   slot-plug-attr-match:
     interface: slot-plug-attr
     c: "C"

   slot-plug-missing-mismatch:
     interface: slot-plug-missing
     x: 1
     z: 2

   slot-plug-missing-match:
     interface: slot-plug-missing
     z: 2

   plug-plug-attr:
     c: "C"

   plug-slot-attr:
     c: "C"

   plug-or-p1-s1:
     interface: plug-or
     p: P1

   plug-or-p2-s2:
     interface: plug-or
     p: P2

   plug-or-p1-s2:
     interface: plug-or
     p: P1

   auto-plug-or-p1-s1:
     interface: auto-plug-or
     p: P1

   auto-plug-or-p2-s2:
     interface: auto-plug-or
     p: P2

   auto-plug-or-p2-s1:
     interface: auto-plug-or
     p: P2

   slot-or-p1-s1:
     interface: slot-or
     p: P1

   slot-or-p2-s2:
     interface: slot-or
     p: P2

   slot-or-p1-s2:
     interface: slot-or
     p: P1

   auto-slot-or-p1-s1:
     interface: auto-slot-or
     p: P1

   auto-slot-or-p2-s2:
     interface: auto-slot-or
     p: P2

   auto-slot-or-p2-s1:
     interface: auto-slot-or
     p: P2

   slot-on-classic-true:
   slot-on-classic-distros:
   slot-on-classic-false:

   plug-on-classic-true:
   plug-on-classic-distros:
   plug-on-classic-false:
`, nil)

	s.slotSnap = snaptest.MockInfo(c, `
name: slot-snap
version: 0
slots:
   random:
   mismatchy:
     interface: baz

   base-plug-allow:
   base-plug-not-allow:
   base-plug-not-allow-slots:
   base-plug-not-allow-plugs:
   base-plug-deny:

   base-slot-allow:
   base-slot-not-allow:
   base-slot-not-allow-slots:
   base-slot-not-allow-plugs:
   base-slot-deny:

   auto-base-plug-allow:
   auto-base-plug-not-allow:
   auto-base-plug-not-allow-slots:
   auto-base-plug-not-allow-plugs:
   auto-base-plug-deny:

   auto-base-slot-allow:
   auto-base-slot-not-allow:
   auto-base-slot-not-allow-slots:
   auto-base-slot-not-allow-plugs:
   auto-base-slot-deny:

   snap-plug-allow:
   snap-plug-not-allow:
   snap-plug-deny:

   snap-slot-allow:
   snap-slot-not-allow:
   snap-slot-deny:

   base-deny-snap-slot-allow:
   base-deny-snap-plug-allow:
   base-allow-snap-slot-not-allow:

   snap-slot-deny-snap-plug-allow:

   auto-snap-plug-allow:
   auto-snap-plug-not-allow:
   auto-snap-plug-deny:

   auto-snap-slot-allow:
   auto-snap-slot-not-allow:
   auto-snap-slot-deny:

   auto-base-deny-snap-slot-allow:
   auto-base-deny-snap-plug-allow:
   auto-base-allow-snap-slot-not-allow:

   auto-snap-slot-deny-snap-plug-allow:

   trustedhelp:

   precise-plug-snap-id:
   precise-slot-snap-id:

   checked-plug-publisher-id:
   checked-slot-publisher-id:

   same-slot-publisher-id:

   slot-slot-attr:
     a:
       b: ["x", "y"]

   slot-plug-attr:
     c: "C"

   slot-plug-missing:

   plug-plug-attr-mismatch:
     interface: plug-plug-attr
     c: "Z"

   plug-plug-attr-match:
     interface: plug-plug-attr
     c: "C"

   plug-plug-attr-dynamic:
     interface: plug-plug-attr

   plug-slot-attr-mismatch:
     interface: plug-slot-attr
     c: "Z"

   plug-slot-attr-match:
     interface: plug-slot-attr
     c: "C"

   plug-or-p1-s1:
     interface: plug-or
     s: S1

   plug-or-p2-s2:
     interface: plug-or
     s: S2

   plug-or-p1-s2:
     interface: plug-or
     s: S2

   auto-plug-or-p1-s1:
     interface: auto-plug-or
     s: S1

   auto-plug-or-p2-s2:
     interface: auto-plug-or
     s: S2

   auto-plug-or-p2-s1:
     interface: auto-plug-or
     s: S1

   slot-or-p1-s1:
     interface: slot-or
     s: S1

   slot-or-p2-s2:
     interface: slot-or
     s: S2

   slot-or-p1-s2:
     interface: slot-or
     s: S2

   auto-slot-or-p1-s1:
     interface: auto-slot-or
     s: S1

   auto-slot-or-p2-s2:
     interface: auto-slot-or
     s: S2

   auto-slot-or-p2-s1:
     interface: auto-slot-or
     s: S1

   slot-on-classic-true:
   slot-on-classic-distros:
   slot-on-classic-false:

   plug-on-classic-true:
   plug-on-classic-distros:
   plug-on-classic-false:
`, nil)

	a, err = asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: plug-snap
snap-id: plugsnapidididididididididididid
publisher-id: plug-publisher
plugs:
  snap-plug-allow: true
  snap-plug-deny: false
  snap-plug-not-allow:
    allow-connection: false
  base-deny-snap-plug-allow: true
  snap-slot-deny-snap-plug-allow:
    deny-connection: false
  trustedhelp:
    allow-connection:
      slot-snap-type:
        - core
        - gadget
  precise-slot-snap-id:
    allow-connection:
      slot-snap-id:
        - slotsnapidididididididididididid
  checked-slot-publisher-id:
    allow-connection:
      slot-publisher-id:
        - slot-publisher
        - $PLUG_PUBLISHER_ID
  auto-snap-plug-allow: true
  auto-snap-plug-deny: false
  auto-snap-plug-not-allow:
    allow-auto-connection: false
  auto-snap-slot-deny-snap-plug-allow:
    deny-auto-connection: false
  auto-base-deny-snap-plug-allow: true
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	s.plugDecl = a.(*asserts.SnapDeclaration)

	a, err = asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: slot-snap
snap-id: slotsnapidididididididididididid
publisher-id: slot-publisher
slots:
  snap-slot-allow: true
  snap-slot-deny: false
  snap-slot-not-allow:
    allow-connection: false
  base-deny-snap-slot-allow: true
  snap-slot-deny-snap-plug-allow:
    deny-connection: true
  base-allow-snap-slot-not-allow:
    allow-connection: false
  precise-plug-snap-id:
    allow-connection:
      plug-snap-id:
        - plugsnapidididididididididididid
  checked-plug-publisher-id:
    allow-connection:
      plug-publisher-id:
        - plug-publisher
  auto-snap-slot-allow: true
  auto-snap-slot-deny: false
  auto-snap-slot-not-allow:
    allow-auto-connection: false
  auto-base-deny-snap-slot-allow: true
  auto-snap-slot-deny-snap-plug-allow:
    deny-auto-connection: true
  auto-base-allow-snap-slot-not-allow:
    allow-auto-connection: false
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	s.slotDecl = a.(*asserts.SnapDeclaration)

	s.randomSnap = snaptest.MockInfo(c, `
name: random-snap
version: 0
plugs:
  precise-plug-snap-id:
  checked-plug-publisher-id:
  same-slot-publisher-id:
  slot-slot-attr:
slots:
  precise-slot-snap-id:
  checked-slot-publisher-id:
  same-plug-publisher-id:
`, nil)

	a, err = asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: random-snap
snap-id: randomsnapididididididididid
publisher-id: random-publisher
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	s.randomDecl = a.(*asserts.SnapDeclaration)
}

func (s *policySuite) TearDownSuite(c *C) {
	s.restoreSanitize()
}

func (s *policySuite) TestBaselineDefaultIsAllow(c *C) {
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["random"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["random"], nil),
		BaseDeclaration: s.baseDecl,
	}

	c.Check(cand.Check(), IsNil)
	c.Check(cand.CheckAutoConnect(), IsNil)
}

func (s *policySuite) TestInterfaceMismatch(c *C) {
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["mismatchy"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["mismatchy"], nil),
		BaseDeclaration: s.baseDecl,
	}

	c.Check(cand.Check(), ErrorMatches, `cannot connect mismatched plug interface "bar" to slot interface "baz"`)
}

func (s *policySuite) TestBaseDeclAllowDenyConnection(c *C) {
	tests := []struct {
		iface    string
		expected string // "" => no error
	}{
		{"base-plug-allow", ""},
		{"base-plug-deny", `connection denied by plug rule of interface "base-plug-deny"`},
		{"base-plug-not-allow", `connection not allowed by plug rule of interface "base-plug-not-allow"`},
		{"base-slot-allow", ""},
		{"base-slot-deny", `connection denied by slot rule of interface "base-slot-deny"`},
		{"base-slot-not-allow", `connection not allowed by slot rule of interface "base-slot-not-allow"`},
		{"base-plug-not-allow-slots", `connection not allowed.*`},
		{"base-slot-not-allow-slots", `connection not allowed.*`},
		{"base-plug-not-allow-plugs", `connection not allowed.*`},
		{"base-slot-not-allow-plugs", `connection not allowed.*`},
		{"plug-or-p1-s1", ""},
		{"plug-or-p2-s2", ""},
		{"plug-or-p1-s2", "connection not allowed by plug rule.*"},
		{"slot-or-p1-s1", ""},
		{"slot-or-p2-s2", ""},
		{"slot-or-p1-s2", "connection not allowed by slot rule.*"},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil),
			BaseDeclaration: s.baseDecl,
		}

		err := cand.Check()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestBaseDeclAllowDenyAutoConnection(c *C) {
	tests := []struct {
		iface    string
		expected string // "" => no error
	}{
		{"auto-base-plug-allow", ""},
		{"auto-base-plug-deny", `auto-connection denied by plug rule of interface "auto-base-plug-deny"`},
		{"auto-base-plug-not-allow", `auto-connection not allowed by plug rule of interface "auto-base-plug-not-allow"`},
		{"auto-base-slot-allow", ""},
		{"auto-base-slot-deny", `auto-connection denied by slot rule of interface "auto-base-slot-deny"`},
		{"auto-base-slot-not-allow", `auto-connection not allowed by slot rule of interface "auto-base-slot-not-allow"`},
		{"auto-base-plug-not-allow-slots", `auto-connection not allowed.*`},
		{"auto-base-slot-not-allow-slots", `auto-connection not allowed.*`},
		{"auto-base-plug-not-allow-plugs", `auto-connection not allowed.*`},
		{"auto-base-slot-not-allow-plugs", `auto-connection not allowed.*`},
		{"auto-plug-or-p1-s1", ""},
		{"auto-plug-or-p2-s2", ""},
		{"auto-plug-or-p2-s1", "auto-connection not allowed by plug rule.*"},
		{"auto-slot-or-p1-s1", ""},
		{"auto-slot-or-p2-s2", ""},
		{"auto-slot-or-p2-s1", "auto-connection not allowed by slot rule.*"},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil),
			BaseDeclaration: s.baseDecl,
		}

		err := cand.CheckAutoConnect()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestSnapDeclAllowDenyConnection(c *C) {
	tests := []struct {
		iface    string
		expected string // "" => no error
	}{
		{"random", ""},
		{"snap-plug-allow", ""},
		{"snap-plug-deny", `connection denied by plug rule of interface "snap-plug-deny" for "plug-snap" snap`},
		{"snap-plug-not-allow", `connection not allowed by plug rule of interface "snap-plug-not-allow" for "plug-snap" snap`},
		{"snap-slot-allow", ""},
		{"snap-slot-deny", `connection denied by slot rule of interface "snap-slot-deny" for "slot-snap" snap`},
		{"snap-slot-not-allow", `connection not allowed by slot rule of interface "snap-slot-not-allow" for "slot-snap" snap`},
		{"base-deny-snap-slot-allow", ""},
		{"base-deny-snap-plug-allow", ""},
		{"snap-slot-deny-snap-plug-allow", ""},
		{"base-allow-snap-slot-not-allow", `connection not allowed.*`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,
			BaseDeclaration:     s.baseDecl,
		}

		err := cand.Check()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestSnapDeclAllowDenyAutoConnection(c *C) {
	tests := []struct {
		iface    string
		expected string // "" => no error
	}{
		{"random", ""},
		{"auto-snap-plug-allow", ""},
		{"auto-snap-plug-deny", `auto-connection denied by plug rule of interface "auto-snap-plug-deny" for "plug-snap" snap`},
		{"auto-snap-plug-not-allow", `auto-connection not allowed by plug rule of interface "auto-snap-plug-not-allow" for "plug-snap" snap`},
		{"auto-snap-slot-allow", ""},
		{"auto-snap-slot-deny", `auto-connection denied by slot rule of interface "auto-snap-slot-deny" for "slot-snap" snap`},
		{"auto-snap-slot-not-allow", `auto-connection not allowed by slot rule of interface "auto-snap-slot-not-allow" for "slot-snap" snap`},
		{"auto-base-deny-snap-slot-allow", ""},
		{"auto-base-deny-snap-plug-allow", ""},
		{"auto-snap-slot-deny-snap-plug-allow", ""},
		{"auto-base-allow-snap-slot-not-allow", `auto-connection not allowed.*`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,
			BaseDeclaration:     s.baseDecl,
		}

		err := cand.CheckAutoConnect()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestSnapTypeCheckConnection(c *C) {
	gadgetSnap := snaptest.MockInfo(c, `
name: gadget
version: 0
type: gadget
plugs:
   gadgethelp:
slots:
   trustedhelp:
`, nil)

	coreSnap := snaptest.MockInfo(c, `
name: core
version: 0
type: os
slots:
   gadgethelp:
   trustedhelp:
`, nil)

	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(gadgetSnap.Plugs["gadgethelp"], nil),
		Slot:            interfaces.NewConnectedSlot(coreSnap.Slots["gadgethelp"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)

	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["gadgethelp"], nil),
		Slot:            interfaces.NewConnectedSlot(coreSnap.Slots["gadgethelp"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	for _, trustedSide := range []*snap.Info{coreSnap, gadgetSnap} {
		cand = policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["trustedhelp"], nil),
			PlugSnapDeclaration: s.plugDecl,
			Slot:                interfaces.NewConnectedSlot(trustedSide.Slots["trustedhelp"], nil),
			BaseDeclaration:     s.baseDecl,
		}
		c.Check(cand.Check(), IsNil)
	}

	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["trustedhelp"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["trustedhelp"], nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")
}

func (s *policySuite) TestPlugSnapIDCheckConnection(c *C) {
	// no plug-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["precise-plug-snap-id"], nil),
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-plug-snap-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["precise-plug-snap-id"], nil),
		PlugSnapDeclaration: s.randomDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-plug-snap-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-plug-snap-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-plug-snap-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotSnapIDCheckConnection(c *C) {
	// no slot-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-slot-snap-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["precise-slot-snap-id"], nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-slot-snap-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["precise-slot-snap-id"], nil),
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-slot-snap-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-slot-snap-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugPublisherIDCheckConnection(c *C) {
	// no plug-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["checked-plug-publisher-id"], nil),
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-plug-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["checked-plug-publisher-id"], nil),
		PlugSnapDeclaration: s.randomDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-plug-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-plug-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-plug-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotPublisherIDCheckConnection(c *C) {
	// no slot-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-slot-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["checked-slot-publisher-id"], nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-slot-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["checked-slot-publisher-id"], nil),
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-slot-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-slot-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarPlugPublisherIDCheckConnection(c *C) {
	// no known publishers
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil),
		Slot:            interfaces.NewConnectedSlot(s.randomSnap.Slots["same-plug-publisher-id"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// no slot-side declaration
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["same-plug-publisher-id"], nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["same-plug-publisher-id"], nil),
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot publisher id == plug publisher id
	samePubSlotSnap := snaptest.MockInfo(c, `
name: same-pub-slot-snap
version: 0
slots:
  same-plug-publisher-id:
`, nil)

	a, err := asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: same-pub-slot-snap
snap-id: samepublslotsnapidididididididid
publisher-id: plug-publisher
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	samePubSlotDecl := a.(*asserts.SnapDeclaration)

	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(samePubSlotSnap.Slots["same-plug-publisher-id"], nil),
		SlotSnapDeclaration: samePubSlotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarSlotPublisherIDCheckConnection(c *C) {
	// no known publishers
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.randomSnap.Plugs["same-slot-publisher-id"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// no plug-side declaration
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["same-slot-publisher-id"], nil),
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["same-slot-publisher-id"], nil),
		PlugSnapDeclaration: s.randomDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug publisher id == slot publisher id
	samePubPlugSnap := snaptest.MockInfo(c, `
name: same-pub-plug-snap
version: 0
plugs:
  same-slot-publisher-id:
`, nil)

	a, err := asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: same-pub-plug-snap
snap-id: samepublplugsnapidididididididid
publisher-id: slot-publisher
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	samePubPlugDecl := a.(*asserts.SnapDeclaration)

	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(samePubPlugSnap.Plugs["same-slot-publisher-id"], nil),
		PlugSnapDeclaration: samePubPlugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestBaselineDefaultIsAllowInstallation(c *C) {
	installSnap := snaptest.MockInfo(c, `
name: install-slot-snap
version: 0
slots:
  random1:
plugs:
  random2:
`, nil)

	cand := policy.InstallCandidate{
		Snap:            installSnap,
		BaseDeclaration: s.baseDecl,
	}

	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestBaseDeclAllowDenyInstallation(c *C) {

	tests := []struct {
		installYaml string
		expected    string // "" => no error
	}{
		{`name: install-snap
version: 0
slots:
  innocuous:
  install-slot-coreonly:
`, `installation not allowed by "install-slot-coreonly" slot rule of interface "install-slot-coreonly"`},
		{`name: install-snap
version: 0
slots:
  install-slot-attr-ok:
    attr: ok
`, ""},
		{`name: install-snap
version: 0
slots:
  install-slot-attr-deny:
    trust: trusted
`, `installation denied by "install-slot-attr-deny" slot rule of interface "install-slot-attr-deny"`},
		{`name: install-snap
version: 0
plugs:
  install-plug-attr-ok:
    attr: ok
`, ""},
		{`name: install-snap
version: 0
plugs:
  install-plug-attr-ok:
    attr: not-ok
`, `installation not allowed by "install-plug-attr-ok" plug rule of interface "install-plug-attr-ok"`},
		{`name: install-snap
version: 0
plugs:
  install-plug-gadget-only:
`, `installation not allowed by "install-plug-gadget-only" plug rule of interface "install-plug-gadget-only"`},
		{`name: install-gadget
version: 0
type: gadget
plugs:
  install-plug-gadget-only:
`, ""},
		{`name: install-gadget
version: 0
type: gadget
plugs:
  install-plug-or:
     p: P2`, `installation denied by "install-plug-or" plug rule.*`},
		{`name: install-snap
version: 0
plugs:
  install-plug-or:
     p: P1`, `installation denied by "install-plug-or" plug rule.*`},
		{`name: install-snap
version: 0
plugs:
  install-plug-or:
     p: P3`, ""},
		{`name: install-gadget
version: 0
type: gadget
slots:
  install-slot-or:
     p: P2`, `installation denied by "install-slot-or" slot rule.*`},
		{`name: install-snap
version: 0
slots:
  install-slot-or:
     p: P1`, `installation denied by "install-slot-or" slot rule.*`},
		{`name: install-snap
version: 0
slots:
  install-slot-or:
     p: P3`, ""},
	}

	for _, t := range tests {
		installSnap := snaptest.MockInfo(c, t.installYaml, nil)

		cand := policy.InstallCandidate{
			Snap:            installSnap,
			BaseDeclaration: s.baseDecl,
		}

		err := cand.Check()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestSnapDeclAllowDenyInstallation(c *C) {

	tests := []struct {
		installYaml string
		plugsSlots  string
		expected    string // "" => no error
	}{
		{`name: install-snap
version: 0
slots:
  install-slot-base-allow-snap-deny:
    have: yes # bool
`, `slots:
  install-slot-base-allow-snap-deny:
    deny-installation:
      slot-attributes:
        have: true
`, `installation denied by "install-slot-base-allow-snap-deny" slot rule of interface "install-slot-base-allow-snap-deny" for "install-snap" snap`},
		{`name: install-snap
version: 0
slots:
  install-slot-base-allow-snap-not-allow:
    have: yes # bool
`, `slots:
  install-slot-base-allow-snap-not-allow:
    allow-installation:
      slot-attributes:
        have: false
`, `installation not allowed by "install-slot-base-allow-snap-not-allow" slot rule of interface "install-slot-base-allow-snap-not-allow" for "install-snap" snap`},
		{`name: install-snap
version: 0
slots:
  install-slot-base-deny-snap-allow:
    have: yes
`, `slots:
  install-slot-base-deny-snap-allow:
    allow-installation: true
`, ""},
		{`name: install-snap
version: 0
plugs:
  install-plug-base-allow-snap-deny:
    attr: give-me
`, `plugs:
  install-plug-base-allow-snap-deny:
    deny-installation:
      plug-attributes:
        attr: .*
`, `installation denied by "install-plug-base-allow-snap-deny" plug rule of interface "install-plug-base-allow-snap-deny" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  install-plug-base-allow-snap-not-allow:
    attr: give-me
`, `plugs:
  install-plug-base-allow-snap-not-allow:
    allow-installation:
      plug-attributes:
        attr: minimal
`, `installation not allowed by "install-plug-base-allow-snap-not-allow" plug rule of interface "install-plug-base-allow-snap-not-allow" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  install-plug-base-deny-snap-allow:
    attr: attrvalue
`, `plugs:
  install-plug-base-deny-snap-allow:
    allow-installation:
      plug-attributes:
        attr: attrvalue
`, ""},
	}

	for _, t := range tests {
		installSnap := snaptest.MockInfo(c, t.installYaml, nil)

		a, err := asserts.Decode([]byte(strings.Replace(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: install-snap
snap-id: installsnap6idididididididididid
publisher-id: publisher
@plugsSlots@
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`, "@plugsSlots@", strings.TrimSpace(t.plugsSlots), 1)))
		c.Assert(err, IsNil)
		snapDecl := a.(*asserts.SnapDeclaration)

		cand := policy.InstallCandidate{
			Snap:            installSnap,
			SnapDeclaration: snapDecl,
			BaseDeclaration: s.baseDecl,
		}

		err = cand.Check()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestPlugOnClassicCheckConnection(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()
	r2 := release.MockReleaseInfo(&release.ReleaseInfo)
	defer r2()

	tests := []struct {
		distro string // "" => not classic
		iface  string
		err    string // "" => no error
	}{
		{"ubuntu", "plug-on-classic-true", ""},
		{"", "plug-on-classic-true", `connection not allowed by plug rule of interface "plug-on-classic-true"`},
		{"", "plug-on-classic-false", ""},
		{"ubuntu", "plug-on-classic-false", "connection not allowed.*"},
		{"ubuntu", "plug-on-classic-distros", ""},
		{"debian", "plug-on-classic-distros", ""},
		{"", "plug-on-classic-distros", "connection not allowed.*"},
		{"other", "plug-on-classic-distros", "connection not allowed.*"},
	}

	for _, t := range tests {
		if t.distro == "" {
			release.OnClassic = false
		} else {
			release.OnClassic = true
			release.ReleaseInfo = release.OS{
				ID: t.distro,
			}
		}
		cand := policy.ConnectCandidate{
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil),
			BaseDeclaration: s.baseDecl,
		}
		err := cand.Check()
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestSlotOnClassicCheckConnection(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()
	r2 := release.MockReleaseInfo(&release.ReleaseInfo)
	defer r2()

	tests := []struct {
		distro string // "" => not classic
		iface  string
		err    string // "" => no error
	}{
		{"ubuntu", "slot-on-classic-true", ""},
		{"", "slot-on-classic-true", `connection not allowed by slot rule of interface "slot-on-classic-true"`},
		{"", "slot-on-classic-false", ""},
		{"ubuntu", "slot-on-classic-false", "connection not allowed.*"},
		{"ubuntu", "slot-on-classic-distros", ""},
		{"debian", "slot-on-classic-distros", ""},
		{"", "slot-on-classic-distros", "connection not allowed.*"},
		{"other", "slot-on-classic-distros", "connection not allowed.*"},
	}

	for _, t := range tests {
		if t.distro == "" {
			release.OnClassic = false
		} else {
			release.OnClassic = true
			release.ReleaseInfo = release.OS{
				ID: t.distro,
			}
		}
		cand := policy.ConnectCandidate{
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil),
			BaseDeclaration: s.baseDecl,
		}
		err := cand.Check()
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestOnClassicInstallation(c *C) {
	r1 := release.MockOnClassic(false)
	defer r1()
	r2 := release.MockReleaseInfo(&release.ReleaseInfo)
	defer r2()

	tests := []struct {
		distro      string // "" => not classic
		installYaml string
		err         string // "" => no error
	}{
		{"", `name: install-snap
version: 0
slots:
  install-slot-on-classic-distros:`, `installation not allowed by "install-slot-on-classic-distros" slot rule.*`},
		{"debian", `name: install-snap
version: 0
slots:
  install-slot-on-classic-distros:`, ""},
		{"", `name: install-snap
version: 0
plugs:
  install-plug-on-classic-distros:`, `installation not allowed by "install-plug-on-classic-distros" plug rule.*`},
		{"debian", `name: install-snap
version: 0
plugs:
  install-plug-on-classic-distros:`, ""},
	}

	for _, t := range tests {
		if t.distro == "" {
			release.OnClassic = false
		} else {
			release.OnClassic = true
			release.ReleaseInfo = release.OS{
				ID: t.distro,
			}
		}

		installSnap := snaptest.MockInfo(c, t.installYaml, nil)

		cand := policy.InstallCandidate{
			Snap:            installSnap,
			BaseDeclaration: s.baseDecl,
		}
		err := cand.Check()
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestSlotDollarSlotAttrConnection(c *C) {
	// no corresponding attr
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.randomSnap.Plugs["slot-slot-attr"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-slot-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// different attr values
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-slot-attr-mismatch"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-slot-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-slot-attr-match"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-slot-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotDollarPlugAttrConnection(c *C) {
	// different attr values
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-mismatch"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-match"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugDollarPlugAttrConnection(c *C) {
	// different attr values
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-mismatch"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-match"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugDollarSlotAttrConnection(c *C) {
	// different attr values
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-slot-attr"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-slot-attr-mismatch"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-slot-attr"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-slot-attr-match"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarMissingConnection(c *C) {
	// not missing
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-missing-mismatch"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-missing"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// missing
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-missing-match"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-missing"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotDollarPlugDynamicAttrConnection(c *C) {
	// "c" attribute of the plug missing
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-dynamic"], map[string]interface{}{}),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr, "c" attribute of the plug provided by dynamic attribute
	cand = policy.ConnectCandidate{
		Plug: interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-dynamic"], map[string]interface{}{
			"c": "C",
		}),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugDollarSlotDynamicAttrConnection(c *C) {
	// "c" attribute of the slot missing
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-dynamic"], map[string]interface{}{}),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr, "c" attribute of the slot provided by dynamic attribute
	cand = policy.ConnectCandidate{
		Plug: interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil),
		Slot: interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-dynamic"], map[string]interface{}{
			"c": "C",
		}),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}
