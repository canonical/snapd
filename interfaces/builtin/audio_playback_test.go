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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type AudioPlaybackInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&AudioPlaybackInterfaceSuite{
	iface: builtin.MustInterface("audio-playback"),
})

const audioPlaybackMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [audio-playback]
`

// a audio-playback slot on a audio-playback snap (as installed on a core/all-snap system)
const audioPlaybackMockCoreSlotSnapInfoYaml = `name: audio-playback
version: 1.0
apps:
 app1:
  command: foo
  slots: [audio-playback]
`

// a audio-playback slot on the core snap (as automatically added on classic)
const audioPlaybackMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 audio-playback:
  interface: audio-playback
`

func (s *AudioPlaybackInterfaceSuite) SetUpTest(c *C) {
	// audio-playback snap with audio-playback slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, audioPlaybackMockCoreSlotSnapInfoYaml, nil)
	s.coreSlotInfo = snapInfo.Slots["audio-playback"]
	s.coreSlot = interfaces.NewConnectedSlot(s.coreSlotInfo, nil, nil)
	// audio-playback slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, audioPlaybackMockClassicSlotSnapInfoYaml, nil)
	s.classicSlotInfo = snapInfo.Slots["audio-playback"]
	s.classicSlot = interfaces.NewConnectedSlot(s.classicSlotInfo, nil, nil)
	// snap with the audio-playback plug
	snapInfo = snaptest.MockInfo(c, audioPlaybackMockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["audio-playback"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *AudioPlaybackInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "audio-playback")
}

func (s *AudioPlaybackInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *AudioPlaybackInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *AudioPlaybackInterfaceSuite) TestSecComp(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "shmctl\n")

	// connected core slot to plug
	spec = &seccomp.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = &seccomp.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.audio-playback.app1"})
	c.Assert(spec.SnippetForTag("snap.audio-playback.app1"), testutil.Contains, "listen\n")
}

func (s *AudioPlaybackInterfaceSuite) TestSecCompOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "shmctl\n")

	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "owner /run/user/[0-9]*/snap.audio-playback/pulse/ r,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "owner /run/user/[0-9]*/snap.audio-playback/pulse/native rwk,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "owner /run/user/[0-9]*/snap.audio-playback/pulse/pid r,\n")

	// connected classic slot to plug
	spec = &seccomp.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &seccomp.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	c.Assert(spec.SnippetForTag("snap.audio-playback.app1"), Not(testutil.Contains), "owner /run/user/[0-9]*/pipewire-[0-9] rwk,\n")
	c.Check(spec.SnippetForTag("snap.audio-playback.app1"), Not(testutil.Contains), "/etc/pulse/ r,\n")
	c.Check(spec.SnippetForTag("snap.audio-playback.app1"), Not(testutil.Contains), "/etc/pulse/** r,\n")
}

func (s *AudioPlaybackInterfaceSuite) TestAppArmor(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{run,dev}/shm/pulse-shm-* mrwk,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/snap.audio-playback/pulse/ r,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/snap.audio-playback/pulse/native rwk,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/snap.audio-playback/pulse/pid r,\n")

	// connected core slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.audio-playback.app1"})
	c.Check(spec.SnippetForTag("snap.audio-playback.app1"), testutil.Contains, "capability setuid,\n")
	c.Assert(spec.SnippetForTag("snap.audio-playback.app1"), testutil.Contains, "owner /run/user/[0-9]*/pipewire-[0-9] rwk,\n")
	c.Check(spec.SnippetForTag("snap.audio-playback.app1"), testutil.Contains, "/etc/pulse/ r,\n")
	c.Check(spec.SnippetForTag("snap.audio-playback.app1"), testutil.Contains, "/etc/pulse/** r,\n")
}

func (s *AudioPlaybackInterfaceSuite) TestAppArmorOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{run,dev}/shm/pulse-shm-* mrwk,\n")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/etc/pulse/ r,\n")

	// connected classic slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *AudioPlaybackInterfaceSuite) TestUDev(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 4)
	c.Assert(spec.Snippets(), testutil.Contains, `# audio-playback
KERNEL=="controlC[0-9]*", TAG+="snap_audio-playback_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# audio-playback
KERNEL=="pcmC[0-9]*D[0-9]*[cp]", TAG+="snap_audio-playback_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# audio-playback
KERNEL=="timer", TAG+="snap_audio-playback_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_audio-playback_app1", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper snap_audio-playback_app1"`, dirs.DistroLibExecDir))
}

func (s *AudioPlaybackInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
