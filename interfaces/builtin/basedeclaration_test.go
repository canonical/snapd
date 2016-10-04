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

package builtin_test

import (
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/snap/snaptest"
)

type baseDeclSuite struct {
	baseDecl *asserts.BaseDeclaration
}

var _ = Suite(&baseDeclSuite{})

func (s *baseDeclSuite) SetUpSuite(c *C) {
	s.baseDecl = asserts.BuiltinBaseDeclaration()
}

func (s *baseDeclSuite) connectCand(c *C, iface, slotYaml, plugYaml string) *policy.ConnectCandidate {
	if slotYaml == "" {
		slotYaml = fmt.Sprintf(`name: slot-snap
slots:
  %s:
`, iface)
	}
	if plugYaml == "" {
		plugYaml = fmt.Sprintf(`name: plug-snap
plugs:
  %s:
`, iface)
	}
	slotSnap := snaptest.MockInfo(c, slotYaml, nil)
	plugSnap := snaptest.MockInfo(c, plugYaml, nil)
	return &policy.ConnectCandidate{
		Plug:            plugSnap.Plugs[iface],
		Slot:            slotSnap.Slots[iface],
		BaseDeclaration: s.baseDecl,
	}
}

const declTempl = `type: snap-declaration
authority-id: canonical
series: 16
snap-name: @name@
snap-id: @snapid@
publisher-id: @publisher@
@plugsSlots@
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`

func (s *baseDeclSuite) mockSnapDecl(c *C, name, snapID, publisher string, plugsSlots string) *asserts.SnapDeclaration {
	encoded := strings.Replace(declTempl, "@name@", name, 1)
	encoded = strings.Replace(encoded, "@snapid@", snapID, 1)
	encoded = strings.Replace(encoded, "@publisher@", publisher, 1)
	if plugsSlots != "" {
		encoded = strings.Replace(encoded, "@plugsSlots@", strings.TrimSpace(plugsSlots), 1)
	} else {
		encoded = strings.Replace(encoded, "@plugsSlots@\n", "", 1)
	}
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	return a.(*asserts.SnapDeclaration)
}

func (s *baseDeclSuite) TestAutoConnection(c *C) {
	all := builtin.Interfaces()

	// these have more complex or in flux policies and have their
	// own separate tests
	snowflakes := map[string]bool{
		"content":       true,
		"home":          true,
		"lxd-support":   true,
		"snapd-control": true,
	}

	for _, iface := range all {
		if snowflakes[iface.Name()] {
			continue
		}
		expected := iface.AutoConnect()
		cand := s.connectCand(c, iface.Name(), "", "")
		err := cand.CheckAutoConnect()
		if expected {
			c.Check(err, IsNil, Commentf(iface.Name()))
		} else {
			c.Check(err, NotNil, Commentf(iface.Name()))
		}
	}
}

func (s *baseDeclSuite) TestAutoConnectPair(c *C) {
	all := builtin.Interfaces()

	// these have more complex or in flux policies and have their
	// own separate tests
	snowflakes := map[string]bool{
		"content":     true,
		"home":        true,
		"lxd-support": true,
	}

	for _, iface := range all {
		if snowflakes[iface.Name()] {
			continue
		}
		c.Check(iface.AutoConnectPair(nil, nil), Equals, true)
	}
}

func (s *baseDeclSuite) TestInterimAutoConnectHome(c *C) {
	// home will be controlled by AutoConnectPair(plug, slot) until
	// we have on-classic support in decls
	// to stop it from working on non-classic
	cand := s.connectCand(c, "home", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestInterimAutoConnectSnapdControl(c *C) {
	// snapd-control is auto-connect until we have snap declaration editing
	cand := s.connectCand(c, "snapd-control", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectContent(c *C) {
	// content will also depend for now AutoConnect(plug, slot)
	// random snaps cannot connect with content
	cand := s.connectCand(c, "content", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectLxdSupport(c *C) {
	cand := s.connectCand(c, "lxd-support", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)

	// TODO: have the real snap-decl allow things and not the base-decl
	lxdDecl := s.mockSnapDecl(c, "lxd", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", "")
	cand.PlugSnapDeclaration = lxdDecl
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}
