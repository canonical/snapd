// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type OnlineAccountsServiceInterfaceSuite struct {
	iface       interfaces.Interface
	classicSlot *interfaces.Slot
	coreSlot    *interfaces.Slot
	plug        *interfaces.Plug
}

var _ = Suite(&OnlineAccountsServiceInterfaceSuite{})

func (s *OnlineAccountsServiceInterfaceSuite) SetUpTest(c *C) {
	const mockClassicSlotSnapInfoYaml = `name: core
type: os
slots:
 online-accounts-service:
  interface: online-accounts-service
`

	const mockCoreSlotSnapInfoYaml = `name: service
version: 1.0
slots:
 online-accounts-service:
  interface: online-accounts-service
apps:
 oa:
  command: foo
  slots: [online-accounts-service]
`

	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [online-accounts-service]
`
	s.iface = &builtin.OnlineAccountsServiceInterface{}

	snapInfo := snaptest.MockInfo(c, mockCoreSlotSnapInfoYaml, nil)
	s.coreSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["online-accounts-service"]}

	snapInfo = snaptest.MockInfo(c, mockClassicSlotSnapInfoYaml, nil)
	s.classicSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["online-accounts-service"]}

	snapInfo = snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: snapInfo.Plugs["online-accounts-service"]}
}

func (s *OnlineAccountsServiceInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "online-accounts-service")
}

func (s *OnlineAccountsServiceInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Assert(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "online-accounts-service"`)
	c.Assert(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "online-accounts-service"`)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.Specification{}
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 1)

	// connected plugs don't have a security snippet for seccomp
	seccompSpec := seccomp.Specification{}
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(seccompSpec.SecurityTags(), HasLen, 0)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.Specification{}
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	// verify apparmor connected
	c.Check(snippet, testutil.Contains, "peer=(label=\"snap.service.oa\")")
}

func (s *OnlineAccountsServiceInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.Specification{}
	c.Assert(apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	snippet := apparmorSpec.SnippetForTag("snap.service.oa")
	c.Check(snippet, testutil.Contains, "peer=(label=\"snap.other.app\")")

	// no apparmor snippet for connected slot on classic
	apparmorSpec = apparmor.Specification{}
	c.Assert(apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(apparmorSpec.Snippets(), HasLen, 0)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	apparmorSpec := apparmor.Specification{}
	c.Assert(apparmorSpec.AddPermanentSlot(s.iface, s.coreSlot), IsNil)
	snippet := apparmorSpec.SnippetForTag("snap.service.oa")
	c.Check(string(snippet), testutil.Contains, "name=\"com.ubuntu.OnlineAccounts.Manager\"")

	// no apparmor snippet for permanent slot on classic
	apparmorSpec = apparmor.Specification{}
	c.Assert(apparmorSpec.AddPermanentSlot(s.iface, s.classicSlot), IsNil)
	c.Assert(apparmorSpec.Snippets(), HasLen, 0)
}

func (s *OnlineAccountsServiceInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	seccompSpec := seccomp.Specification{}
	c.Assert(seccompSpec.AddPermanentSlot(s.iface, s.coreSlot), IsNil)
	snippet := seccompSpec.SnippetForTag("snap.service.oa")
	c.Check(snippet, testutil.Contains, "listen\n")

	seccompSpec = seccomp.Specification{}
	c.Assert(seccompSpec.AddPermanentSlot(s.iface, s.classicSlot), IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)
}
