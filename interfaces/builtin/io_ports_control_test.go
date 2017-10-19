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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type ioPortsControlInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&ioPortsControlInterfaceSuite{
	iface: builtin.MustInterface("io-ports-control"),
})

const ioPortsControlConsumerYaml = `name: consumer
apps:
 app:
  plugs: [io-ports-control]
`

const ioPortsControlCoreYaml = `name: core
type: os
slots:
  io-ports-control:
`

func (s *ioPortsControlInterfaceSuite) SetUpTest(c *C) {
	s.plug = builtin.MockPlug(c, ioPortsControlConsumerYaml, nil, "io-ports-control")
	s.slot = builtin.MockSlot(c, ioPortsControlCoreYaml, nil, "io-ports-control")
}

func (s *ioPortsControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "io-ports-control")
}

func (s *ioPortsControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "io-ports-control",
		Interface: "io-ports-control",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"io-ports-control slots are reserved for the core snap")
}

func (s *ioPortsControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *ioPortsControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/dev/port rw,")
}

func (s *ioPortsControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "ioperm\n")
}

func (s *ioPortsControlInterfaceSuite) TestUDevSpec(c *C) {
	udevSpec := &udev.Specification{}
	c.Assert(udevSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(udevSpec.Snippets(), HasLen, 1)
	c.Assert(udevSpec.Snippets()[0], Equals, `KERNEL=="port", TAG+="snap_consumer_app"`)
}

func (s *ioPortsControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to all I/O ports`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "io-ports-control")
}

func (s *ioPortsControlInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *ioPortsControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
