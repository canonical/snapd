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

type DesktopInputInterfaceSuite struct {
	iface    interfaces.Interface
	coreSlot *interfaces.Slot
	plug     *interfaces.Plug
}

var _ = Suite(&DesktopInputInterfaceSuite{
	iface: builtin.MustInterface("desktop-input"),
})

const desktopInputConsumerYaml = `name: consumer
apps:
 app:
  plugs: [desktop-input]
`

const desktopInputCoreYaml = `name: core
type: os
slots:
  desktop-input:
`

func (s *DesktopInputInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, desktopInputConsumerYaml, nil, "desktop-input")
	s.coreSlot = MockSlot(c, desktopInputCoreYaml, nil, "desktop-input")
}

func (s *DesktopInputInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "desktop-input")
}

func (s *DesktopInputInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.coreSlot.Sanitize(s.iface), IsNil)
	// desktop-input slot currently only used with core
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "desktop-input",
		Interface: "desktop-input",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"desktop-input slots are reserved for the core snap")
}

func (s *DesktopInputInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *DesktopInputInterfaceSuite) TestAppArmorSpec(c *C) {
	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access common desktop input methods")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `peer=(addr="@/tmp/ibus/dbus-*"),`)

	// connected plug to core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *DesktopInputInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows privileged access to desktop input methods`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "desktop-input")
}

func (s *DesktopInputInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
