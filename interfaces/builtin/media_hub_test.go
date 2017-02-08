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
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type MediaHubInterfaceSuite struct {
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
}

var _ = Suite(&MediaHubInterfaceSuite{
	iface: &builtin.MediaHubInterface{},
	slot: &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap:      &snap.Info{SuggestedName: "media-hub"},
			Name:      "media-hub-server",
			Interface: "media-hub",
		},
	},
	plug: &interfaces.Plug{
		PlugInfo: &snap.PlugInfo{
			Snap:      &snap.Info{SuggestedName: "media-hub"},
			Name:      "media-hub-client",
			Interface: "media-hub",
		},
	},
})

func (s *MediaHubInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "media-hub")
}

// The label glob when all apps are bound to the media-hub slot
func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "media-hub",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
			},
			Name:      "media-hub",
			Interface: "media-hub",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.media-hub.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the media-hub slot
func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "media-hub",
				Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
			},
			Name:      "media-hub",
			Interface: "media-hub",
			Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.media-hub.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the media-hub slot
func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	app := &snap.AppInfo{Name: "app"}
	slot := &interfaces.Slot{
		SlotInfo: &snap.SlotInfo{
			Snap: &snap.Info{
				SuggestedName: "media-hub",
				Apps:          map[string]*snap.AppInfo{"app": app},
			},
			Name:      "media-hub",
			Interface: "media-hub",
			Apps:      map[string]*snap.AppInfo{"app": app},
		},
	}
	release.OnClassic = false
	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, slot, interfaces.SecurityAppArmor)
	c.Assert(err, IsNil)
	c.Assert(string(snippet), testutil.Contains, `peer=(label="snap.media-hub.app"),`)
}

func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetAppArmor(c *C) {
	system := interfaces.SecurityAppArmor

	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-session-strict>")
	c.Assert(string(snippet), testutil.Contains, "peer=(label=unconfined),")
}

func (s *MediaHubInterfaceSuite) TestPermanentSlotSnippetAppArmor(c *C) {
	system := interfaces.SecurityAppArmor

	snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Assert(string(snippet), testutil.Contains, "#include <abstractions/dbus-session-strict>")
	c.Assert(string(snippet), testutil.Contains, "peer=(label=unconfined),")
}

func (s *MediaHubInterfaceSuite) TestConnectedSlotSnippetAppArmor(c *C) {
	system := interfaces.SecurityAppArmor

	snippet, err := s.iface.ConnectedSlotSnippet(s.plug, s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Assert(string(snippet), Not(testutil.Contains), "peer=(label=unconfined),")
}

func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetSecComp(c *C) {
	system := interfaces.SecuritySecComp

	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "sendto\n")
}

func (s *MediaHubInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	system := interfaces.SecuritySecComp

	snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, Not(IsNil))

	c.Check(string(snippet), testutil.Contains, "sendto\n")
}

func (s *MediaHubInterfaceSuite) TestConnectedPlugSnippetDBus(c *C) {
	system := interfaces.SecurityDBus

	snippet, err := s.iface.ConnectedPlugSnippet(s.plug, s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}

func (s *MediaHubInterfaceSuite) TestPermanentSlotSnippetDBus(c *C) {
	system := interfaces.SecurityDBus

	snippet, err := s.iface.PermanentSlotSnippet(s.slot, system)
	c.Assert(err, IsNil)
	c.Assert(snippet, IsNil)
}
