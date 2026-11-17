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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type NetworkInterfaceSuite struct {
	iface    interfaces.Interface
	base     string
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const netMockPlugSnapInfoYaml = `name: other
version: 1.0
base: %s
apps:
 app2:
  command: foo
  plugs: [network]
`

const netMockSlotSnapInfoYaml = `name: %s
version: 1.0
type: os
slots:
 network:
  interface: network
`

var _ = Suite(&NetworkInterfaceSuite{
	iface: builtin.MustInterface("network"),
	base: "core",
})

var _ = Suite(&NetworkInterfaceSuite{
	iface: builtin.MustInterface("network"),
	base: "core20",
})

var _ = Suite(&NetworkInterfaceSuite{
	iface: builtin.MustInterface("network"),
	base: "bare",
})

func (s *NetworkInterfaceSuite) SetUpTest(c *C) {
	s.slot, s.slotInfo = MockConnectedSlot(c, fmt.Sprintf(netMockSlotSnapInfoYaml, s.base), nil, "network")
	s.plug, s.plugInfo = MockConnectedPlug(c, fmt.Sprintf(netMockPlugSnapInfoYaml, s.base), nil, "network")
}

func (s *NetworkInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "network")
}

func (s *NetworkInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *NetworkInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NetworkInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(s.plug.AppSet())
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `tcp_fastopen`)
	if s.base == "core" {
		c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), Not(testutil.Contains), `bin/ping`)
	} else if s.base == "bare" {
		c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), Not(testutil.Contains), `bin/ping`)
	} else if s.base == "core20" {
		c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, `bin/ping`)
	} else {
		c.Errorf("Unknown base")
	}

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := seccomp.NewSpecification(s.plug.AppSet())
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "bind\n")
}

func (s *NetworkInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
