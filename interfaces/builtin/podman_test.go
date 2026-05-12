// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

type PodmanInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const podmanMockPlugSnapInfoYaml = `name: podman
version: 1.0
apps:
 app:
  command: foo
  plugs: [podman]
`

const podmanMockSlotSnapInfoYaml = `name: core
version: 1.0
type: os
slots:
 podman:
  interface: podman
`

var _ = Suite(&PodmanInterfaceSuite{
	iface: builtin.MustInterface("podman"),
})

func (s *PodmanInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, podmanMockSlotSnapInfoYaml, nil, "podman")
	s.plug, s.plugInfo = MockConnectedPlug(c, podmanMockPlugSnapInfoYaml, nil, "podman")
}

func (s *PodmanInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "podman")
}

func (s *PodmanInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.podman.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.podman.app"), testutil.Contains, `/{,var/}run/podman/podman.sock`)
	c.Assert(apparmorSpec.SnippetForTag("snap.podman.app"), testutil.Contains, `/{,var/}run/user/[0-9]*/podman/podman.sock`)

	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.podman.app"})
	c.Check(seccompSpec.SnippetForTag("snap.podman.app"), testutil.Contains, "bind\n")
}

func (s *PodmanInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *PodmanInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *PodmanInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
