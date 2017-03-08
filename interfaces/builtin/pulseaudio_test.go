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
	iface interfaces.Interface
	slot  *interfaces.Slot
	plug  *interfaces.Plug
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

const pulseaudioMockSlotSnapInfoYaml = `name: pulseaudio
version: 1.0
apps:
 app1:
  command: foo
  slots: [pulseaudio]
`

const pulseaudioMockSlotOSSnapInfoYaml = `name: pulseaudio
version: 1.0
slots:
 pulseaudio:
  interface: pulseaudio
`

func (s *PulseAudioInterfaceSuite) SetUpTest(c *C) {
	slotSnap := snaptest.MockInfo(c, pulseaudioMockSlotOSSnapInfoYaml, nil)
	plugSnap := snaptest.MockInfo(c, pulseaudioMockPlugSnapInfoYaml, nil)
	s.plug = &interfaces.Plug{PlugInfo: plugSnap.Plugs["pulseaudio"]}
	s.slot = &interfaces.Slot{SlotInfo: slotSnap.Slots["pulseaudio"]}
}

func (s *PulseAudioInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "pulseaudio")
}

func (s *PulseAudioInterfaceSuite) TestSanitizeSlot(c *C) {
	err := s.iface.SanitizeSlot(s.slot)
	c.Assert(err, IsNil)
}

func (s *PulseAudioInterfaceSuite) TestSanitizePlug(c *C) {
	err := s.iface.SanitizePlug(s.plug)
	c.Assert(err, IsNil)
}

func (s *PulseAudioInterfaceSuite) TestSecCompOnClassic(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.slot)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 1)
	c.Assert(len(snippets["snap.other.app2"]), Equals, 1)
	c.Check(string(snippets["snap.other.app2"][0]), testutil.Contains, "shmctl\n")
}

func (s *PulseAudioInterfaceSuite) TestSecCompOnAllSnaps(c *C) {
	slotSnap := snaptest.MockInfo(c, pulseaudioMockSlotSnapInfoYaml, nil)
	slot := &interfaces.Slot{SlotInfo: slotSnap.Slots["pulseaudio"]}

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, slot)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, slot)
	c.Assert(err, IsNil)
	snippets := seccompSpec.Snippets()
	c.Assert(len(snippets), Equals, 2)
	c.Assert(len(snippets["snap.pulseaudio.app1"]), Equals, 1)
	c.Check(string(snippets["snap.pulseaudio.app1"][0]), testutil.Contains, "listen\n")
	c.Assert(len(snippets["snap.other.app2"]), Equals, 1)
	c.Check(string(snippets["snap.other.app2"][0]), testutil.Contains, "shmctl\n")
}
