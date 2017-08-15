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
	"github.com/snapcore/snapd/testutil"
)

type AccessibilityInterfaceSuite struct {
	iface    interfaces.Interface
	coreSlot *interfaces.Slot
	plug     *interfaces.Plug
}

var _ = Suite(&AccessibilityInterfaceSuite{
	iface: builtin.MustInterface("accessibility"),
})

const accessibilityConsumerYaml = `name: consumer
apps:
 app:
  plugs: [accessibility]
`

const accessibilityCoreYaml = `name: core
type: os
slots:
  accessibility:
`

func (s *AccessibilityInterfaceSuite) SetUpTest(c *C) {
	s.plug = MockPlug(c, accessibilityConsumerYaml, nil, "accessibility")
	s.coreSlot = MockSlot(c, accessibilityCoreYaml, nil, "accessibility")
}

func (s *AccessibilityInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "accessibility")
}

func (s *AccessibilityInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.coreSlot.Sanitize(s.iface), IsNil)
	// accessibility slot currently only used with core
	slot := &interfaces.Slot{SlotInfo: &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "accessibility",
		Interface: "accessibility",
	}}
	c.Assert(slot.Sanitize(s.iface), ErrorMatches,
		"accessibility slots are reserved for the core snap")
}

func (s *AccessibilityInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.plug.Sanitize(s.iface), IsNil)
}

func (s *AccessibilityInterfaceSuite) TestAppArmorSpec(c *C) {
	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/dbus-accessibility-strict>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "interface=org.a11y.Bus")

	// connected plug to core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AccessibilityInterfaceSuite) TestSecCompSpec(c *C) {
	// connected plug to core slot
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "listen")

	// connected plug to core slot
	spec = &seccomp.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, nil, s.coreSlot, nil), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AccessibilityInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows using desktop accessibility (a11y)`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "accessibility")
}

func (s *AccessibilityInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
