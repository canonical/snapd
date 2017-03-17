// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

type ThumbnailerInterfaceSuite struct {
	iface       interfaces.Interface
	coreSlot    *interfaces.Slot
	classicSlot *interfaces.Slot
	plug        *interfaces.Plug
}

var _ = Suite(&ThumbnailerInterfaceSuite{})

func (s *ThumbnailerInterfaceSuite) SetUpTest(c *C) {
	// a thumbnailer slot on a thumbnailer snap
	const thumbnailerMockCoreSlotSnapInfoYaml = `name: thumbnailer
version: 1.0
apps:
 app:
  command: foo
  slots: [thumbnailer]
`
	// a thumbnailer slot on the core snap (as automatically added on classic)
	const thumbnailerMockClassicSlotSnapInfoYaml = `name: core
type: os
slots:
 thumbnailer:
  interface: thumbnailer
`
	const mockPlugSnapInfo = `name: client
version: 1.0
apps:
 app:
  command: foo
  plugs: [thumbnailer]
`
	s.iface = &builtin.ThumbnailerInterface{}
	// thumbnailer snap with thumbnailer slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, thumbnailerMockCoreSlotSnapInfoYaml, nil)
	s.coreSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["thumbnailer"]}
	// thumbnailer slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, thumbnailerMockClassicSlotSnapInfoYaml, nil)
	s.classicSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["thumbnailer"]}

	plugSnap := snaptest.MockInfo(c, mockPlugSnapInfo, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["thumbnailer"]}
}

func (s *ThumbnailerInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "thumbnailer")
}

func (s *ThumbnailerInterfaceSuite) TestSanitizeIncorrectInterface(c *C) {
	c.Check(func() { s.iface.SanitizeSlot(&interfaces.Slot{SlotInfo: &snap.SlotInfo{Interface: "other"}}) },
		PanicMatches, `slot is not of interface "thumbnailer"`)
	c.Check(func() { s.iface.SanitizePlug(&interfaces.Plug{PlugInfo: &snap.PlugInfo{Interface: "other"}}) },
		PanicMatches, `plug is not of interface "thumbnailer"`)
}

func (s *ThumbnailerInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected slots have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.thumbnailer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.thumbnailer.app"), testutil.Contains, `interface=com.canonical.Thumbnailer`)

	// slots have no permanent snippet on classic
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	// slots have a permanent non-nil security snippet for apparmor
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddPermanentSlot(s.iface, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.thumbnailer.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.thumbnailer.app"), testutil.Contains, `member={RequestName,ReleaseName,GetConnectionCredentials}`)
}

func (s *ThumbnailerInterfaceSuite) TestSlotGrantedAccessToPlugFiles(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.thumbnailer.app"})
	snippet := apparmorSpec.SnippetForTag("snap.thumbnailer.app")
	c.Check(snippet, testutil.Contains, `@{INSTALL_DIR}/client/**`)
	c.Check(snippet, testutil.Contains, `@{HOME}/snap/client/**`)
	c.Check(snippet, testutil.Contains, `/var/snap/client/**`)
}
