// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

type CoreSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&CoreSupportInterfaceSuite{
	iface: builtin.MustInterface("core-support"),
})

func (s *CoreSupportInterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
hooks:
 prepare-device:
     plugs: [core-support]
`
	const mockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 core-support:
  interface: core-support
`
	s.slot, s.slotInfo = MockConnectedSlot(c, mockSlotSnapInfoYaml, nil, "core-support")
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "core-support")
}

func (s *CoreSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "core-support")
}

func (s *CoreSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *CoreSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *CoreSupportInterfaceSuite) TestNoSecuritySystems(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(seccompSpec.SecurityTags(), HasLen, 0)
}

func (s *CoreSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
