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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type unity8InterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&unity8InterfaceSuite{
	iface: &builtin.Unity8Interface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "unity8-session"},
			Name:      "unity8-session",
			Interface: "unity8",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "unity8-app"},
			Name:      "unity8-app",
			Interface: "unity8",
		},
	},
})

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
	dirs.SetRootDir(c.MkDir())
}

func (s *unity8InterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *unity8InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity8")
}

func (s *unity8InterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "name=com.canonical.Unity.Launcher")

	// connected plugs have a non-nil security snippet for seccomp
	snippet, err = s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecuritySecComp)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))
	c.Check(string(snippet), testutil.Contains, "shutdown\n")
}

func (s *unity8InterfaceSuite) TestSecurityTags(c *C) {
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Check(string(snippet), testutil.Contains, "peer=(label=\"snap.unity8-session.*\")")
}

func (s *unity8InterfaceSuite) TestDbusPaths(c *C) {
	// One command
	plug := createMockFooPlug(c, `
name: one-cmd
apps:
 one:
  plugs: [unity8]
`)
	snippet, err := s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Check(string(snippet), testutil.Contains, "path=/*one_2dcmd_5fone*")

	// Two commands
	plug = createMockFooPlug(c, `
name: two-cmds
apps:
 one:
  plugs: [unity8]
 two:
  plugs: [unity8]
`)
	snippet, err = s.iface.ConnectedPlugSnippet(plug, s.slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Check(string(snippet), testutil.Contains, "path=/{*two_2dcmds_5fone*,*two_2dcmds_5ftwo*}")
}
