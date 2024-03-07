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

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type RemoteprocInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&RemoteprocInterfaceSuite{
	iface: builtin.MustInterface("remoteproc"),
})

func (s *RemoteprocInterfaceSuite) SetUpTest(c *C) {
	consumingSnapInfo := snaptest.MockInfo(c, `
name: other
version: 0
apps:
 app:
    command: foo
    plugs: [remoteproc]
 `, nil)
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "remoteproc",
		Interface: "remoteproc",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	s.plugInfo = consumingSnapInfo.Plugs["remoteproc"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *RemoteprocInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "remoteproc")
}

func (s *RemoteprocInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *RemoteprocInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *RemoteprocInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/sys/devices/platform/**/remoteproc/remoteproc[0-9]/firmware rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/sys/devices/platform/**/remoteproc/remoteproc[0-9]/state rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/sys/devices/platform/**/remoteproc/remoteproc[0-9]/name r,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/sys/devices/platform/**/remoteproc/remoteproc[0-9]/coredump rw,`)
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, `/sys/devices/platform/**/remoteproc/remoteproc[0-9]/recovery rw,`)
}

func (s *RemoteprocInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
