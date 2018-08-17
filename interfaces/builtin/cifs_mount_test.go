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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type CifsMountInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const cifsMountConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [cifs-mount]
`

const cifsMountCoreYaml = `name: core
version: 0
type: os
slots:
  cifs-mount:
`

var _ = Suite(&CifsMountInterfaceSuite{
	iface: builtin.MustInterface("cifs-mount"),
})

func (s *CifsMountInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, cifsMountConsumerYaml, nil, "cifs-mount")
	s.slot, s.slotInfo = MockConnectedSlot(c, cifsMountCoreYaml, nil, "cifs-mount")
}

func (s *CifsMountInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cifs-mount")
}

func (s *CifsMountInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "cifs-mount",
		Interface: "cifs-mount",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"cifs-mount slots are reserved for the core snap")
}

func (s *CifsMountInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *CifsMountInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `#deny /run/mount/utab w,`)
}

func (s *CifsMountInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "mount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount2\n")
}

func (s *CifsMountInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows mounting and unmounting CIFS filesystems`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "cifs-mount")
}

func (s *CifsMountInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
