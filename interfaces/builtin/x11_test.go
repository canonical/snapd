// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

type X11InterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	classicSlotInfo *snap.SlotInfo
	classicSlot     *interfaces.ConnectedSlot
	plugInfo        *snap.PlugInfo
	plug            *interfaces.ConnectedPlug
}

var _ = Suite(&X11InterfaceSuite{
	iface: builtin.MustInterface("x11"),
})

const x11MockPlugSnapInfoYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [x11]
`

// an x11 slot on an x11 snap (as installed on a core/all-snap system)
const x11CoreYaml = `name: x11
version: 0
apps:
 app1:
  slots: [x11]
`

// an x11 slot on the core snap (as automatically added on classic)
const x11ClassicYaml = `name: core
version: 0
type: os
slots:
 x11:
  interface: x11
`

func (s *X11InterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, x11MockPlugSnapInfoYaml, nil, "x11")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, x11CoreYaml, nil, "x11")
	s.classicSlot, s.classicSlotInfo = MockConnectedSlot(c, x11ClassicYaml, nil, "x11")
}

func (s *X11InterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "x11")
}

func (s *X11InterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.classicSlotInfo), IsNil)
}

func (s *X11InterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *X11InterfaceSuite) TestAppArmorSpec(c *C) {
	// on a core system with x11 slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "fontconfig")

	// connected core slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.x11.app1"})
	c.Assert(spec.SnippetForTag("snap.x11.app1"), testutil.Contains, `peer=(label="snap.consumer.app"),`)

	// permanent core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.x11.app1"})
	c.Assert(spec.SnippetForTag("snap.x11.app1"), testutil.Contains, "capability sys_tty_config,")
}

func (s *X11InterfaceSuite) TestAppArmorSpecOnClassic(c *C) {
	// on a classic system with x11 slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/.Xauthority r,")

	// connected classic slot to plug
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *X11InterfaceSuite) TestSecCompOnClassic(c *C) {
	// on a classic system with x11 slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.classicSlotInfo)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)

	// app snap has additional seccomp rules
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(seccompSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *X11InterfaceSuite) TestSecCompOnCore(c *C) {
	// on a core system with x11 slot coming from a snap.
	restore := release.MockOnClassic(false)
	defer restore()

	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)

	// both app and x11 have secomp rules set
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app", "snap.x11.app1"})
	c.Assert(seccompSpec.SnippetForTag("snap.x11.app1"), testutil.Contains, "listen\n")
	c.Assert(seccompSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *X11InterfaceSuite) TestUDev(c *C) {
	// on a core system with x11 slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 6)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="event[0-9]*", TAG+="snap_x11_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="mice", TAG+="snap_x11_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="mouse[0-9]*", TAG+="snap_x11_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="ts[0-9]*", TAG+="snap_x11_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="tty[0-9]*", TAG+="snap_x11_app1"`)
	c.Assert(spec.Snippets(), testutil.Contains, `TAG=="snap_x11_app1", RUN+="/usr/lib/snapd/snap-device-helper $env{ACTION} snap_x11_app1 $devpath $major:$minor"`)
	c.Assert(spec.TriggeredSubsystems(), DeepEquals, []string{"input"})

	// on a classic system with x11 slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = &udev.Specification{}
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
	c.Assert(spec.TriggeredSubsystems(), IsNil)
}

func (s *X11InterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows interacting with or running as an X11 server`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "x11")
}

func (s *X11InterfaceSuite) TestAutoConnect(c *C) {
	// FIXME fix AutoConnect methods to use ConnectedPlug/Slot
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.plugInfo}, &interfaces.Slot{SlotInfo: s.coreSlotInfo}), Equals, true)
	c.Assert(s.iface.AutoConnect(&interfaces.Plug{PlugInfo: s.plugInfo}, &interfaces.Slot{SlotInfo: s.classicSlotInfo}), Equals, true)
}

func (s *X11InterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
