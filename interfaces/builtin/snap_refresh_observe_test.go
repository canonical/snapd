// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type SnapRefreshObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SnapRefreshObserveInterfaceSuite{
	iface: builtin.MustInterface("snap-refresh-observe"),
})

func (s *SnapRefreshObserveInterfaceSuite) SetUpTest(c *C) {
	const coreSlotYaml = `
name: core
type: os
version: 1.0
slots:
  snap-refresh-observe:
 `
	s.slot, s.slotInfo = MockConnectedSlot(c, coreSlotYaml, nil, "snap-refresh-observe")

	const appPlugYaml = `
name: other
version: 0
apps:
  app:
    command: foo
    plugs: [snap-refresh-observe]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, appPlugYaml, nil, "snap-refresh-observe")
}

func (s *SnapRefreshObserveInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "snap-refresh-observe")
}

func (s *SnapRefreshObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *SnapRefreshObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *SnapRefreshObserveInterfaceSuite) TestAppArmor(c *C) {
	// The interface generates no AppArmor rules
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.slot.Snap(), nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plugInfo.Snap, nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.slotInfo.Snap, nil))

	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)
}

func (s *SnapRefreshObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
