// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

type ClassicSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const classicSupportMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [classic-support]
`

const classicSupportMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 classic-support:
  interface: classic-support
`

var _ = Suite(&ClassicSupportInterfaceSuite{
	iface: builtin.MustInterface("classic-support"),
})

func (s *ClassicSupportInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, classicSupportMockSlotSnapInfoYaml, nil, "classic-support")
	s.plug, s.plugInfo = MockConnectedPlug(c, classicSupportMockPlugSnapInfoYaml, nil, "classic-support")
}

func (s *ClassicSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "classic-support")
}

func (s *ClassicSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *ClassicSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *ClassicSupportInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	c.Check(string(snippet), testutil.Contains, "/usr/bin/systemd-run Uxr,\n")
	c.Check(string(snippet), testutil.Contains, "/bin/systemctl Uxr,\n")

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Check(seccompSpec.SnippetForTag("snap.other.app"), testutil.Contains, "mount\n")
}

func (s *ClassicSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
