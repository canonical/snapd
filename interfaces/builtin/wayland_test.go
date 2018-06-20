// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

type WaylandInterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&WaylandInterfaceSuite{
	iface: builtin.MustInterface("wayland"),
})

const waylandConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [wayland]
`

// a wayland slot on a wayland snap (as installed on a core/all-snap system)
const waylandCoreYaml = `name: wayland
version: 0
apps:
 app1:
  slots: [wayland]
`

// a wayland slot on the core snap (as automatically added on classic)
const waylandClassicYaml = `name: core
version: 0
type: os
slots:
 wayland:
  interface: wayland
`

func (s *WaylandInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, waylandConsumerYaml, nil, "wayland")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, waylandCoreYaml, nil, "wayland")
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, waylandClassicYaml, nil, "wayland")
}

func (s *WaylandInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "wayland")
}

func (s *WaylandInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *WaylandInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *WaylandInterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with wayland slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/etc/drirc r,")

	// connected core slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.wayland.app1"})
	c.Assert(spec.SnippetForTag("snap.wayland.app1"), testutil.Contains, "owner /run/user/[0-9]*/snap.consumer/wayland-shared-* rw,")

	// permanent core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.wayland.app1"})
	c.Assert(spec.SnippetForTag("snap.wayland.app1"), testutil.Contains, "capability sys_tty_config,")
}

func (s *WaylandInterfaceSuite) TestAppArmorSpecOnClassic(c *C) {
	// on a classic system with wayland slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/wayland-[0-9]* rw,")

	// connected classic slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *WaylandInterfaceSuite) TestSecCompOnClassic(c *C) {
	// on a classic system with wayland slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.classicSlotInfo)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)
	// No SecComp on Classic
	c.Assert(seccompSpec.SecurityTags(), IsNil)
}

func (s *WaylandInterfaceSuite) TestSecCompOnCore(c *C) {
	// on a core system with wayland slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.wayland.app1"})
	c.Assert(seccompSpec.SnippetForTag("snap.wayland.app1"), testutil.Contains, "listen\n")
}

func (s *WaylandInterfaceSuite) TestUDev(c *C) {
	// on a core system with wayland slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 6)
	c.Assert(spec.Snippets(), testutil.Contains, `# wayland
KERNEL=="event[0-9]*", TAG+="snap_wayland_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# wayland
KERNEL=="mice", TAG+="snap_wayland_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# wayland
KERNEL=="mouse[0-9]*", TAG+="snap_wayland_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# wayland
KERNEL=="ts[0-9]*", TAG+="snap_wayland_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# wayland
KERNEL=="tty[0-9]*", TAG+="snap_wayland_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_wayland_app1", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_wayland_app1 $devpath $major:$minor"`)
	c.Assert(spec.TriggeredSubsystems(), DeepEquals, []string{"input"})

	// on a classic system with wayland slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
	c.Assert(spec.TriggeredSubsystems(), IsNil)
}

func (s *WaylandInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to compositors supporting wayland protocol`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "wayland")
}

func (s *WaylandInterfaceSuite) TestAutoConnect(c *C) {
	// FIXME fix AutoConnect methods to use ConnectedPlug/Slot
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.plugInfo}, &interfaces.Slot{SlotInfo: s.coreSlotInfo}), Equals, true)
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.plugInfo}, &interfaces.Slot{SlotInfo: s.classicSlotInfo}), Equals, true)
}

func (s *WaylandInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
