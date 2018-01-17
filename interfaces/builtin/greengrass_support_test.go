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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type GreengrassSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const ggMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [greengrass-support]
`

var _ = Suite(&GreengrassSupportInterfaceSuite{
	iface: builtin.MustInterface("greengrass-support"),
})

func (s *GreengrassSupportInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", Type: snap.TypeOS},
		Name:      "greengrass-support",
		Interface: "greengrass-support",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil)
	plugSnap := snaptest.MockInfo(c, ggMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["greengrass-support"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil)
}

func (s *GreengrassSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "greengrass-support")
}

func (s *GreengrassSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "greengrass-support",
		Interface: "greengrass-support",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"greengrass-support slots are reserved for the core snap")
}

func (s *GreengrassSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *GreengrassSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Check(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "# for overlayfs and various bind mounts\nmount\numount2\npivot_root\n")

	apparmorSpec := &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "mount fstype=overlay no_source -> /var/snap/@{SNAP_NAME}/**,\n")
}

func (s *GreengrassSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
