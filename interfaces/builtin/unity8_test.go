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
			Snap:      &snap.Info{SuggestedName: "unity8-session"},
			Name:      "unity8-app",
			Interface: "unity8",
		},
	},
})

func (s *unity8InterfaceSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *unity8InterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *unity8InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "unity8")
}

func (s *unity8InterfaceSuite) TestRestrictedSlotConnection(c *C) {
	info := snap.Info{}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &info,
			Name:      "unity8-session",
			Interface: "unity8",
		},
	}

	// Official store snap
	info.SuggestedName = "unity8-session"
	info.PublisherID = "canonical"
	err := s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)

	// Other canonical snap
	info.SuggestedName = "foo"
	info.PublisherID = "canonical"
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, Not(IsNil))

	// Other unity8 snap
	info.SuggestedName = "unity8-session"
	info.PublisherID = "bar"
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, Not(IsNil))

	// Local snap
	info.SuggestedName = "foo"
	info.PublisherID = ""
	err = s.iface.SanitizeSlot(slot)
	c.Assert(err, IsNil)
}

func createMockFooPlug(c *C, content string) *interfaces.Plug {
	info := snaptest.MockSnap(c, content, "", &snap.SideInfo{Revision: snap.R(1)})

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

func (s *unity8InterfaceSuite) TestRestrictedPlugConnection(c *C) {
	// Basic success case
	plug := createMockFooPlug(c, `
name: success
apps:
 foo:
  plugs: [unity8]
`)
	err := s.iface.SanitizePlug(plug)
	c.Assert(err, IsNil)

	// No apps
	plug = createMockFooPlug(c, `
name: no-apps
plugs:
 unity8:
  interface: unity8
`)
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "\"unity8\" plug must be on an application")

	// Daemon
	plug = createMockFooPlug(c, `
name: daemon
apps:
 foo:
  daemon: simple
  plugs: [unity8]
`)
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "Application \"foo\" is a daemon, which isn't allowed to use interface \"unity8\"")

	// No desktop file
	plug = createMockFooPlug(c, `
name: no-desktop-file
apps:
 wrong-name:
  plugs: [unity8]
`)
	err = s.iface.SanitizePlug(plug)
	c.Assert(err, ErrorMatches, "Application \"wrong-name\" does not have a required desktop file for interface \"unity8\"")
}
