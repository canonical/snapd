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
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type FramebufferInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

const framebufferConsumerYaml = `
name: consumer
apps:
  app:
    plugs: [framebuffer]
`

const framebufferOsYaml = `
name: core
type: os
slots:
  framebuffer:
`

var _ = Suite(&FramebufferInterfaceSuite{
	iface: builtin.MustInterface("framebuffer"),
})

func (s *FramebufferInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, framebufferConsumerYaml, nil, "framebuffer")
	s.slot = MockSlot(c, framebufferOsYaml, nil, "framebuffer")
}

func (s *FramebufferInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "framebuffer")
}

func (s *FramebufferInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.slot.Sanitize(s.iface), IsNil)
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "framebuffer",
		Interface: "framebuffer",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"framebuffer slots are reserved for the core snap")
}

func (s *FramebufferInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *FramebufferInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/dev/fb[0-9]* rw,`)
}

func (s *FramebufferInterfaceSuite) TestUDevSpec(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil), IsNil)
	c.Assert(spec.Snippets(), HasLen, 1)
	c.Assert(spec.Snippets()[0], Equals, `# framebuffer
KERNEL=="fb[0-9]*", TAG+="snap_consumer_app"`)
}

func (s *FramebufferInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to universal framebuffer devices`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "framebuffer")
}

func (s *FramebufferInterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plug, s.slot), Equals, true)
}

func (s *FramebufferInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
