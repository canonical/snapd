// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	apparmor_sandbox "github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type UMUSupportInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const umuSupportCoreYaml = `name: core
version: 0
type: os
slots:
  umu-support:
`

const umuSupportConsumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [umu-support]
`

var _ = Suite(&UMUSupportInterfaceSuite{
	iface: builtin.MustInterface("umu-support"),
})

func (s *UMUSupportInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, umuSupportConsumerYaml, nil, "umu-support")
	s.slot, s.slotInfo = MockConnectedSlot(c, umuSupportCoreYaml, nil, "umu-support")
}

func (s *UMUSupportInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "umu-support")
}

func (s *UMUSupportInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *UMUSupportInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *UMUSupportInterfaceSuite) TestAppArmorSpec(c *C) {
	restore := apparmor_sandbox.MockFeatures(nil, nil, []string{"userns"}, nil)
	defer restore()

	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	snippet := spec.SnippetForTag("snap.consumer.app")

	c.Check(snippet, testutil.Contains, "@{PROC}/sys/kernel/overflowuid r,")
	c.Check(snippet, testutil.Contains, "@{PROC}/sys/kernel/overflowgid r,")
	c.Check(snippet, testutil.Contains, "@{PROC}/sys/kernel/sched_autogroup_enabled r,")
	c.Check(snippet, testutil.Contains, "owner @{PROC}/@{pid}/uid_map rw,")
	c.Check(snippet, testutil.Contains, "owner @{PROC}/@{pid}/gid_map rw,")
	c.Check(snippet, testutil.Contains, "owner @{PROC}/@{pid}/setgroups rw,")
	c.Check(snippet, testutil.Contains, "owner @{PROC}/@{pid}/mounts r,")
	c.Check(snippet, testutil.Contains, "owner @{PROC}/@{pid}/mountinfo r,")

	c.Check(snippet, testutil.Contains, "mount,")
	c.Check(snippet, testutil.Contains, "umount,")
	c.Check(snippet, testutil.Contains, "pivot_root,")

	c.Check(snippet, testutil.Contains, "userns,")

	c.Check(snippet, testutil.Contains, "/newroot/** rwkl,")

	c.Check(snippet, testutil.Contains, "mount options=(rw, rbind) /oldroot/usr/ -> /newroot/run/host/usr/,")

	c.Check(snippet, testutil.Contains, "/run/host/usr/lib/** mr,")

	c.Check(snippet, testutil.Contains, "/bindfile* rw,")

	c.Check(snippet, testutil.Contains, "mount options=(rw, rbind) /bindfile* -> /newroot/**,")

	c.Check(snippet, testutil.Contains, "/usr/bin/steam-runtime-launcher-interface-* ixr,")
	c.Check(snippet, testutil.Contains, "/usr/lib/pressure-vessel/from-host/libexec/steam-runtime-tools-*/* ixr,")

	c.Check(snippet, testutil.Contains, "/run/pressure-vessel/** mrw,")
	c.Check(snippet, testutil.Contains, "/var/pressure-vessel/** mrw,")

	c.Check(snippet, testutil.Contains, "owner /home/*/.config/menus/{,**} rw,")
	c.Check(snippet, testutil.Contains, "owner /home/*/.local/share/applications/{,**} rw,")
	c.Check(snippet, testutil.Contains, "owner /home/*/.local/share/desktop-directories/{,**} rw,")
	c.Check(snippet, testutil.Contains, "owner /home/*/.local/share/icons/{,**} rw,")

	c.Check(snippet, testutil.Contains, "/usr/bin/zenity ixr,")
	c.Check(snippet, testutil.Contains, "/run/host/usr/sbin/ldconfig* ixr,")
	c.Check(snippet, testutil.Contains, "/usr/bin/df ixr,")

	c.Check(snippet, testutil.Contains, "capability sys_admin,")
}

func (s *UMUSupportInterfaceSuite) TestSecCompSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := seccomp.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)

	snippet := spec.SnippetForTag("snap.consumer.app")
	c.Check(snippet, Not(Equals), "")
	c.Check(snippet, testutil.Contains, "@unrestricted")
}

func (s *UMUSupportInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *UMUSupportInterfaceSuite) TestUDevSpec(c *C) {
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := udev.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)

	c.Assert(spec.Snippets(), HasLen, 2)
	c.Assert(spec.Snippets()[0], testutil.Contains, `SUBSYSTEM=="usb", ATTRS{idVendor}=="28de", MODE="0660", TAG+="uaccess"`)
	c.Assert(spec.Snippets()[1], testutil.Contains, `KERNEL=="hidraw*", SUBSYSTEM=="hidraw", ATTRS{idVendor}=="114d", ATTRS{idProduct}=="8a12", MODE="0660", TAG+="uaccess"`)
}

func (s *UMUSupportInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, release.OnCoreDesktop)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows UMU launcher to configure pressure-vessel containers`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "umu-support")
}