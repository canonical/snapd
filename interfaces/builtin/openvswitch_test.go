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
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type OpenvSwitchInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&OpenvSwitchInterfaceSuite{
	iface: builtin.MustInterface("openvswitch"),
})

func (s *OpenvSwitchInterfaceSuite) SetUpTest(c *C) {
	var mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app:
  command: foo
  plugs: [openvswitch]
`
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "openvswitch",
		Interface: "openvswitch",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	snapInfo := snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["openvswitch"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *OpenvSwitchInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "openvswitch")
}

func (s *OpenvSwitchInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *OpenvSwitchInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *OpenvSwitchInterfaceSuite) TestUsedSecuritySystems(c *C) {
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "run/openvswitch/db.sock rw")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/run/openvswitch/*.mgmt rw")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/run/openvswitch/ovs-vswitchd.*.ctl rw")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app"), testutil.Contains, "/run/openvswitch/ovs-vswitchd.pid rw")
}

func (s *OpenvSwitchInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
