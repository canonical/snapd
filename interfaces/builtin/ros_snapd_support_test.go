// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

type RosSnapdSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&RosSnapdSupportInterfaceSuite{
	iface: builtin.MustInterface("ros-snapd-support"),
})

func (s *RosSnapdSupportInterfaceSuite) SetUpTest(c *C) {
	const rosSnapdSupportCoreYaml = `name: core
version: 0
type: os
slots:
  ros-snapd-support:
`

	const rosSnapdSupportConsumerYaml = `name: consumer
version: 0
apps:
  app:
    command: foo
    plugs: [ros-snapd-support]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, rosSnapdSupportConsumerYaml, nil, "ros-snapd-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, rosSnapdSupportCoreYaml, nil, "ros-snapd-support")
}

func (s *RosSnapdSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "ros-snapd-support")
}

func (s *RosSnapdSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *RosSnapdSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *RosSnapdSupportInterfaceSuite) TestAppArmor(c *C) {
	// The interface generates no AppArmor rules
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	appSet, err = interfaces.NewSnapAppSet(s.slot.Snap(), nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	appSet, err = interfaces.NewSnapAppSet(s.plugInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	appSet, err = interfaces.NewSnapAppSet(s.slotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)
}

func (s *RosSnapdSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *RosSnapdSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, "allows ros-snapd the use of snapd's apps control API")
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "ros-snapd-support")
}
