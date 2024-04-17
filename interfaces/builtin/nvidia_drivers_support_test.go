// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type NvidiaDriversSupportSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

var _ = Suite(&NvidiaDriversSupportSuite{
	iface: builtin.MustInterface("nvidia-drivers-support"),
})

const nvidiaDriversSupportMockPlugSnapInfo = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [nvidia-drivers-support]
`

func (s *NvidiaDriversSupportSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "nvidia-drivers-support",
		Interface: "nvidia-drivers-support",
		Apps: map[string]*snap.AppInfo{
			"app1": {
				Snap: &snap.Info{
					SuggestedName: "core",
				},
				Name: "app1"}},
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)

	plugSnap := snaptest.MockInfo(c, nvidiaDriversSupportMockPlugSnapInfo, nil)
	s.plugInfo = plugSnap.Plugs["nvidia-drivers-support"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *NvidiaDriversSupportSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "nvidia-drivers-support")
}

func (s *NvidiaDriversSupportSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *NvidiaDriversSupportSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *NvidiaDriversSupportSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "/{,usr/}bin/mknod")

	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "mknodat - - |S_IFCHR -")
}

func (s *NvidiaDriversSupportSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.slotInfo), Equals, true)
}

func (s *NvidiaDriversSupportSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
