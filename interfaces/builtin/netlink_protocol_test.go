// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

type NetlinkProtocolInterfaceSuite struct {
	testutil.BaseTest

	iface                       interfaces.Interface
	gadgetNetlinkSlotInfo       *snap.SlotInfo
	gadgetNetlinkSlot           *interfaces.ConnectedSlot
	gadgetMissingNumberSlotInfo *snap.SlotInfo
	gadgetMissingNumberSlot     *interfaces.ConnectedSlot
	gadgetBadNumberSlotInfo     *snap.SlotInfo
	gadgetBadNumberSlot         *interfaces.ConnectedSlot
	gadgetBadInterfaceSlotInfo  *snap.SlotInfo
	gadgetBadInterfaceSlot      *interfaces.ConnectedSlot
	appPlugProtocolInfo         *snap.PlugInfo
	appPlugProtocol             *interfaces.ConnectedPlug
	osNetlinkSlotInfo           *snap.SlotInfo
	osNetlinkSlot               *interfaces.ConnectedSlot
}

var _ = Suite(&NetlinkProtocolInterfaceSuite{
	iface: builtin.MustInterface("netlink-protocol"),
})

func (s *NetlinkProtocolInterfaceSuite) SetUpTest(c *C) {
	gadgetInfo := snaptest.MockInfo(c, `
name: my-device
version: 0
type: gadget
slots:
    my-protocol:
        interface: netlink-protocol
        protocol: 100
    missing-number:
        interface: netlink-protocol
    bad-number:
        interface: netlink-protocol
        protocol: forty-two
    bad-interface-slot: other-interface
`, nil)
	s.gadgetNetlinkSlotInfo = gadgetInfo.Slots["my-protocol"]
	s.gadgetNetlinkSlot = interfaces.NewConnectedSlot(s.gadgetNetlinkSlotInfo, nil, nil)
	s.gadgetMissingNumberSlotInfo = gadgetInfo.Slots["missing-number"]
	s.gadgetMissingNumberSlot = interfaces.NewConnectedSlot(s.gadgetMissingNumberSlotInfo, nil, nil)
	s.gadgetBadNumberSlotInfo = gadgetInfo.Slots["bad-number"]
	s.gadgetBadNumberSlot = interfaces.NewConnectedSlot(s.gadgetBadNumberSlotInfo, nil, nil)
	s.gadgetBadInterfaceSlotInfo = gadgetInfo.Slots["bad-interface-slot"]
	s.gadgetBadInterfaceSlot = interfaces.NewConnectedSlot(s.gadgetBadInterfaceSlotInfo, nil, nil)

	osInfo := snaptest.MockInfo(c, `
name: my-core
version: 0
type: os
slots:
    my-protocol:
        interface: netlink-protocol
        protocol: 777
`, nil)
	s.osNetlinkSlotInfo = osInfo.Slots["my-protocol"]
	s.osNetlinkSlot = interfaces.NewConnectedSlot(s.osNetlinkSlotInfo, nil, nil)

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
version: 0
plugs:
  plug-for-netlink-protocol:
    interface: netlink-protocol
apps:
  netlink-test:
    command: bin/foo.sh
    plugs: [netlink-protocol]
`, nil)
	s.appPlugProtocolInfo = consumingSnapInfo.Plugs["plug-for-netlink-protocol"]
	s.appPlugProtocol = interfaces.NewConnectedPlug(s.appPlugProtocolInfo, nil, nil)
}

func (s *NetlinkProtocolInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, "allows communication through the kernel custom netlink protocol")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "netlink-protocol")
}

func (s *NetlinkProtocolInterfaceSuite) TestAutoConnect(c *C) {
	c.Check(s.iface.AutoConnect(nil, nil), Equals, true)
}

func (s *NetlinkProtocolInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *NetlinkProtocolInterfaceSuite) TestSanitizeSlotGadgetSnap(c *C) {
	// netlink slot on gadget accepeted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetNetlinkSlotInfo), IsNil)

	// slots without number attribute are rejected
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetMissingNumberSlotInfo), ErrorMatches,
		"netlink-protocol slot must have a protocol number attribute")

	// slots with number attribute that isnt a number
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadNumberSlotInfo), ErrorMatches,
		"netlink-protocol slot protocol number attribute must be an int")
}

func (s *NetlinkProtocolInterfaceSuite) TestSanitizeSlotOsSnap(c *C) {
	// netlink slot on OS accepeted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.osNetlinkSlotInfo), IsNil)
}

func (s *NetlinkProtocolInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appPlugProtocolInfo), IsNil)
}

func (s *NetlinkProtocolInterfaceSuite) TestApparmorConnectedPlug(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.appPlugProtocol, s.gadgetNetlinkSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.netlink-test"})
	c.Assert(spec.SnippetForTag("snap.client-snap.netlink-test"), testutil.Contains, `network netlink raw,`)
}

func (s *NetlinkProtocolInterfaceSuite) TestSecCompConnectedPlug(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.appPlugProtocol, s.gadgetNetlinkSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.netlink-test"})
	c.Assert(spec.SnippetForTag("snap.client-snap.netlink-test"), testutil.Contains, `socket AF_NETLINK - 100`)
}
