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

type CifsMountControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const cifsMountControlConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [cifs-mount-control]
`

const cifsMountControlCoreYaml = `name: core
version: 0
type: os
slots:
  cifs-mount-control:
`

var _ = Suite(&CifsMountControlInterfaceSuite{
	iface: builtin.MustInterface("cifs-mount-control"),
})

func (s *CifsMountControlInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, cifsMountControlConsumerYaml, nil, "cifs-mount-control")
	s.slot, s.slotInfo = MockConnectedSlot(c, cifsMountControlCoreYaml, nil, "cifs-mount-control")
}

func (s *CifsMountControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "cifs-mount-control")
}

func (s *CifsMountControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
	slot := &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "some-snap"},
		Name:      "cifs-mount-control",
		Interface: "cifs-mount-control",
	}
	c.Assert(interfaces.BeforePrepareSlot(s.iface, slot), ErrorMatches,
		"cifs-mount-control slots are reserved for the core snap")
}

func (s *CifsMountControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *CifsMountControlInterfaceSuite) TestAppArmorSpec(c *C) {
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/run/mount/utab`)
}

func (s *CifsMountControlInterfaceSuite) TestSecCompSpec(c *C) {
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "mount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "umount2\n")
}

func (s *CifsMountControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows to mount and unmount CIFS shares`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "cifs-mount-control")
}

func (s *CifsMountControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
