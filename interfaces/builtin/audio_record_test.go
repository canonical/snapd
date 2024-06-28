// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type AudioRecordInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&AudioRecordInterfaceSuite{
	iface: builtin.MustInterface("audio-record"),
})

const audioRecordMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [audio-record]
`

// a audio-record slot on a audio-record snap (as installed on a core/all-snap system)
const audioRecordMockCoreSlotSnapInfoYaml = `name: audio-record
version: 1.0
apps:
 app1:
  command: foo
  slots: [audio-record]
`

// a audio-record slot on the core snap (as automatically added on classic)
const audioRecordMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 audio-record:
  interface: audio-record
`

func (s *AudioRecordInterfaceSuite) SetUpTest(c *C) {
	// audio-record snap with audio-record slot on an core/all-snap install.
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, audioRecordMockCoreSlotSnapInfoYaml, nil, "audio-record")
	// audio-record slot on a core snap in a classic install.
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, audioRecordMockClassicSlotSnapInfoYaml, nil, "audio-record")
	// snap with the audio-record plug
	s.plug, s.plugInfo = MockConnectedPlug(c, audioRecordMockPlugSnapInfoYaml, nil, "audio-record")
}

func (s *AudioRecordInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "audio-record")
}

func (s *AudioRecordInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *AudioRecordInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *AudioRecordInterfaceSuite) TestAppArmor(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Access for communication with audio recording service done via\n")

	// connected core slot to plug
	spec = apparmor.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core clot
	spec = apparmor.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AudioRecordInterfaceSuite) TestAppArmorOnClassic(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to classic slot
	spec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Access for communication with audio recording service done via\n")

	// connected classic slot to plug
	spec = apparmor.NewSpecification(s.classicSlot.AppSet())
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = apparmor.NewSpecification(s.classicSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AudioRecordInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
