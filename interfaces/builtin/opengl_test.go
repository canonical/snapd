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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type OpenglInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&OpenglInterfaceSuite{
	iface: builtin.MustInterface("opengl"),
})

const openglConsumerYaml = `name: consumer
apps:
 app:
  plugs: [opengl]
`

const openglCoreYaml = `name: core
type: os
slots:
  opengl:
`

func (s *OpenglInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, openglConsumerYaml, nil, "opengl")
	s.slot = MockSlot(c, openglCoreYaml, nil, "opengl")
}

func (s *OpenglInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "opengl")
}

func (s *OpenglInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "opengl",
		Interface: "opengl",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"opengl slots are reserved for the core snap")
}

func (s *OpenglInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *OpenglInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Equals, `
# Description: Can access opengl.

  # specific gl libs
  /var/lib/snapd/lib/gl/ r,
  /var/lib/snapd/lib/gl/** rm,

  /dev/dri/ r,
  /dev/dri/card0 rw,
  # nvidia
  @{PROC}/driver/nvidia/params r,
  @{PROC}/modules r,
  /dev/nvidia* rw,
  unix (send, receive) type=dgram peer=(addr="@nvidia[0-9a-f]*"),

  # eglfs
  /dev/vchiq rw,
  /sys/devices/pci[0-9]*/**/config r,
  /sys/devices/pci[0-9]*/**/{,subsystem_}device r,
  /sys/devices/pci[0-9]*/**/{,subsystem_}vendor r,

  # FIXME: this is an information leak and snapd should instead query udev for
  # the specific accesses associated with the above devices.
  /sys/bus/pci/devices/** r,
  /run/udev/data/+drm:card* r,
  /run/udev/data/+pci:[0-9]* r,

  # FIXME: for each device in /dev that this policy references, lookup the
  # device type, major and minor and create rules of this form:
  # /run/udev/data/<type><major>:<minor> r,
  # For now, allow 'c'haracter devices and 'b'lock devices based on
  # https://www.kernel.org/doc/Documentation/devices.txt
  /run/udev/data/c226:[0-9]* r,  # 226 drm
`)
}

func (s *OpenglInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to OpenGL stack`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "opengl")
}

func (s *OpenglInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *OpenglInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
