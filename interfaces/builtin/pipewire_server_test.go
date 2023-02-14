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

type PipewireServerInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&PipewireServerInterfaceSuite{
	iface: builtin.MustInterface("pipewire-server"),
})

const pipewireServerMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [pipewire-server]
`

// a pipewire-server slot on a pipewire-server snap (as installed on a core/all-snap system)
const pipewireServerMockCoreSlotSnapInfoYaml = `name: pipewire-server
version: 1.0
apps:
 app1:
  command: foo
  slots: [pipewire-server]
`

// a pipewire-server slot on the core snap (as automatically added on classic)
const pipewireServerMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 pipewire-server:
  interface: pipewire-server
`

func (s *PipewireServerInterfaceSuite) SetUpTest(c *C) {
	// pipewire-server snap with pipewire-server slot on an core/all-snap install.
	snapInfo := snaptest.MockInfo(c, pipewireServerMockCoreSlotSnapInfoYaml, nil)
	s.coreSlotInfo = snapInfo.Slots["pipewire-server"]
	s.coreSlot = interfaces.NewConnectedSlot(s.coreSlotInfo, nil, nil)
	// pipewire-server slot on a core snap in a classic install.
	snapInfo = snaptest.MockInfo(c, pipewireServerMockClassicSlotSnapInfoYaml, nil)
	s.classicSlotInfo = snapInfo.Slots["pipewire-server"]
	s.classicSlot = interfaces.NewConnectedSlot(s.classicSlotInfo, nil, nil)
	// snap with the pipewire-server plug
	snapInfo = snaptest.MockInfo(c, pipewireServerMockPlugSnapInfoYaml, nil)
	s.plugInfo = snapInfo.Plugs["pipewire-server"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *PipewireServerInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "pipewire-server")
}

func (s *PipewireServerInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *PipewireServerInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *PipewireServerInterfaceSuite) TestSecComp(c *C) {
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
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.pipewire-server.app1"})
	c.Assert(spec.SnippetForTag("snap.pipewire-server.app1"), testutil.Contains, "listen\n")
}

func (s *PipewireServerInterfaceSuite) TestSecCompOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &seccomp.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "shmctl\n")

	// connected classic slot to plug
	spec = &seccomp.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &seccomp.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *PipewireServerInterfaceSuite) TestAppArmor(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/user/[0-9]*/ r,\n")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/user/[0-9]*/pipewire-0 rwk,\n")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/user/[0-9]*/pipewire-0.lock rwk,\n")

	// connected core slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.pipewire-server.app1"})
	c.Check(spec.SnippetForTag("snap.pipewire-server.app1"), testutil.Contains, "capability setuid,\n")
}

func (s *PipewireServerInterfaceSuite) TestAppArmorOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/user/[0-9]*/ r,\n")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/user/[0-9]*/pipewire-0 rwk,\n")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/{,var/}run/user/[0-9]*/pipewire-0.lock rwk,\n")

	// connected classic slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *PipewireServerInterfaceSuite) TestUDev(c *C) {
	spec := &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets(), testutil.Contains, `# pipewire-server
KERNEL=="timer", TAG+="snap_pipewire-server_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_pipewire-server_app1", RUN+="%v/snap-device-helper $env{ACTION} snap_pipewire-server_app1 $devpath $major:$minor"`, dirs.DistroLibExecDir))
}

func (s *PipewireServerInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
