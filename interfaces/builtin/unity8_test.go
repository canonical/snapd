// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type unity8InterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&unity8InterfaceSuite{
	iface: builtin.MustInterface("unity8"),
})

func (s *unity8InterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 unity8-app:
  command: foo
  plugs: [unity8]
`
	dirs.SetRootDir(c.MkDir())
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "unity8-session"},
		Name:      "unity8-session",
		Interface: "unity8",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["unity8"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)

}

func (s *unity8InterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *unity8InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity8")
}

func (s *unity8InterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.unity8-app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.unity8-app"), testutil.Contains, "name=com.canonical.URLDispatcher")
}

func (s *unity8InterfaceSuite) TestSecurityTags(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	apparmorSpec := apparmor.NewSpecification(appSet)
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.unity8-app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.unity8-app"), testutil.Contains, "label=\"snap.unity8-session.*\"")
}

func (s *unity8InterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
