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
	"strings"
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

	randomSnap *snap.Info
	randomDecl *asserts.SnapDeclaration
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
  same-plug-publisher-id:
    allow-connection:
      slot-publisher-id:
        - $plug_publisher_id
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
        - $slot_publisher_id
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
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
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
   base-allow-snap-slot-not-allow:

   snap-slot-deny-snap-plug-allow:

   gadgethelp:
   trustedhelp:

   precise-plug-snap-id:
   precise-slot-snap-id:

   checked-plug-publisher-id:
   checked-slot-publisher-id:

   same-plug-publisher-id:
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
   base-allow-snap-slot-not-allow:

   snap-slot-deny-snap-plug-allow:

   trustedhelp:

   precise-plug-snap-id:
   precise-slot-snap-id:

   checked-plug-publisher-id:
   checked-slot-publisher-id:

   same-slot-publisher-id:
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
        - $plug_publisher_id
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
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	s.slotDecl = a.(*asserts.SnapDeclaration)

	s.randomSnap = snaptest.MockInfo(c, `
name: random-snap
plugs:
  precise-plug-snap-id:
  checked-plug-publisher-id:
  same-slot-publisher-id:
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
		{"base-plug-deny", `connection denied by plug rule of interface "base-plug-deny"`},
		{"base-plug-not-allow", `connection not allowed by plug rule of interface "base-plug-not-allow"`},
		{"base-slot-allow", ""},
		{"base-slot-deny", `connection denied by slot rule of interface "base-slot-deny"`},
		{"base-slot-not-allow", `connection not allowed by slot rule of interface "base-slot-not-allow"`},
		{"base-plug-not-allow-slots", `connection not allowed.*`},
		{"base-slot-not-allow-slots", `connection not allowed.*`},
		{"base-plug-not-allow-plugs", `connection not allowed.*`},
		{"base-slot-not-allow-plugs", `connection not allowed.*`},
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
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

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
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")
}

func (s *policySuite) TestPlugSnapIDCheckConnection(c *C) {
	// no plug-side declaration
	cand := policy.ConnectCandidate{
		Plug:                s.randomSnap.Plugs["precise-plug-snap-id"],
		Slot:                s.slotSnap.Slots["precise-plug-snap-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong snap-id
	cand = policy.ConnectCandidate{
		Plug:                s.randomSnap.Plugs["precise-plug-snap-id"],
		PlugSnapDeclaration: s.randomDecl,
		Slot:                s.slotSnap.Slots["precise-plug-snap-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right snap-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["precise-plug-snap-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.slotSnap.Slots["precise-plug-snap-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotSnapIDCheckConnection(c *C) {
	// no slot-side declaration
	cand := policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["precise-slot-snap-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.randomSnap.Slots["precise-slot-snap-id"],
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong snap-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["precise-slot-snap-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.randomSnap.Slots["precise-slot-snap-id"],
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right snap-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["precise-slot-snap-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.slotSnap.Slots["precise-slot-snap-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugPublisherIDCheckConnection(c *C) {
	// no plug-side declaration
	cand := policy.ConnectCandidate{
		Plug:                s.randomSnap.Plugs["checked-plug-publisher-id"],
		Slot:                s.slotSnap.Slots["checked-plug-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                s.randomSnap.Plugs["checked-plug-publisher-id"],
		PlugSnapDeclaration: s.randomDecl,
		Slot:                s.slotSnap.Slots["checked-plug-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right publisher-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["checked-plug-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.slotSnap.Slots["checked-plug-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotPublisherIDCheckConnection(c *C) {
	// no slot-side declaration
	cand := policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["checked-slot-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.randomSnap.Slots["checked-slot-publisher-id"],
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["checked-slot-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.randomSnap.Slots["checked-slot-publisher-id"],
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right publisher-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["checked-slot-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.slotSnap.Slots["checked-slot-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarPlugPublisherIDCheckConnection(c *C) {
	// no known publishers
	cand := policy.ConnectCandidate{
		Plug:            s.plugSnap.Plugs["same-plug-publisher-id"],
		Slot:            s.randomSnap.Slots["same-plug-publisher-id"],
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// no slot-side declaration
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["same-plug-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.randomSnap.Slots["same-plug-publisher-id"],
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                s.plugSnap.Plugs["same-plug-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                s.randomSnap.Slots["same-plug-publisher-id"],
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot publisher id == plug publisher id
	samePubSlotSnap := snaptest.MockInfo(c, `
name: same-pub-slot-snap
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
		Plug:                s.plugSnap.Plugs["same-plug-publisher-id"],
		PlugSnapDeclaration: s.plugDecl,
		Slot:                samePubSlotSnap.Slots["same-plug-publisher-id"],
		SlotSnapDeclaration: samePubSlotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarSlotPublisherIDCheckConnection(c *C) {
	// no known publishers
	cand := policy.ConnectCandidate{
		Plug:            s.randomSnap.Plugs["same-slot-publisher-id"],
		Slot:            s.slotSnap.Slots["same-slot-publisher-id"],
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// no plug-side declaration
	cand = policy.ConnectCandidate{
		Plug:                s.randomSnap.Plugs["same-slot-publisher-id"],
		Slot:                s.slotSnap.Slots["same-slot-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                s.randomSnap.Plugs["same-slot-publisher-id"],
		PlugSnapDeclaration: s.randomDecl,
		Slot:                s.slotSnap.Slots["same-slot-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug publisher id == slot publisher id
	samePubPlugSnap := snaptest.MockInfo(c, `
name: same-pub-plug-snap
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
		Plug:                samePubPlugSnap.Plugs["same-slot-publisher-id"],
		PlugSnapDeclaration: samePubPlugDecl,
		Slot:                s.slotSnap.Slots["same-slot-publisher-id"],
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestBaselineDefaultIsAllowInstallation(c *C) {
	installSnap := snaptest.MockInfo(c, `
name: install-slot-snap
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
slots:
  innocuous:
  install-slot-coreonly:
`, `installation not allowed over slot "install-slot-coreonly" by rule of interface "install-slot-coreonly"`},
		{`name: install-snap
slots:
  install-slot-attr-ok:
    attr: ok
`, ""},
		{`name: install-snap
slots:
  install-slot-attr-deny:
    trust: trusted
`, `installation denied over slot "install-slot-attr-deny" by rule of interface "install-slot-attr-deny"`},
		{`name: install-snap
plugs:
  install-plug-attr-ok:
    attr: ok
`, ""},
		{`name: install-snap
plugs:
  install-plug-attr-ok:
    attr: not-ok
`, `installation not allowed over plug "install-plug-attr-ok" by rule of interface "install-plug-attr-ok"`},
		{`name: install-snap
plugs:
  install-plug-gadget-only:
`, `installation not allowed over plug "install-plug-gadget-only" by rule of interface "install-plug-gadget-only"`},
		{`name: install-gadget
type: gadget
plugs:
  install-plug-gadget-only:
`, ""},
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
slots:
  install-slot-base-allow-snap-deny:
    have: yes # bool
`, `slots:
  install-slot-base-allow-snap-deny:
    deny-installation:
      slot-attributes:
        have: true
`, `installation denied over slot "install-slot-base-allow-snap-deny" by rule of interface "install-slot-base-allow-snap-deny" for "install-snap" snap`},
		{`name: install-snap
slots:
  install-slot-base-allow-snap-not-allow:
    have: yes # bool
`, `slots:
  install-slot-base-allow-snap-not-allow:
    allow-installation:
      slot-attributes:
        have: false
`, `installation not allowed over slot "install-slot-base-allow-snap-not-allow" by rule of interface "install-slot-base-allow-snap-not-allow" for "install-snap" snap`},
		{`name: install-snap
slots:
  install-slot-base-deny-snap-allow:
    have: yes
`, `slots:
  install-slot-base-deny-snap-allow:
    allow-installation: true
`, ""},
		{`name: install-snap
plugs:
  install-plug-base-allow-snap-deny:
    attr: give-me
`, `plugs:
  install-plug-base-allow-snap-deny:
    deny-installation:
      plug-attributes:
        attr: .*
`, `installation denied over plug "install-plug-base-allow-snap-deny" by rule of interface "install-plug-base-allow-snap-deny" for "install-snap" snap`},
		{`name: install-snap
plugs:
  install-plug-base-allow-snap-not-allow:
    attr: give-me
`, `plugs:
  install-plug-base-allow-snap-not-allow:
    allow-installation:
      plug-attributes:
        attr: minimal
`, `installation not allowed over plug "install-plug-base-allow-snap-not-allow" by rule of interface "install-plug-base-allow-snap-not-allow" for "install-snap" snap`},
		{`name: install-snap
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
