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

type NetlinkDriverInterfaceSuite struct {
	testutil.BaseTest

	iface interfaces.Interface

	// slots from gadget
	gadgetNetlinkSlotInfo       *snap.SlotInfo
	gadgetNetlinkSlot           *interfaces.ConnectedSlot
	gadgetMissingNumberSlotInfo *snap.SlotInfo
	gadgetMissingNumberSlot     *interfaces.ConnectedSlot
	gadgetMissingNameSlotInfo   *snap.SlotInfo
	gadgetMissingNameSlot       *interfaces.ConnectedSlot
	gadgetBadNumberSlotInfo     *snap.SlotInfo
	gadgetBadNumberSlot         *interfaces.ConnectedSlot
	gadgetBadNameSlotInfo       *snap.SlotInfo
	gadgetBadNameSlot           *interfaces.ConnectedSlot
	gadgetBadNameStringSlotInfo *snap.SlotInfo
	gadgetBadNameStringSlot     *interfaces.ConnectedSlot

	// slot from core
	osNetlinkSlotInfo *snap.SlotInfo
	osNetlinkSlot     *interfaces.ConnectedSlot

	// plugs from app
	appToGadgetPlugDriverInfo            *snap.PlugInfo
	appToGadgetPlugDriver                *interfaces.ConnectedPlug
	appToCorePlugDriverInfo              *snap.PlugInfo
	appToCorePlugDriver                  *interfaces.ConnectedPlug
	appMissingFamilyNamePlugDriverInfo   *snap.PlugInfo
	appMissingFamilyNamePlugDriver       *interfaces.ConnectedPlug
	appBadFamilyNamePlugDriverInfo       *snap.PlugInfo
	appBadFamilyNamePlugDriver           *interfaces.ConnectedPlug
	appBadFamilyNameStringPlugDriverInfo *snap.PlugInfo
	appBadFamilyNameStringPlugDriver     *interfaces.ConnectedPlug
}

var _ = Suite(&NetlinkDriverInterfaceSuite{
	iface: builtin.MustInterface("netlink-driver"),
})

func (s *NetlinkDriverInterfaceSuite) SetUpTest(c *C) {
	gadgetInfo := snaptest.MockInfo(c, `
name: my-device
version: 0
type: gadget
slots:
    my-driver:
        interface: netlink-driver
        family: 100
        family-name: foo-driver
    missing-number:
        interface: netlink-driver
        family-name: missing-number
    missing-name:
        interface: netlink-driver
        family: 400
    bad-number:
        interface: netlink-driver
        family: one-hundred
        family-name: foo-driver
    bad-family-name:
        interface: netlink-driver
        family: 100
        family-name: foo---------
    bad-family-name-string:
        interface: netlink-driver
        family: 100
        family-name: 12123323443432
`, nil)
	s.gadgetNetlinkSlotInfo = gadgetInfo.Slots["my-driver"]
	s.gadgetNetlinkSlot = interfaces.NewConnectedSlot(s.gadgetNetlinkSlotInfo, nil, nil)
	s.gadgetMissingNumberSlotInfo = gadgetInfo.Slots["missing-number"]
	s.gadgetMissingNumberSlot = interfaces.NewConnectedSlot(s.gadgetMissingNumberSlotInfo, nil, nil)
	s.gadgetMissingNameSlotInfo = gadgetInfo.Slots["missing-name"]
	s.gadgetMissingNameSlot = interfaces.NewConnectedSlot(s.gadgetMissingNameSlotInfo, nil, nil)
	s.gadgetBadNumberSlotInfo = gadgetInfo.Slots["bad-number"]
	s.gadgetBadNumberSlot = interfaces.NewConnectedSlot(s.gadgetBadNumberSlotInfo, nil, nil)
	s.gadgetBadNameSlotInfo = gadgetInfo.Slots["bad-family-name"]
	s.gadgetBadNameSlot = interfaces.NewConnectedSlot(s.gadgetBadNameSlotInfo, nil, nil)
	s.gadgetBadNameStringSlotInfo = gadgetInfo.Slots["bad-family-name-string"]
	s.gadgetBadNameStringSlot = interfaces.NewConnectedSlot(s.gadgetBadNameStringSlotInfo, nil, nil)

	osInfo := snaptest.MockInfo(c, `
name: my-core
version: 0
type: os
slots:
    my-driver:
        interface: netlink-driver
        family: 777
        family-name: seven-7-seven
`, nil)
	s.osNetlinkSlotInfo = osInfo.Slots["my-driver"]
	s.osNetlinkSlot = interfaces.NewConnectedSlot(s.osNetlinkSlotInfo, nil, nil)

	// Snap Consumers
	consumingSnapInfo := snaptest.MockInfo(c, `
name: client-snap
version: 0
plugs:
  plug-for-netlink-driver-777:
    interface: netlink-driver
    family-name: seven-7-seven
  plug-for-netlink-driver-foo:
    interface: netlink-driver
    family-name: foo-driver
  missing-family-name:
    interface: netlink-driver
  invalid-family-name:
    interface: netlink-driver
    family-name: ---foo-----
  invalid-family-name-string:
    interface: netlink-driver
    family-name: 1213123
apps:
  netlink-test:
    command: bin/foo.sh
    plugs: 
      - plug-for-netlink-driver-777
      - plug-for-netlink-driver-foo
`, nil)
	s.appToCorePlugDriverInfo = consumingSnapInfo.Plugs["plug-for-netlink-driver-777"]
	s.appToCorePlugDriver = interfaces.NewConnectedPlug(s.appToCorePlugDriverInfo, nil, nil)

	s.appToGadgetPlugDriverInfo = consumingSnapInfo.Plugs["plug-for-netlink-driver-foo"]
	s.appToGadgetPlugDriver = interfaces.NewConnectedPlug(s.appToCorePlugDriverInfo, nil, nil)

	s.appBadFamilyNamePlugDriverInfo = consumingSnapInfo.Plugs["invalid-family-name"]
	s.appBadFamilyNamePlugDriver = interfaces.NewConnectedPlug(s.appBadFamilyNamePlugDriverInfo, nil, nil)

	s.appBadFamilyNameStringPlugDriverInfo = consumingSnapInfo.Plugs["invalid-family-name-string"]
	s.appBadFamilyNameStringPlugDriver = interfaces.NewConnectedPlug(s.appBadFamilyNameStringPlugDriverInfo, nil, nil)

	s.appMissingFamilyNamePlugDriverInfo = consumingSnapInfo.Plugs["missing-family-name"]
	s.appMissingFamilyNamePlugDriver = interfaces.NewConnectedPlug(s.appMissingFamilyNamePlugDriverInfo, nil, nil)
}

func (s *NetlinkDriverInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, false)
	c.Assert(si.Summary, Equals, "allows operating a kernel driver module exposing itself via a netlink protocol family")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "netlink-driver")
}

func (s *NetlinkDriverInterfaceSuite) TestAutoConnect(c *C) {
	// ensure the plug definitions in the YAML didn't change
	c.Check(s.appToCorePlugDriverInfo.Attrs["family-name"], Equals, "seven-7-seven")
	c.Check(s.osNetlinkSlotInfo.Attrs["family-name"], Equals, "seven-7-seven")

	// ensure the plug definitions in the YAML didn't change
	c.Check(s.appToGadgetPlugDriverInfo.Attrs["family-name"], Equals, "foo-driver")
	c.Check(s.gadgetNetlinkSlotInfo.Attrs["family-name"], Equals, "foo-driver")

	// with matching family-name attributes, it works
	c.Check(s.iface.AutoConnect(s.appToCorePlugDriverInfo, s.osNetlinkSlotInfo), Equals, true)
	c.Check(s.iface.AutoConnect(s.appToGadgetPlugDriverInfo, s.gadgetNetlinkSlotInfo), Equals, true)

	// with different family-name attributes, it doesn't
	c.Check(s.iface.AutoConnect(s.appToCorePlugDriverInfo, s.gadgetNetlinkSlotInfo), Equals, false)
	c.Check(s.iface.AutoConnect(s.appToGadgetPlugDriverInfo, s.osNetlinkSlotInfo), Equals, false)
}

func (s *NetlinkDriverInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *NetlinkDriverInterfaceSuite) TestSanitizeSlotGadgetSnap(c *C) {
	// netlink slot on gadget accepted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetNetlinkSlotInfo), IsNil)

	// slots without number attribute are rejected
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetMissingNumberSlotInfo), ErrorMatches,
		"netlink-driver slot must have a family number attribute")

	// slots with number attribute that isnt a number
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadNumberSlotInfo), ErrorMatches,
		"netlink-driver slot family number attribute must be an int")

	// slots without family-name
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetMissingNameSlotInfo), ErrorMatches,
		"netlink-driver slot must have a family-name attribute")

	// slots with family-name attribute that isn't a string
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadNameStringSlotInfo), ErrorMatches,
		`netlink-driver slot family-name attribute must be a string`)

	// slots with bad family-name
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.gadgetBadNameSlotInfo), ErrorMatches,
		`netlink-driver slot family-name "foo---------" is invalid`)
}

func (s *NetlinkDriverInterfaceSuite) TestSanitizePlugAppSnap(c *C) {
	// netlink plug on app accepted
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appToCorePlugDriverInfo), IsNil)

	// plugs without family-name are rejected
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appMissingFamilyNamePlugDriverInfo), ErrorMatches,
		"netlink-driver plug must have a family-name attribute")

	// slots with family-name attribute that isn't a string
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appBadFamilyNameStringPlugDriverInfo), ErrorMatches,
		`netlink-driver plug family-name attribute must be a string`)

	// slots with bad family-name
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appBadFamilyNamePlugDriverInfo), ErrorMatches,
		`netlink-driver plug family-name "---foo-----" is invalid`)
}

func (s *NetlinkDriverInterfaceSuite) TestSanitizeSlotOsSnap(c *C) {
	// netlink slot on OS accepted
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.osNetlinkSlotInfo), IsNil)
}

func (s *NetlinkDriverInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appToCorePlugDriverInfo), IsNil)
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.appToGadgetPlugDriverInfo), IsNil)
}

func (s *NetlinkDriverInterfaceSuite) TestApparmorConnectedPlug(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.appToCorePlugDriver.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.appToCorePlugDriver, s.gadgetNetlinkSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.netlink-test"})
	c.Assert(spec.SnippetForTag("snap.client-snap.netlink-test"), testutil.Contains, `network netlink,`)
}

func (s *NetlinkDriverInterfaceSuite) TestSecCompConnectedPlug(c *C) {
	spec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.appToCorePlugDriver.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.appToCorePlugDriver, s.osNetlinkSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.client-snap.netlink-test"})
	c.Assert(spec.SnippetForTag("snap.client-snap.netlink-test"), testutil.Contains, `socket AF_NETLINK - 777`)

	spec2 := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.appToGadgetPlugDriver.Snap()))
	c.Assert(spec2.AddConnectedPlug(s.iface, s.appToGadgetPlugDriver, s.gadgetNetlinkSlot), IsNil)
	c.Assert(spec2.SecurityTags(), DeepEquals, []string{"snap.client-snap.netlink-test"})
	c.Assert(spec2.SnippetForTag("snap.client-snap.netlink-test"), testutil.Contains, `socket AF_NETLINK - 100`)
}
