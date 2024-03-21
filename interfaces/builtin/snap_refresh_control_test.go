// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

type SnapRefreshControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SnapRefreshControlInterfaceSuite{
	iface: builtin.MustInterface("snap-refresh-control"),
})

func (s *SnapRefreshControlInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `
name: other
version: 0
apps:
 app:
  command: foo
  plugs: [snap-refresh-control]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 snap-refresh-control:
  interface: snap-refresh-control
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "snap-refresh-control")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "snap-refresh-control")
}

func (s *SnapRefreshControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "snap-refresh-control")
}

func (s *SnapRefreshControlInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have nil security snippet for apparmor and seccomp
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), IsNil)
	c.Assert(apparmorSpec.Snippets(), HasLen, 0)

	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)
}

func (s *SnapRefreshControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
