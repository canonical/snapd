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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type PulseAudioInterfaceSuite struct {
	iface       interfaces.Interface
	coreSlot    *interfaces.Slot
	classicSlot *interfaces.Slot
	plug        *interfaces.Plug
}

var _ = Suite(&PulseAudioInterfaceSuite{
	iface: &builtin.PulseAudioInterface{},
})

const pulseaudioMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [pulseaudio]
`

// a pulseaudio slot on a pulseaudio snap (as installed on a core/all-snap system)
const pulseaudioMockCoreSlotSnapInfoYaml = `name: pulseaudio
version: 1.0
apps:
 app1:
  command: foo
  slots: [pulseaudio]
`

// a pulseaudio slot on the core snap (as automatically added on classic)
const pulseaudioMockClassicSlotSnapInfoYaml = `name: core
type: os
slots:
 pulseaudio:
  interface: pulseaudio
`

func (s *PulseAudioInterfaceSuite) SetUpTest(c *C) {
	// pulseaudio snap with pulseaudio slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, pulseaudioMockCoreSlotSnapInfoYaml, nil)
	s.coreSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["pulseaudio"]}
	// pulseaudio slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, pulseaudioMockClassicSlotSnapInfoYaml, nil)
	s.classicSlot = &interfaces.Slot{SlotInfo: snapInfo.Slots["pulseaudio"]}
	// snap with the pulseaudio plug
	snapInfo = snaptest.MockInfo(c, pulseaudioMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: snapInfo.Plugs["pulseaudio"]}
}

func (s *PulseAudioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "pulseaudio")
}

func (s *PulseAudioInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(s.iface.SanitizeSlot(s.coreSlot), IsNil)
	c.Assert(s.iface.SanitizeSlot(s.classicSlot), IsNil)
}

func (s *PulseAudioInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(s.iface.SanitizePlug(s.plug), IsNil)
}

func (s *PulseAudioInterfaceSuite) TestSecCompOnClassic(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.classicSlot)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.other.app2"]), Equals, 1)
	c.Check(string(snippets["snap.other.app2"][0]), testutil.Contains, "shmctl\n")
}

func (s *PulseAudioInterfaceSuite) TestSecCompOnAllSnaps(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlot)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2", "snap.pulseaudio.app1"})
	c.Assert(seccompSpec.SnippetForTag("snap.pulseaudio.app1"), testutil.Contains, "listen\n")
	c.Assert(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "shmctl\n")
}
