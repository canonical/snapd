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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type SnapdThemesControlInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&SnapdThemesControlInterfaceSuite{
	iface: builtin.MustInterface("snapd-themes-control"),
})

func (s *SnapdThemesControlInterfaceSuite) SetUpTest(c *C) {
	const coreSlotYaml = `
name: core
type: os
version: 1.0
slots:
  snapd-themes-control:
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreSlotYaml, nil, "snapd-themes-control")

	const appPlugYaml = `
name: other
version: 0
apps:
 app:
    command: foo
    plugs: [snapd-themes-control]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, appPlugYaml, nil, "snapd-themes-control")
}

func (s *SnapdThemesControlInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "snapd-themes-control")
}

func (s *SnapdThemesControlInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *SnapdThemesControlInterfaceSuite) TestSanitizePlug(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *SnapdThemesControlInterfaceSuite) TestAppArmor(c *C) {
	// The interface generates no AppArmor rules
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.slot), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)

	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.slotInfo), IsNil)
	c.Check(spec.SecurityTags(), HasLen, 0)
}

func (s *SnapdThemesControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
