// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

type ScreencastLegacyInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&ScreencastLegacyInterfaceSuite{
	iface: builtin.MustInterface("screencast-legacy"),
})

const screencastLegacyConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [screencast-legacy]
`

const screencastLegacyCoreYaml = `name: core
version: 0
type: os
slots:
  screencast-legacy:
`

func (s *ScreencastLegacyInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, screencastLegacyConsumerYaml, nil, "screencast-legacy")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, screencastLegacyCoreYaml, nil, "screencast-legacy")
}

func (s *ScreencastLegacyInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "screencast-legacy")
}

func (s *ScreencastLegacyInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	// screencast-legacy slot currently only used with core
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "screencast-legacy",
		Interface: "screencast-legacy",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"screencast-legacy slots are reserved for the core snap")
}

func (s *ScreencastLegacyInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *ScreencastLegacyInterfaceSuite) TestAppArmorSpec(c *C) {
	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access common desktop screenshot, screencast and recording")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "path=/org/gnome/Shell/Screen{cast,shot}")

	// connected plug to core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *ScreencastLegacyInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows screen recording and audio recording, and also writing to arbitrary filesystem paths`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "screencast-legacy")
}

func (s *ScreencastLegacyInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
