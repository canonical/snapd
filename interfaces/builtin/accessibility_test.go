// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

type AccessibilityLegacyInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

const accessibilityLegacyConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [desktop-legacy]
`

const accessibilityLegacyCoreYaml = `name: core
version: 0
type: os
slots:
  desktop-legacy:
  accessibility:
`

var _ = Suite(&AccessibilityLegacyInterfaceSuite{
	iface: builtin.MustInterface("accessibility"),
})

func (s *AccessibilityLegacyInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, accessibilityLegacyConsumerYaml, nil, "accessibility")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, accessibilityLegacyCoreYaml, nil, "accessibility")
}

func (s *AccessibilityLegacyInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "accessibility")
}

func (s *AccessibilityLegacyInterfaceSuite) TestAppArmorSpecForConnectedPlug(c *C) {
	// connected plug to core slot
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/dbus-accessibility-strict>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.a11y.atspi.Registry, label=\"snap.core.\"),")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.a11y.atspi.Registry, label=unconfined),")
}

func (s *AccessibilityLegacyInterfaceSuite) TestAppArmorSpecForNotConnectedPlug(c *C) {
	// connected plug to core slot
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/dbus-accessibility-strict>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "peer=(name=org.a11y.atspi.Registry, label=\"snap.core.\"),")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "peer=(name=org.a11y.atspi.Registry, label=unconfined),")
}
