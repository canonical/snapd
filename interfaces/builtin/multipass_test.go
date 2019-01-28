// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type MultipassInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const multipassMockPlugSnapInfoYaml = `name: multipass
version: 1.0
apps:
 app:
  command: foo
  plugs: [multipass]
`

var _ = Suite(&MultipassInterfaceSuite{
	iface: builtin.MustInterface("multipass"),
})

func (s *MultipassInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "multipass",
		},
		Name:      "multipass-daemon",
		Interface: "multipass",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, multipassMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["multipass"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *MultipassInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "multipass")
}

func (s *MultipassInterfaceSuite) TestConnectedPlugSnippet(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.multipass.app"})
	c.Check(seccompSpec.SnippetForTag("snap.multipass.app"), testutil.Contains, "bind\n")
}

func (s *MultipassInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *MultipassInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *MultipassInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
