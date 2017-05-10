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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type unity8InterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&unity8InterfaceSuite{})

var _ = Suite(&FirewallControlInterfaceSuite{})

func createMockFooPlug(c *C, content string) *interfaces.Plug {
	info := snaptest.MockSnap(c, content, "", &snap.SideInfo{Revision: snap.R(3)})

	desktopDir := filepath.Join(info.MountDir(), "meta", "gui")
	err := os.MkdirAll(desktopDir, 0755)
	c.Assert(err, IsNil)

	desktopPath := filepath.Join(desktopDir, "foo.desktop")
	err = ioutil.WriteFile(desktopPath, []byte(""), 0644)
	c.Assert(err, IsNil)

	return &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      info,
			Name:      info.Name(),
			Interface: "unity8",
			Apps:      info.Apps,
		},
	}
}

func (s *unity8InterfaceSuite) SetUpTest(c *C) {
	const mockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 unity8-app:
  command: foo
  plugs: [unity8]
`
	dirs.SetRootDir(c.MkDir())
	s.iface = &builtin.Unity8Interface{}
	s.slot = &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "unity8-session"},
			Name:      "unity8-session",
			Interface: "unity8",
		},
	}
	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["unity8"]}

}

func (s *unity8InterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *unity8InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity8")
}

func (s *unity8InterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.unity8-app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.unity8-app"), testutil.Contains, "name=com.canonical.URLDispatcher")

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := &seccomp.Specification{}
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.unity8-app"})
	c.Check(seccompSpec.SnippetForTag("snap.other.unity8-app"), testutil.Contains, "shutdown\n")
}

func (s *unity8InterfaceSuite) TestSecurityTags(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, nil, s.slot, nil)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.unity8-app"})
	c.Check(apparmorSpec.SnippetForTag("snap.other.unity8-app"), testutil.Contains, "label=\"snap.unity8-session.*\"")
}
