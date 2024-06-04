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
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type loginSessionControlSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&loginSessionControlSuite{
	iface: builtin.MustInterface("login-session-control"),
})

func (s *loginSessionControlSuite) SetUpTest(c *C) {
	consumingSnapInfo := snaptest.MockInfo(c, `
name: other
version: 0
apps:
 app:
    command: foo
    plugs: [login-session-control]
`, nil)
	s.plugInfo = consumingSnapInfo.Plugs["login-session-control"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "login-session-control",
		Interface: "login-session-control",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
}

func (s *loginSessionControlSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "login-session-control")
}

func (s *loginSessionControlSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *loginSessionControlSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *loginSessionControlSuite) TestConnectedPlugSnippet(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app"})
	snippet := apparmorSpec.SnippetForTag("snap.other.app")
	c.Assert(snippet, testutil.Contains, `Can setup login session & seat.`)

	c.Assert(snippet, testutil.Contains, `dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1/{seat,session}/**
    interface=org.freedesktop.DBus.Properties
    member={GetAll,PropertiesChanged,Get}
    peer=(label=unconfined),`)
	c.Assert(snippet, testutil.Contains, `dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1/seat/**
    interface=org.freedesktop.login1.Seat
    peer=(label=unconfined),`)
	c.Assert(snippet, testutil.Contains, `dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1/session/**
    interface=org.freedesktop.login1.Session
    peer=(label=unconfined),`)
	c.Assert(snippet, testutil.Contains, `dbus (send,receive)
    bus=system
    path=/org/freedesktop/login1
    interface=org.freedesktop.login1.Manager
    member={ActivateSession,GetSession,GetSeat,KillSession,ListSessions,LockSession,TerminateSession,UnlockSession}
    peer=(label=unconfined),`)
}

func (s *loginSessionControlSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
