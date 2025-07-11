// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type pipewireInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&pipewireInterfaceSuite{
	iface: builtin.MustInterface("pipewire"),
})

const pipewireMockPlugSnapInfoYaml = `name: consumer
version: 1.0
apps:
 app:
  command: foo
  plugs: [pipewire]
`

// a pipewire slot on a pipewire snap (as installed on a core/all-snap system)
const pipewireMockCoreSlotSnapInfoYaml = `name: pipewire
version: 1.0
apps:
 app1:
  command: foo
  slots: [pipewire]
`

// a pipewire slot on the core snap (as automatically added on classic)
const pipewireMockClassicSlotSnapInfoYaml = `name: core
version: 0
type: os
slots:
 pipewire:
  interface: pipewire
`

func (s *pipewireInterfaceSuite) SetUpTest(c *C) {
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, pipewireMockCoreSlotSnapInfoYaml, nil, "pipewire")
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, pipewireMockClassicSlotSnapInfoYaml, nil, "pipewire")
	s.plug, s.plugInfo = MockConnectedPlug(c, pipewireMockPlugSnapInfoYaml, nil, "pipewire")
}

func (s *pipewireInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "pipewire")
}

func (s *pipewireInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *pipewireInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *pipewireInterfaceSuite) TestSecComp(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := seccomp.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "shmctl\n")

	// connected core slot to plug
	spec = seccomp.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = seccomp.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.pipewire.app1"})
	c.Assert(spec.SnippetForTag("snap.pipewire.app1"), testutil.Contains, "listen\n")
}

func (s *pipewireInterfaceSuite) TestSecCompOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := seccomp.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "shmctl\n")

	// connected classic slot to plug
	spec = seccomp.NewSpecification(s.classicSlot.AppSet())
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = seccomp.NewSpecification(s.classicSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *pipewireInterfaceSuite) TestAppArmor(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/snap.pipewire/pipewire-[0-9] rw,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/pipewire-[0-9] rw,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /var/snap/pipewire/common/pipewire-[0-9] rw,\n")

	// connected core slot to plug
	spec = apparmor.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent core slot
	spec = apparmor.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.pipewire.app1"})
	c.Assert(spec.SnippetForTag("snap.pipewire.app1"), testutil.Contains, "owner /run/user/[0-9]*/pipewire-[0-9] rwk,\n")
	c.Assert(spec.SnippetForTag("snap.pipewire.app1"), testutil.Contains, "owner /run/user/[0-9]*/pipewire-[0-9]-manager rwk,\n")
}

func (s *pipewireInterfaceSuite) TestAppArmorOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := apparmor.NewSpecification(s.plug.AppSet())
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/pipewire-[0-9] rw,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "owner /run/user/[0-9]*/snap.pipewire/pipewire-[0-9] rw,\n")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), Not(testutil.Contains), "owner /var/snap/pipewire/common/pipewire-[0-9] r,\n")

	// connected classic slot to plug
	spec = apparmor.NewSpecification(s.classicSlot.AppSet())
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = apparmor.NewSpecification(s.classicSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	c.Assert(spec.SnippetForTag("snap.pipewire.app1"), Not(testutil.Contains), "owner /run/user/[0-9]*/pipewire-[0-9] rwk,\n")
	c.Assert(spec.SnippetForTag("snap.pipewire.app1"), Not(testutil.Contains), "owner /var/snap/pipewire/common/pipewire-[0-9] r,\n")
}

func (s *pipewireInterfaceSuite) TestUDev(c *C) {
	spec := udev.NewSpecification(s.coreSlot.AppSet())
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
}

func (s *pipewireInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
