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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
	consumingSnapInfo := snaptest.MockInfo(c, `
name: other
version: 0
apps:
  app:
    command: foo
    plugs: [snap-refresh-control]
`, nil)
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "snap-refresh-control",
		Interface: "snap-refresh-control",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	s.plugInfo = consumingSnapInfo.Plugs["snap-refresh-control"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *SnapRefreshControlInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "snap-refresh-control")
}

func (s *SnapRefreshControlInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have nil security snippet for apparmor and seccomp
	appSet := mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	apparmorSpec := apparmor.NewSpecification(appSet)
	mylog.Check(apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot))

	c.Assert(apparmorSpec.SecurityTags(), IsNil)
	c.Assert(apparmorSpec.Snippets(), HasLen, 0)

	appSet = mylog.Check2(interfaces.NewSnapAppSet(s.plug.Snap(), nil))

	seccompSpec := seccomp.NewSpecification(appSet)
	c.Assert(seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(seccompSpec.Snippets(), HasLen, 0)
}

func (s *SnapRefreshControlInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
