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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type legacyMntSuiteSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&legacyMntSuiteSuite{
	iface: builtin.MustInterface("legacy-mnt"),
})

func (s *legacyMntSuiteSuite) SetUpTest(c *C) {
	consumingSnapInfo := snaptest.MockInfo(c, `
name: consumer 
version: 0
apps:
  other:
    command: foo
    plugs: [legacy-mnt]
`, nil)
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "legacy-mnt",
		Interface: "legacy-mnt",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
	s.plugInfo = consumingSnapInfo.Plugs["legacy-mnt"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *legacyMntSuiteSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "legacy-mnt")
}

func (s *legacyMntSuiteSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "legacy-mnt",
		Interface: "legacy-mnt",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"legacy-mnt slots are reserved for the core snap")
}

func (s *legacyMntSuiteSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *legacyMntSuiteSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	err := spec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.other"})
	c.Check(spec.SnippetForTag("snap.consumer.other"), testutil.Contains, "/mnt/*/** rwk,")
}

func (s *legacyMntSuiteSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
