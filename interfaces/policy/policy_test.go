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

package policy_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces/policy"
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
}

var _ = Suite(&policySuite{})

func (s *policySuite) SetUpSuite(c *C) {
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
  base-allow-snap-slot-deny: true
  gadgethelp:
    allow-connection:
      plug-snap-type:
        - gadget
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

$builtin`))
	c.Assert(err, IsNil)
	s.baseDecl = a.(*asserts.BaseDeclaration)

	s.plugSnap = snaptest.MockInfo(c, `
name: plug-snap
plugs:
   random:

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

   snap-plug-allow:
   snap-plug-not-allow:
   snap-plug-deny:

   snap-slot-allow:
   snap-slot-not-allow:
   snap-slot-deny:

   base-deny-snap-slot-allow:
   base-deny-snap-plug-allow:
   base-allow-snap-slot-deny:

   snap-slot-deny-snap-plug-allow:

   gadgethelp:
   trustedhelp:
`, nil)

	s.slotSnap = snaptest.MockInfo(c, `
name: slot-snap
slots:
   random:

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

   snap-plug-allow:
   snap-plug-not-allow:
   snap-plug-deny:

   snap-slot-allow:
   snap-slot-not-allow:
   snap-slot-deny:

   base-deny-snap-slot-allow:
   base-deny-snap-plug-allow:
   base-allow-snap-slot-deny:

   snap-slot-deny-snap-plug-allow:

   trustedhelp:
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
  base-allow-snap-slot-deny:
    allow-connection: false
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	s.slotDecl = a.(*asserts.SnapDeclaration)
}

func (s *policySuite) TestBaselineDefaultIsAllow(c *C) {
	cand := policy.ConnectCandidate{
		Plug:            s.plugSnap.Plugs["random"],
		Slot:            s.slotSnap.Slots["random"],
		BaseDeclaration: s.baseDecl,
	}

	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestBaseDeclAllowDenyConnection(c *C) {
	tests := []struct {
		iface    string
		expected string // "" => no error
	}{
		{"base-plug-allow", ""},
		{"base-plug-deny", `connection denied because it matches deny-connection in plug rule for interface "base-plug-deny" from base-declaration`},
		{"base-plug-not-allow", `connection denied because it does not match allow-connection in plug rule for interface "base-plug-not-allow" from base-declaration`},
		{"base-slot-allow", ""},
		{"base-slot-deny", `connection denied because it matches deny-connection in slot rule for interface "base-slot-deny" from base-declaration.*`},
		{"base-slot-not-allow", `connection denied because it does not match allow-connection in slot rule for interface "base-slot-not-allow" from base-declaration.*`},
		{"base-plug-not-allow-slots", `connection denied.*`},
		{"base-slot-not-allow-slots", `connection denied.*`},
		{"base-plug-not-allow-plugs", `connection denied.*`},
		{"base-slot-not-allow-plugs", `connection denied.*`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:            s.plugSnap.Plugs[t.iface],
			Slot:            s.slotSnap.Slots[t.iface],
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

func (s *policySuite) TestSnapDeclAllowDenyConnection(c *C) {
	tests := []struct {
		iface    string
		expected string // "" => no error
	}{
		{"random", ""},
		{"snap-plug-allow", ""},
		{"snap-plug-deny", `connection denied because it matches deny-connection in plug rule for interface "snap-plug-deny" from snap-declaration for snap "plug-snap" \(id plugsnapidid.*\)`},
		{"snap-plug-not-allow", `connection denied because it does not match allow-connection in plug rule for interface "snap-plug-not-allow" from snap-declaration for snap "plug-snap" \(id plugsnapidid.*\)`},
		{"snap-slot-allow", ""},
		{"snap-slot-deny", `connection denied because it matches deny-connection in slot rule for interface "snap-slot-deny" from snap-declaration for snap "slot-snap" \(id slotsnapid.*\)`},
		{"snap-slot-not-allow", `connection denied because it does not match allow-connection in slot rule for interface "snap-slot-not-allow" from snap-declaration for snap "slot-snap" \(id slotsnap.*\)`},
		{"base-deny-snap-slot-allow", ""},
		{"base-deny-snap-plug-allow", ""},
		{"snap-slot-deny-snap-plug-allow", ""},
		{"base-allow-snap-slot-deny", `connection denied.*`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                s.plugSnap.Plugs[t.iface],
			Slot:                s.slotSnap.Slots[t.iface],
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

func (s *policySuite) TestSnapTypeCheckConnection(c *C) {
	gadgetSnap := snaptest.MockInfo(c, `
name: gadget
type: gadget
plugs:
   gadgethelp:
slots:
   trustedhelp:
`, nil)

	coreSnap := snaptest.MockInfo(c, `
name: core
type: os
slots:
   gadgethelp:
   trustedhelp:
`, nil)

	cand := policy.ConnectCandidate{
		Plug:            gadgetSnap.Plugs["gadgethelp"],
		Slot:            coreSnap.Slots["gadgethelp"],
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)

	cand = policy.ConnectCandidate{
		Plug:            s.plugSnap.Plugs["gadgethelp"],
		Slot:            coreSnap.Slots["gadgethelp"],
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection denied.*")

	for _, trustedSide := range []*snap.Info{coreSnap, gadgetSnap} {
		cand = policy.ConnectCandidate{
			Plug:                s.plugSnap.Plugs["trustedhelp"],
			PlugSnapDeclaration: s.plugDecl,
			Slot:                trustedSide.Slots["trustedhelp"],
			BaseDeclaration:     s.baseDecl,
		}
		c.Check(cand.Check(), IsNil)
	}

	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["trustedhelp"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.slotSnap.Slots["trustedhelp"],
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection denied.*")

}
