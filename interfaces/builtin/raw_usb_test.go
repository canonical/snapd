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

type RawUsbInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&RawUsbInterfaceSuite{
	iface: builtin.MustInterface("raw-usb"),
})

const rawusbConsumerYaml = `name: consumer
apps:
 app:
  plugs: [raw-usb]
`

const rawusbCoreYaml = `name: core
type: os
slots:
  raw-usb:
`

func (s *RawUsbInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, rawusbConsumerYaml, nil, "raw-usb")
	s.slot = MockSlot(c, rawusbCoreYaml, nil, "raw-usb")
}

func (s *RawUsbInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "raw-usb")
}

func (s *RawUsbInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "raw-usb",
		Interface: "raw-usb",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"raw-usb slots are reserved for the core snap")
}

func (s *RawUsbInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *RawUsbInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/sys/bus/usb/devices/`)
}

func (s *RawUsbInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets(), testutil.Contains, `# raw-usb
SUBSYSTEM=="usb", TAG+="snap_consumer_app"`)
}

func (s *RawUsbInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows raw access to all USB devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "raw-usb")
}

func (s *RawUsbInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *RawUsbInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
