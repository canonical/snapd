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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type X11InterfaceSuite struct {
	iface           interfaces.Interface
	coreSlotInfo    *snap.SlotInfo
	coreSlot        *interfaces.ConnectedSlot
	corePlugInfo    *snap.PlugInfo
	corePlug        *interfaces.ConnectedPlug
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
 app:
  slots: [x11-provider]
  plugs: [x11-consumer]
plugs:
  x11-consumer:
    interface: x11
slots:
  x11-provider:
    interface: x11
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
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, x11CoreYaml, nil, "x11-provider")
	s.corePlug, s.corePlugInfo = MockConnectedPlug(c, x11CoreYaml, nil, "x11-consumer")
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

func (s *X11InterfaceSuite) TestMountSpec(c *C) {
	// case A: x11 slot is provided by the system
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{{
		Name:    "/var/lib/snapd/hostfs/tmp/.X11-unix",
		Dir:     "/tmp/.X11-unix",
		Options: []string{"bind", "ro"},
	}})
	c.Assert(spec.UserMountEntries(), HasLen, 0)

	// case B: x11 slot is provided by another snap on the system
	spec = &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.MountEntries(), DeepEquals, []osutil.MountEntry{{
		Name:    "/var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.x11/tmp/.X11-unix",
		Dir:     "/tmp/.X11-unix",
		Options: []string{"bind", "ro"},
	}})
	c.Assert(spec.UserMountEntries(), HasLen, 0)

	// case C: x11 slot is both provided and consumed by a snap on the system.
	spec = &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.corePlug, s.coreSlot), IsNil)
	c.Assert(spec.MountEntries(), HasLen, 0)
	c.Assert(spec.UserMountEntries(), HasLen, 0)
}

func (s *X11InterfaceSuite) TestAppArmorSpec(c *C) {
	// case A: x11 slot is provided by the classic system
	restore := release.MockOnClassic(true)
	defer restore()

	// Plug side connection permissions
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "fontconfig")
	c.Assert(spec.UpdateNS(), HasLen, 1)
	c.Assert(spec.UpdateNS()[0], testutil.Contains, `mount options=(rw, bind) /var/lib/snapd/hostfs/tmp/.X11-unix/ -> /tmp/.X11-unix/,`)

	// case B: x11 slot is provided by another snap on the system
	restore = release.MockOnClassic(false)
	defer restore()

	// Plug side connection permissions
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "fontconfig")
	c.Assert(spec.UpdateNS(), HasLen, 1)
	c.Assert(spec.UpdateNS()[0], testutil.Contains, `mount options=(rw, bind) /var/lib/snapd/hostfs/tmp/snap-private-tmp/snap.x11/tmp/.X11-unix/ -> /tmp/.X11-unix/,`)

	// Slot side connection permissions
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.x11.app"})
	c.Assert(spec.SnippetForTag("snap.x11.app"), testutil.Contains, `peer=(label="snap.consumer.app"),`)
	c.Assert(spec.UpdateNS(), HasLen, 0)

	// Slot side permantent permissions
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.x11.app"})
	c.Assert(spec.SnippetForTag("snap.x11.app"), testutil.Contains, "capability sys_tty_config,")
	c.Assert(spec.UpdateNS(), HasLen, 0)

	// case C: x11 slot is both provided and consumed by a snap on the system.
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.corePlug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.corePlug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.x11.app"})
	c.Assert(spec.SnippetForTag("snap.x11.app"), testutil.Contains, "fontconfig")
	// Self-connection does not need bind mounts, so no additional permissions are provided to snap-update-ns.
	c.Assert(spec.UpdateNS(), HasLen, 0)
}

func (s *X11InterfaceSuite) TestAppArmorSpecOnClassic(c *C) {
	// on a classic system with x11 slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	// connected plug to classic slot
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "owner /run/user/[0-9]*/.Xauthority r,")

	// connected classic slot to plug
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.classicSlot.Snap(), nil))
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.classicSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)

	// permanent classic slot
	spec = apparmor.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *X11InterfaceSuite) TestSecCompOnClassic(c *C) {
	// on a classic system with x11 slot coming from the core snap.
	restore := release.MockOnClassic(true)
	defer restore()

	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.classicSlot)
	c.Assert(err, IsNil)

	// app snap has additional seccomp rules
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(seccompSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *X11InterfaceSuite) TestSecCompOnCore(c *C) {
	// on a core system with x11 slot coming from a snap.
	restore := release.MockOnClassic(false)
	defer restore()

	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil))
	err := seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)

	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.x11.app"})
	c.Assert(seccompSpec.SnippetForTag("snap.x11.app"), testutil.Contains, "listen\n")

	seccompSpec = seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)

	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(seccompSpec.SnippetForTag("snap.consumer.app"), testutil.Contains, "bind\n")
}

func (s *X11InterfaceSuite) TestUDev(c *C) {
	// on a core system with x11 slot coming from a regular app snap.
	restore := release.MockOnClassic(false)
	defer restore()

	spec := udev.NewSpecification(interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 6)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="event[0-9]*", TAG+="snap_x11_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="mice", TAG+="snap_x11_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="mouse[0-9]*", TAG+="snap_x11_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="ts[0-9]*", TAG+="snap_x11_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, `# x11
KERNEL=="tty[0-9]*", TAG+="snap_x11_app"`)
	c.Assert(spec.Snippets(), testutil.Contains, fmt.Sprintf(`TAG=="snap_x11_app", SUBSYSTEM!="module", SUBSYSTEM!="subsystem", RUN+="%v/snap-device-helper $env{ACTION} snap_x11_app $devpath $major:$minor"`, dirs.DistroLibExecDir))
	c.Assert(spec.TriggeredSubsystems(), DeepEquals, []string{"input"})

	// on a classic system with x11 slot coming from the core snap.
	restore = release.MockOnClassic(true)
	defer restore()

	spec = udev.NewSpecification(interfaces.NewSnapAppSet(s.classicSlotInfo.Snap, nil))
	c.Assert(spec.AddPermanentSlot(s.iface, s.classicSlotInfo), IsNil)
	c.Assert(spec.Snippets(), HasLen, 0)
	c.Assert(spec.TriggeredSubsystems(), IsNil)
}

func (s *X11InterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows interacting with or running as an X11 server`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "x11")
	c.Assert(si.AffectsPlugOnRefresh, Equals, true)
}

func (s *X11InterfaceSuite) TestAutoConnect(c *C) {
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.coreSlotInfo), Equals, true)
	c.Assert(s.iface.AutoConnect(s.plugInfo, s.classicSlotInfo), Equals, true)
}

func (s *X11InterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
