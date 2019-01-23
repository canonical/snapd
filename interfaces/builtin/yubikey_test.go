// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type yubikeyInterfaceSuite struct {
	testutil.BaseTest

	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&yubikeyInterfaceSuite{
	iface: builtin.MustInterface("yubikey"),
})

const yubikeyConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [yubikey]
`

const yubikeyCoreYaml = `name: core
version: 0
type: os
slots:
  yubikey:
`

func (s *yubikeyInterfaceSuite) SetUpTest(c *C) {
	//s.BaseTest.SetUpTest(c)

	s.plug, s.plugInfo = MockConnectedPlug(c, yubikeyConsumerYaml, nil, "yubikey")
	s.slot, s.slotInfo = MockConnectedSlot(c, yubikeyCoreYaml, nil, "yubikey")
}

func (s *yubikeyInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "yubikey")
}

func (s *yubikeyInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "yubikey",
		Interface: "yubikey",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches, "yubikey slots are reserved for the core snap")
}

func (s *yubikeyInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *yubikeyInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `# Description: Allow write access to yubikey hidraw devices.`)
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/hidraw* rw,`)
}

func (s *yubikeyInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.Snippets(), HasLen, 14)
	c.Assert(spec.Snippets(), testutil.Contains, `# yubikey
# Yubico YubiKey
SUBSYSTEM=="hidraw", KERNEL=="hidraw*", ATTRS{idVendor}=="1050", ATTRS{idProduct}=="0113|0114|0115|0116|0120|0200|0402|0403|0406|0407|0410", TAG+="snap_consumer_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_consumer_app", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_consumer_app $devpath $major:$minor"`)
}

func (s *yubikeyInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to yubikey devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "yubikey")
}

func (s *yubikeyInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *yubikeyInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
