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

type SnapFDEControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SnapFDEControlInterfaceSuite{
	iface: builtin.MustInterface("snap-fde-control"),
})

func (s *SnapFDEControlInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `
name: other
version: 0
apps:
 app:
  command: foo
  plugs: [snap-fde-control]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 snap-fde-control:
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "snap-fde-control")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "snap-fde-control")
}

func (s *SnapFDEControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "snap-fde-control")
}

func (s *SnapFDEControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *SnapFDEControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *SnapFDEControlInterfaceSuite) TestAppArmor(c *C) {
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

func (s *SnapFDEControlInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, true)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to the FDE subset of snapd's system-volumes API`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "deny-auto-connection: true")
}

func (s *SnapFDEControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
