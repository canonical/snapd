// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
  auto-plug-on-store1:
    allow-auto-connection: false
  auto-plug-on-my-brand:
    allow-auto-connection: false
  auto-plug-on-my-model2:
    allow-auto-connection: false
  auto-plug-on-multi:
    allow-auto-connection: false
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
  install-plug-device-scope:
    allow-installation: false
  install-plug-name-bound:
    allow-installation:
      plug-names:
        - $INTERFACE
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
  fromcore:
    allow-connection:
      slot-snap-type:
        - core
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
  auto-slot-on-store1:
    allow-auto-connection: false
  auto-slot-on-my-brand:
    allow-auto-connection: false
  auto-slot-on-my-model2:
    allow-auto-connection: false
  auto-slot-on-multi:
    allow-auto-connection: false
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
  install-slot-device-scope:
    allow-installation: false
  install-slot-name-bound:
    allow-installation:
      slot-names:
        - $INTERFACE
  slots-arity-default:
    allow-auto-connection: true
  slots-arity-slot-any:
    deny-auto-connection: true
  slots-arity-plug-any:
    deny-auto-connection: true
  slots-arity-slot-any-plug-one:
    deny-auto-connection: true
  slots-arity-slot-any-plug-two:
    deny-auto-connection: true
  slots-arity-slot-any-plug-default:
    deny-auto-connection: true
  slots-arity-slot-one-plug-any:
    deny-auto-connection: true
  slots-name-bound:
    deny-auto-connection: true
  plugs-name-bound:
    deny-auto-connection: true
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
   fromcore:

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

   auto-plug-on-store1:
   auto-plug-on-my-brand:
   auto-plug-on-my-model2:
   auto-plug-on-multi:

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

   auto-slot-on-store1:
   auto-slot-on-my-brand:
   auto-slot-on-my-model2:
   auto-slot-on-multi:

   slot-on-classic-true:
   slot-on-classic-distros:
   slot-on-classic-false:

   plug-on-classic-true:
   plug-on-classic-distros:
   plug-on-classic-false:

   slots-arity-default:
   slots-arity-slot-any:
   slots-arity-plug-any:
   slots-arity-slot-any-plug-one:
   slots-arity-slot-any-plug-two:
   slots-arity-slot-any-plug-default:
   slots-arity-slot-one-plug-any:

   slots-name-bound-p1:
     interface: slots-name-bound
   slots-name-bound-p2:
     interface: slots-name-bound
   plugs-name-bound-p1:
     interface: plugs-name-bound
   plugs-name-bound-p2:
     interface: plugs-name-bound
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

   auto-plug-on-store1:
   auto-plug-on-my-brand:
   auto-plug-on-my-model2:
   auto-plug-on-multi:

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

   auto-slot-on-store1:
   auto-slot-on-my-brand:
   auto-slot-on-my-model2:
   auto-slot-on-multi:

   slot-on-classic-true:
   slot-on-classic-distros:
   slot-on-classic-false:

   plug-on-classic-true:
   plug-on-classic-distros:
   plug-on-classic-false:

   slots-arity-default:
   slots-arity-slot-any:
   slots-arity-plug-any:
   slots-arity-slot-any-plug-one:
   slots-arity-slot-any-plug-two:
   slots-arity-slot-any-plug-default:
   slots-arity-slot-one-plug-any:

   slots-name-bound-s1:
     interface: slots-name-bound
   slots-name-bound-s2:
     interface: slots-name-bound
   plugs-name-bound-s1:
     interface: plugs-name-bound
   plugs-name-bound-s2:
     interface: plugs-name-bound

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
  auto-plug-on-store1:
    allow-auto-connection:
      on-store:
        - store1
  auto-plug-on-my-brand:
    allow-auto-connection:
      on-brand:
        - my-brand
        - my-brand-subbrand
  auto-plug-on-my-model2:
    allow-auto-connection:
      on-model:
        - my-brand-subbrand/my-model2
  auto-plug-on-multi:
    allow-auto-connection:
      on-brand:
        - my-brand
        - my-brand-subbrand
      on-store:
        - store1
        - other-store
      on-model:
        - my-brand/my-model1
        - my-brand-subbrand/my-model2
  slots-arity-plug-any:
    allow-auto-connection:
      slots-per-plug: *
  slots-arity-slot-any-plug-one:
    allow-auto-connection:
      slots-per-plug: 1
  slots-arity-slot-any-plug-two:
    allow-auto-connection:
      slots-per-plug: 2
  slots-arity-slot-any-plug-default:
    allow-auto-connection: true
  slots-arity-slot-one-plug-any:
    allow-auto-connection:
      slots-per-plug: *
  plugs-name-bound:
    allow-auto-connection:
      -
        plug-names:
          - plugs-name-bound-p1
        slot-names:
          - plugs-name-bound-s2
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
  auto-slot-on-store1:
    allow-auto-connection:
      on-store:
        - store1
  auto-slot-on-my-brand:
    allow-auto-connection:
      on-brand:
        - my-brand
        - my-brand-subbrand
  auto-slot-on-my-model2:
    allow-auto-connection:
      on-model:
        - my-brand-subbrand/my-model2
  auto-slot-on-multi:
    allow-auto-connection:
      on-brand:
        - my-brand
        - my-brand-subbrand
      on-store:
        - store1
        - other-store
      on-model:
        - my-brand/my-model1
        - my-brand-subbrand/my-model2
  slots-arity-slot-any:
    allow-auto-connection:
      slots-per-plug: *
  slots-arity-slot-any-plug-one:
    allow-auto-connection:
      slots-per-plug: *
  slots-arity-slot-any-plug-two:
    allow-auto-connection:
      slots-per-plug: *
  slots-arity-slot-any-plug-default:
    allow-auto-connection:
      slots-per-plug: *
  slots-arity-slot-one-plug-any:
    allow-auto-connection:
      slots-per-plug: 1
  slots-name-bound:
    allow-auto-connection:
      -
        plug-names:
          - slots-name-bound-p2
        slot-names:
          - slots-name-bound-s2
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
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["random"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["random"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}

	c.Check(cand.Check(), IsNil)
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)
}

func (s *policySuite) TestInterfaceMismatch(c *C) {
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["mismatchy"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["mismatchy"], nil, nil),
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
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
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
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			BaseDeclaration: s.baseDecl,
		}

		arity, err := cand.CheckAutoConnect()
		if t.expected == "" {
			c.Check(err, IsNil)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
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
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
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
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,
			BaseDeclaration:     s.baseDecl,
		}

		arity, err := cand.CheckAutoConnect()
		if t.expected == "" {
			c.Check(err, IsNil)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
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
   fromcore:
`, nil)

	coreSnap := snaptest.MockInfo(c, `
name: core
version: 0
type: os
slots:
   gadgethelp:
   trustedhelp:
   fromcore:
`, nil)

	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(gadgetSnap.Plugs["gadgethelp"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(coreSnap.Slots["gadgethelp"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)

	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["gadgethelp"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(coreSnap.Slots["gadgethelp"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	for _, trustedSide := range []*snap.Info{coreSnap, gadgetSnap} {
		cand = policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["trustedhelp"], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			Slot:                interfaces.NewConnectedSlot(trustedSide.Slots["trustedhelp"], nil, nil),
			BaseDeclaration:     s.baseDecl,
		}
		c.Check(cand.Check(), IsNil)
	}

	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["trustedhelp"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["trustedhelp"], nil, nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["fromcore"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(coreSnap.Slots["fromcore"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)

	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["fromcore"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(gadgetSnap.Slots["fromcore"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")
}

func (s *policySuite) TestPlugSnapIDCheckConnection(c *C) {
	// no plug-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["precise-plug-snap-id"], nil, nil),
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-plug-snap-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["precise-plug-snap-id"], nil, nil),
		PlugSnapDeclaration: s.randomDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-plug-snap-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-plug-snap-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-plug-snap-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotSnapIDCheckConnection(c *C) {
	// no slot-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-slot-snap-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["precise-slot-snap-id"], nil, nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-slot-snap-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["precise-slot-snap-id"], nil, nil),
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right snap-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["precise-slot-snap-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["precise-slot-snap-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugPublisherIDCheckConnection(c *C) {
	// no plug-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["checked-plug-publisher-id"], nil, nil),
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-plug-publisher-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["checked-plug-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.randomDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-plug-publisher-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-plug-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-plug-publisher-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotPublisherIDCheckConnection(c *C) {
	// no slot-side declaration
	cand := policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-slot-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["checked-slot-publisher-id"], nil, nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-slot-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["checked-slot-publisher-id"], nil, nil),
		SlotSnapDeclaration: s.randomDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// right publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["checked-slot-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["checked-slot-publisher-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarPlugPublisherIDCheckConnection(c *C) {
	// no known publishers
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.randomSnap.Slots["same-plug-publisher-id"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// no slot-side declaration
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["same-plug-publisher-id"], nil, nil),
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// slot-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(s.randomSnap.Slots["same-plug-publisher-id"], nil, nil),
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
		Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs["same-plug-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.plugDecl,
		Slot:                interfaces.NewConnectedSlot(samePubSlotSnap.Slots["same-plug-publisher-id"], nil, nil),
		SlotSnapDeclaration: samePubSlotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarSlotPublisherIDCheckConnection(c *C) {
	// no known publishers
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.randomSnap.Plugs["same-slot-publisher-id"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// no plug-side declaration
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["same-slot-publisher-id"], nil, nil),
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil, nil),
		SlotSnapDeclaration: s.slotDecl,
		BaseDeclaration:     s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug-side declaration, wrong publisher-id
	cand = policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(s.randomSnap.Plugs["same-slot-publisher-id"], nil, nil),
		PlugSnapDeclaration: s.randomDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil, nil),
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
		Plug:                interfaces.NewConnectedPlug(samePubPlugSnap.Plugs["same-slot-publisher-id"], nil, nil),
		PlugSnapDeclaration: samePubPlugDecl,
		Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots["same-slot-publisher-id"], nil, nil),
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

func (s *policySuite) TestBaseDeclAllowDenyInstallationMinimalCheck(c *C) {
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
		{`name: install-gadget
version: 0
type: gadget
slots:
  install-slot-or:
`, ""}, // we ignore deny-installation rules for the purpose of the minimal check
		{`name: install-snap
version: 0
slots:
  install-slot-or:
`, ""},
		{`name: install-snap
version: 0
plugs:
  install-plug-gadget-only:
`, ``}, // plug is not validated with minimal installation check
	}

	for _, t := range tests {
		installSnap := snaptest.MockInfo(c, t.installYaml, nil)

		cand := policy.InstallCandidateMinimalCheck{
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

func (s *policySuite) TestOnClassicMinimalInstallationCheck(c *C) {
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
  install-plug-on-classic-distros:`, ""}, // plug is not validated with minimal installation check
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

		cand := policy.InstallCandidateMinimalCheck{
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
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
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
			Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
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

var (
	otherModel *asserts.Model
	myModel1   *asserts.Model
	myModel2   *asserts.Model
	myModel3   *asserts.Model

	substore1 *asserts.Store
)

func init() {
	a, err := asserts.Decode([]byte(`type: model
authority-id: other-brand
series: 16
brand-id: other-brand
model: other-model
classic: true
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	if err != nil {
		panic(err)
	}
	otherModel = a.(*asserts.Model)

	a, err = asserts.Decode([]byte(`type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: my-model1
store: store1
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	if err != nil {
		panic(err)
	}
	myModel1 = a.(*asserts.Model)

	a, err = asserts.Decode([]byte(`type: model
authority-id: my-brand-subbrand
series: 16
brand-id: my-brand-subbrand
model: my-model2
store: store2
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	if err != nil {
		panic(err)
	}
	myModel2 = a.(*asserts.Model)

	a, err = asserts.Decode([]byte(`type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: my-model3
store: substore1
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	if err != nil {
		panic(err)
	}
	myModel3 = a.(*asserts.Model)

	a, err = asserts.Decode([]byte(`type: store
store: substore1
authority-id: canonical
operator-id: canonical
friendly-stores:
  - a-store
  - store1
  - store2
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	if err != nil {
		panic(err)
	}
	substore1 = a.(*asserts.Store)
}

func (s *policySuite) TestPlugDeviceScopeCheckAutoConnection(c *C) {
	tests := []struct {
		model *asserts.Model
		iface string
		err   string // "" => no error
	}{
		{nil, "auto-plug-on-store1", `auto-connection not allowed by plug rule of interface "auto-plug-on-store1" for "plug-snap" snap`},
		{otherModel, "auto-plug-on-store1", `auto-connection not allowed by plug rule of interface "auto-plug-on-store1" for "plug-snap" snap`},
		{myModel1, "auto-plug-on-store1", ""},
		{myModel2, "auto-plug-on-store1", `auto-connection not allowed by plug rule of interface "auto-plug-on-store1" for "plug-snap" snap`},
		{otherModel, "auto-plug-on-my-brand", `auto-connection not allowed by plug rule of interface "auto-plug-on-my-brand" for "plug-snap" snap`},
		{myModel1, "auto-plug-on-my-brand", ""},
		{myModel2, "auto-plug-on-my-brand", ""},
		{otherModel, "auto-plug-on-my-model2", `auto-connection not allowed by plug rule of interface "auto-plug-on-my-model2" for "plug-snap" snap`},
		{myModel1, "auto-plug-on-my-model2", `auto-connection not allowed by plug rule of interface "auto-plug-on-my-model2" for "plug-snap" snap`},
		{myModel2, "auto-plug-on-my-model2", ""},
		// on-store/on-brand/on-model are ANDed for consistency!
		{otherModel, "auto-plug-on-multi", `auto-connection not allowed by plug rule of interface "auto-plug-on-multi" for "plug-snap" snap`},
		{myModel1, "auto-plug-on-multi", ""},
		{myModel2, "auto-plug-on-multi", `auto-connection not allowed by plug rule of interface "auto-plug-on-multi" for "plug-snap" snap`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,

			BaseDeclaration: s.baseDecl,

			Model: t.model,
		}
		arity, err := cand.CheckAutoConnect()
		if t.err == "" {
			c.Check(err, IsNil)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestPlugDeviceScopeFriendlyStoreCheckAutoConnection(c *C) {
	tests := []struct {
		model *asserts.Model
		store *asserts.Store
		iface string
		err   string // "" => no error
	}{
		{nil, nil, "auto-plug-on-store1", `auto-connection not allowed by plug rule of interface "auto-plug-on-store1" for "plug-snap" snap`},
		{myModel3, nil, "auto-plug-on-store1", `auto-connection not allowed by plug rule of interface "auto-plug-on-store1" for "plug-snap" snap`},
		{myModel3, substore1, "auto-plug-on-store1", ""},
		{myModel2, substore1, "auto-plug-on-store1", `auto-connection not allowed by plug rule of interface "auto-plug-on-store1" for "plug-snap" snap`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,

			BaseDeclaration: s.baseDecl,

			Model: t.model,
			Store: t.store,
		}
		arity, err := cand.CheckAutoConnect()
		if t.err == "" {
			c.Check(err, IsNil)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestSlotDeviceScopeCheckAutoConnection(c *C) {
	tests := []struct {
		model *asserts.Model
		iface string
		err   string // "" => no error
	}{
		{nil, "auto-slot-on-store1", `auto-connection not allowed by slot rule of interface "auto-slot-on-store1" for "slot-snap" snap`},
		{otherModel, "auto-slot-on-store1", `auto-connection not allowed by slot rule of interface "auto-slot-on-store1" for "slot-snap" snap`},
		{myModel1, "auto-slot-on-store1", ""},
		{myModel2, "auto-slot-on-store1", `auto-connection not allowed by slot rule of interface "auto-slot-on-store1" for "slot-snap" snap`},
		{otherModel, "auto-slot-on-my-brand", `auto-connection not allowed by slot rule of interface "auto-slot-on-my-brand" for "slot-snap" snap`},
		{myModel1, "auto-slot-on-my-brand", ""},
		{myModel2, "auto-slot-on-my-brand", ""},
		{otherModel, "auto-slot-on-my-model2", `auto-connection not allowed by slot rule of interface "auto-slot-on-my-model2" for "slot-snap" snap`},
		{myModel1, "auto-slot-on-my-model2", `auto-connection not allowed by slot rule of interface "auto-slot-on-my-model2" for "slot-snap" snap`},
		{myModel2, "auto-slot-on-my-model2", ""},
		// on-store/on-brand/on-model are ANDed for consistency!
		{otherModel, "auto-slot-on-multi", `auto-connection not allowed by slot rule of interface "auto-slot-on-multi" for "slot-snap" snap`},
		{myModel1, "auto-slot-on-multi", ""},
		{myModel2, "auto-slot-on-multi", `auto-connection not allowed by slot rule of interface "auto-slot-on-multi" for "slot-snap" snap`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,

			BaseDeclaration: s.baseDecl,

			Model: t.model,
		}
		arity, err := cand.CheckAutoConnect()
		if t.err == "" {
			c.Check(err, IsNil)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestSlotDeviceScopeFriendlyStoreCheckAutoConnection(c *C) {
	tests := []struct {
		model *asserts.Model
		store *asserts.Store
		iface string
		err   string // "" => no error
	}{
		{nil, nil, "auto-slot-on-store1", `auto-connection not allowed by slot rule of interface "auto-slot-on-store1" for "slot-snap" snap`},
		{myModel3, nil, "auto-slot-on-store1", `auto-connection not allowed by slot rule of interface "auto-slot-on-store1" for "slot-snap" snap`},
		{myModel3, substore1, "auto-slot-on-store1", ""},
		{myModel2, substore1, "auto-slot-on-store1", `auto-connection not allowed by slot rule of interface "auto-slot-on-store1" for "slot-snap" snap`},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,

			BaseDeclaration: s.baseDecl,

			Model: t.model,
			Store: t.store,
		}
		arity, err := cand.CheckAutoConnect()
		if t.err == "" {
			c.Check(err, IsNil)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestDeviceScopeInstallation(c *C) {
	const plugSnap = `name: install-snap
version: 0
plugs:
  install-plug-device-scope:`

	const slotSnap = `name: install-snap
version: 0
slots:
  install-slot-device-scope:`

	const plugOnStore1 = `plugs:
  install-plug-device-scope:
    allow-installation:
      on-store:
        - store1
`
	const plugOnMulti = `plugs:
  install-plug-device-scope:
    allow-installation:
      on-brand:
        - my-brand
        - my-brand-subbrand
      on-store:
        - store1
        - other-store
      on-model:
        - my-brand/my-model1
        - my-brand-subbrand/my-model2
`
	const slotOnStore2 = `slots:
  install-slot-device-scope:
    allow-installation:
      on-store:
        - store2
`

	tests := []struct {
		model       *asserts.Model
		store       *asserts.Store
		installYaml string
		plugsSlots  string
		err         string // "" => no error
	}{
		{nil, nil, plugSnap, plugOnStore1, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{otherModel, nil, plugSnap, plugOnStore1, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{myModel1, nil, plugSnap, plugOnStore1, ""},
		{myModel2, nil, plugSnap, plugOnStore1, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{otherModel, nil, plugSnap, plugOnMulti, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{myModel1, nil, plugSnap, plugOnMulti, ""},
		{myModel2, nil, plugSnap, plugOnMulti, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{otherModel, nil, slotSnap, slotOnStore2, `installation not allowed by "install-slot-device-scope" slot rule of interface "install-slot-device-scope" for "install-snap" snap`},
		{myModel1, nil, slotSnap, slotOnStore2, `installation not allowed by "install-slot-device-scope" slot rule of interface "install-slot-device-scope" for "install-snap" snap`},
		{myModel2, nil, slotSnap, slotOnStore2, ""},
		// friendly-stores
		{myModel3, nil, plugSnap, plugOnStore1, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{myModel3, substore1, plugSnap, plugOnStore1, ""},
		{myModel2, substore1, plugSnap, plugOnStore1, `installation not allowed by "install-plug-device-scope" plug rule of interface "install-plug-device-scope" for "install-snap" snap`},
		{myModel3, nil, slotSnap, slotOnStore2, `installation not allowed by "install-slot-device-scope" slot rule of interface "install-slot-device-scope" for \"install-snap\" snap`},
		{myModel3, substore1, slotSnap, slotOnStore2, ""},
		{myModel2, substore1, slotSnap, slotOnStore2, `installation not allowed by "install-slot-device-scope" slot rule of interface "install-slot-device-scope" for \"install-snap\" snap`},
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
			Model:           t.model,
			Store:           t.store,
		}
		err = cand.Check()
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
		Plug:            interfaces.NewConnectedPlug(s.randomSnap.Plugs["slot-slot-attr"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-slot-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// different attr values
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-slot-attr-mismatch"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-slot-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-slot-attr-match"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-slot-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotDollarPlugAttrConnection(c *C) {
	// different attr values
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-mismatch"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-match"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugDollarPlugAttrConnection(c *C) {
	// different attr values
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-mismatch"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-match"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugDollarSlotAttrConnection(c *C) {
	// different attr values
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-slot-attr"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-slot-attr-mismatch"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-slot-attr"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-slot-attr-match"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestDollarMissingConnection(c *C) {
	// not missing
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-missing-mismatch"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-missing"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// missing
	cand = policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-missing-match"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-missing"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotDollarPlugDynamicAttrConnection(c *C) {
	// "c" attribute of the plug missing
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-dynamic"], nil, map[string]interface{}{}),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr, "c" attribute of the plug provided by dynamic attribute
	cand = policy.ConnectCandidate{
		Plug: interfaces.NewConnectedPlug(s.plugSnap.Plugs["slot-plug-attr-dynamic"], nil, map[string]interface{}{
			"c": "C",
		}),

		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["slot-plug-attr"], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestPlugDollarSlotDynamicAttrConnection(c *C) {
	// "c" attribute of the slot missing
	cand := policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil, nil),
		Slot:            interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-dynamic"], nil, map[string]interface{}{}),
		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), ErrorMatches, "connection not allowed.*")

	// plug attr == slot attr, "c" attribute of the slot provided by dynamic attribute
	cand = policy.ConnectCandidate{
		Plug: interfaces.NewConnectedPlug(s.plugSnap.Plugs["plug-plug-attr"], nil, nil),
		Slot: interfaces.NewConnectedSlot(s.slotSnap.Slots["plug-plug-attr-dynamic"], nil, map[string]interface{}{
			"c": "C",
		}),

		BaseDeclaration: s.baseDecl,
	}
	c.Check(cand.Check(), IsNil)
}

func (s *policySuite) TestSlotsArityAutoConnection(c *C) {
	tests := []struct {
		iface string
		any   bool
	}{
		{"slots-arity-default", false},
		{"slots-arity-slot-any", true},
		{"slots-arity-plug-any", true},
		{"slots-arity-slot-any-plug-one", false},
		{"slots-arity-slot-any-plug-two", false},
		{"slots-arity-slot-any-plug-default", false},
		{"slots-arity-slot-one-plug-any", true},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.iface], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.iface], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,

			BaseDeclaration: s.baseDecl,
		}
		arity, err := cand.CheckAutoConnect()
		c.Assert(err, IsNil)
		c.Check(arity.SlotsPerPlugAny(), Equals, t.any)
	}
}

func (s *policySuite) TestNameConstraintsInstallation(c *C) {
	const plugSnap = `name: install-snap
version: 0
plugs:
  install-plug-name-bound:`

	const plugOtherNameSnap = `name: install-snap
version: 0
plugs:
  install-plug-name-bound-other:
    interface: install-plug-name-bound
`

	const slotSnap = `name: install-snap
version: 0
slots:
  install-slot-name-bound:`

	const slotOtherNameSnap = `name: install-snap
version: 0
slots:
  install-slot-name-bound-other:
    interface: install-slot-name-bound
`

	const plugOtherName = `plugs:
  install-plug-name-bound:
    allow-installation:
      plug-names:
        - install-plug-name-bound-other`

	tests := []struct {
		installYaml string
		plugsSlots  string
		err         string // "" => no error
	}{
		{plugSnap, "", ""},
		{plugOtherNameSnap, "", `installation not allowed by "install-plug-name-bound-other" plug rule of interface "install-plug-name-bound"`},
		{plugOtherNameSnap, plugOtherName, ""},
		{slotSnap, "", ""},
		{slotOtherNameSnap, "", `installation not allowed by "install-slot-name-bound-other" slot rule of interface "install-slot-name-bound"`},
	}

	for _, t := range tests {
		installSnap := snaptest.MockInfo(c, t.installYaml, nil)

		plugsSlots := strings.TrimSpace(t.plugsSlots)
		if plugsSlots != "" {
			plugsSlots = "\n" + plugsSlots
		}

		a, err := asserts.Decode([]byte(strings.Replace(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: install-snap
snap-id: installsnap6idididididididididid
publisher-id: publisher
@plugsSlots@
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`, "\n@plugsSlots@", plugsSlots, 1)))
		c.Assert(err, IsNil)
		snapDecl := a.(*asserts.SnapDeclaration)

		cand := policy.InstallCandidate{
			Snap:            installSnap,
			SnapDeclaration: snapDecl,
			BaseDeclaration: s.baseDecl,
		}
		err = cand.Check()
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *policySuite) TestNameConstraintsAutoConnection(c *C) {
	tests := []struct {
		plug, slot string
		ok         bool
	}{
		{"plugs-name-bound-p1", "plugs-name-bound-s1", false},
		{"plugs-name-bound-p2", "plugs-name-bound-s1", false},
		{"plugs-name-bound-p1", "plugs-name-bound-s2", true},
		{"plugs-name-bound-p2", "plugs-name-bound-s2", false},
		{"slots-name-bound-p1", "slots-name-bound-s1", false},
		{"slots-name-bound-p2", "slots-name-bound-s1", false},
		{"slots-name-bound-p1", "slots-name-bound-s2", false},
		{"slots-name-bound-p2", "slots-name-bound-s2", true},
	}

	for _, t := range tests {
		cand := policy.ConnectCandidate{
			Plug:                interfaces.NewConnectedPlug(s.plugSnap.Plugs[t.plug], nil, nil),
			Slot:                interfaces.NewConnectedSlot(s.slotSnap.Slots[t.slot], nil, nil),
			PlugSnapDeclaration: s.plugDecl,
			SlotSnapDeclaration: s.slotDecl,

			BaseDeclaration: s.baseDecl,
		}
		_, err := cand.CheckAutoConnect()
		if t.ok {
			c.Check(err, IsNil, Commentf("%s:%s", t.plug, t.slot))
		} else {
			var expected string
			if cand.Plug.Interface() == "plugs-name-bound" {
				expected = `auto-connection not allowed by plug rule of interface "plugs-name-bound".*`
			} else {
				// slots-name-bound
				expected = `auto-connection not allowed by slot rule of interface "slots-name-bound".*`
			}
			c.Check(err, ErrorMatches, expected)
		}
	}

}

// Test miscellaneous store patterns when base declaration has
// 'allow-installation: false' and we grant based on interface attributes
// such as with personal-files, system-files, etc.
//
// While this is also tested elsewhere, combining this into a single test
// makes it easy to verify correctness of a related set of patterns
//
// Eg, if base decl has:
//
//	slots:
//	  system-files:
//	    allow-installation:
//	      slot-snap-type:
//	        - core
//	plugs:
//	  system-files:
//	    allow-installation: false
//
// then test snap decls of the form:
//
//	plugs:
//	  system-files:
//	    allow-installation:
//	      plug-attributes:
//	        write: ...
//
// or:
//
//	plugs:
//	  system-files:
//	    allow-installation:
//	      -
//	        plug-attributes:
//	          write: ...
//
// or:
//
//	plugs:
//	  system-files:
//	    allow-installation:
//	      -
//	        plug-attributes:
//	          write: ...
//	      -
//	        plug-attributes:
//	          write: ...
func (s *policySuite) TestSnapDeclListAttribWithBaseAllowInstallationFalse(c *C) {
	baseDeclStr := `type: base-declaration
authority-id: canonical
series: 16
slots:
  base-allow-install-false:
    allow-installation:
      slot-snap-type:
        - core
plugs:
  base-allow-install-false:
    allow-installation: false
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`
	a, err := asserts.Decode([]byte(baseDeclStr))
	c.Assert(err, IsNil)
	baseDecl := a.(*asserts.BaseDeclaration)

	tests := []struct {
		installYaml        string
		snapDeclPlugsSlots string
		expected           string // "" => no error
	}{
		// expected match
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1a?
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1a?
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1a?
`, ``},

		// expected match single alternation
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1a?
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1a?
`, ``},

		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1a?
`, ``},
		// expected match two
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
  p2:
    interface: base-allow-install-false
    write:
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1a?
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
  p2:
    interface: base-allow-install-false
    write:
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1a?
`, ``},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
  p2:
    interface: base-allow-install-false
    write:
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1
      -
        plug-attributes:
          write: /path1a
`, ``},
		// expected no match
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1
`, `installation not allowed by "p1" plug rule of interface "base-allow-install-false" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
    - /path1a
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1
`, `installation not allowed by "p1" plug rule of interface "base-allow-install-false" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
  p2:
    interface: base-allow-install-false
    write:
    - /path1nomatch
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1a?
`, `installation not allowed by "p2" plug rule of interface "base-allow-install-false" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write:
    - /path1
  p2:
    interface: base-allow-install-false
    write:
    - /path1nomatch
`, `plugs:
  base-allow-install-false:
    allow-installation:
      -
        plug-attributes:
          write: /path1
      -
        plug-attributes:
          write: /path1a
`, `installation not allowed by "p2" plug rule of interface "base-allow-install-false" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        write: /path1
`, `installation not allowed by "p1" plug rule of interface "base-allow-install-false" for "install-snap" snap`},
		{`name: install-snap
version: 0
plugs:
  p1:
    interface: base-allow-install-false
    write: /path2
`, `plugs:
  base-allow-install-false:
    allow-installation:
      plug-attributes:
        read: /path1
        write: /path2
`, `installation not allowed by "p1" plug rule of interface "base-allow-install-false" for "install-snap" snap`},
	}

	for _, t := range tests {
		installSnap := snaptest.MockInfo(c, t.installYaml, nil)

		snapDeclStr := strings.Replace(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: install-snap
snap-id: installsnap6idididididididididid
publisher-id: publisher
@plugsSlots@
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`, "@plugsSlots@", strings.TrimSpace(t.snapDeclPlugsSlots), 1)
		b, err := asserts.Decode([]byte(snapDeclStr))
		c.Assert(err, IsNil)
		snapDecl := b.(*asserts.SnapDeclaration)

		cand := policy.InstallCandidate{
			Snap:            installSnap,
			SnapDeclaration: snapDecl,
			BaseDeclaration: baseDecl,
		}

		err = cand.Check()
		if t.expected == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.expected)
		}
	}
}

func (s *policySuite) TestSuperprivilegedVsAllowedSystemSlotInterfaceAllowInstallation(c *C) {
	baseDeclStr := `type: base-declaration
authority-id: canonical
series: 16
slots:
  superprivileged-vs-allowed-system-slot:
    allow-installation:
      slot-snap-type:
        - app
        - core
    deny-installation:
      slot-snap-type:
        - app
timestamp: 2022-03-20T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`
	a, err := asserts.Decode([]byte(baseDeclStr))
	c.Assert(err, IsNil)
	baseDecl := a.(*asserts.BaseDeclaration)

	appSnap := snaptest.MockInfo(c, `name: app-snap
version: 0
slots:
  myslot:
    interface: superprivileged-vs-allowed-system-slot
    special: special
`, nil)

	// ok with dangerous
	minCand := policy.InstallCandidateMinimalCheck{
		Snap:            appSnap,
		BaseDeclaration: baseDecl,
	}
	err = minCand.Check()
	c.Check(err, IsNil)

	// not ok without snap-declaration rule
	a, err = asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: app-snap
snap-id: appsnapid
publisher-id: publisher
timestamp: 2022-03-20T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	snapDecl := a.(*asserts.SnapDeclaration)

	cand := policy.InstallCandidate{
		Snap:            appSnap,
		SnapDeclaration: snapDecl,
		BaseDeclaration: baseDecl,
	}
	err = cand.Check()
	c.Check(err, NotNil)

	snapdSnap := snaptest.MockInfo(c, `name: snapd
version: 0
type: snapd
slots:
  superprivileged-vs-allowed-system-slot:
`, nil)
	a, err = asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: snapd
snap-id: PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4
publisher-id: canonical
timestamp: 2022-03-20T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	snapDecl = a.(*asserts.SnapDeclaration)
	c.Check(snapdSnap.Type(), Equals, snap.TypeSnapd)
	cand = policy.InstallCandidate{
		Snap:            snapdSnap,
		SnapDeclaration: snapDecl,
		BaseDeclaration: baseDecl,
	}
	err = cand.Check()
	c.Check(err, IsNil)
}

func (s *policySuite) TestSuperprivilegedVsAllowedSystemPlugInterfaceAllowInstallation(c *C) {
	// this is unlikely to be used in practice as system snap so far
	// have no plugs, but tested for symmetry/completeness
	baseDeclStr := `type: base-declaration
authority-id: canonical
series: 16
plugs:
  superprivileged-vs-allowed-system-plug:
    allow-installation:
      plug-snap-type:
        - app
        - core
    deny-installation:
      plug-snap-type:
        - app
timestamp: 2022-03-20T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`
	a, err := asserts.Decode([]byte(baseDeclStr))
	c.Assert(err, IsNil)
	baseDecl := a.(*asserts.BaseDeclaration)

	appSnap := snaptest.MockInfo(c, `name: app-snap
version: 0
plugs:
  myplug:
    interface: superprivileged-vs-allowed-system-plug
    special: special
`, nil)

	// ok with dangerous
	// NB: so far InstallCandidateMinimalCheck simply does not consider
	// plugs
	minCand := policy.InstallCandidateMinimalCheck{
		Snap:            appSnap,
		BaseDeclaration: baseDecl,
	}
	err = minCand.Check()
	c.Check(err, IsNil)

	// not ok without snap-declaration rule
	a, err = asserts.Decode([]byte(`type: snap-declaration
authority-id: canonical
series: 16
snap-name: app-snap
snap-id: appsnapid
publisher-id: publisher
timestamp: 2022-03-20T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`))
	c.Assert(err, IsNil)
	snapDecl := a.(*asserts.SnapDeclaration)

	cand := policy.InstallCandidate{
		Snap:            appSnap,
		SnapDeclaration: snapDecl,
		BaseDeclaration: baseDecl,
	}
	err = cand.Check()
	c.Check(err, NotNil)
}
