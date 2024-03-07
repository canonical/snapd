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
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type SystemObserveInterfaceSuite struct {
	iface    interfaces.Interface
	slotInfo *snap.SlotInfo
	slot     *interfaces.ConnectedSlot
	plugInfo *snap.PlugInfo
	plug     *interfaces.ConnectedPlug
}

const sysobsMockPlugSnapInfoYaml = `name: other
version: 1.0
apps:
 app2:
  command: foo
  plugs: [system-observe]
`

var _ = Suite(&SystemObserveInterfaceSuite{
	iface: builtin.MustInterface("system-observe"),
})

func (s *SystemObserveInterfaceSuite) SetUpTest(c *C) {
	s.slotInfo = &snap.SlotInfo{
		Snap:      &snap.Info{SuggestedName: "core", SnapType: snap.TypeOS},
		Name:      "system-observe",
		Interface: "system-observe",
	}
	s.slot = interfaces.NewConnectedSlot(s.slotInfo, nil, nil)
	plugSnap := snaptest.MockInfo(c, sysobsMockPlugSnapInfoYaml, nil)
	s.plugInfo = plugSnap.Plugs["system-observe"]
	s.plug = interfaces.NewConnectedPlug(s.plugInfo, nil, nil)
}

func (s *SystemObserveInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "system-observe")
}

func (s *SystemObserveInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *SystemObserveInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *SystemObserveInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "ptrace")
	c.Assert(apparmorSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "@{PROC}/partitions r,")

	updateNS := apparmorSpec.UpdateNS()
	expectedUpdateNS := `  # Read-only access to /boot
  mount options=(bind,rw) /var/lib/snapd/hostfs/boot/ -> /boot/,
  mount options=(bind,remount,ro) -> /boot/,
  umount /boot/,
`
	c.Assert(strings.Join(updateNS[:], "\n"), Equals, expectedUpdateNS)

	// connected plugs have a non-nil security snippet for seccomp
	seccompSpec := seccomp.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap(), nil))
	err = seccompSpec.AddConnectedPlug(s.iface, s.plug, s.slot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.other.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.other.app2"), testutil.Contains, "ptrace\n")
}

func (s *SystemObserveInterfaceSuite) TestMountPermanentPlug(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)

	// Create a /boot/config-* file so that the interface will generate a bind
	// mount for it
	fakeBootDir := filepath.Join(tmpdir, "/boot")
	c.Assert(os.MkdirAll(fakeBootDir, 0777), IsNil)
	file, err := os.OpenFile(filepath.Join(fakeBootDir, "config-5.10"), os.O_CREATE, 0644)
	c.Assert(err, IsNil)
	c.Assert(file.Close(), IsNil)

	mountSpec := &mount.Specification{}
	c.Assert(mountSpec.AddPermanentPlug(s.iface, s.plugInfo), IsNil)

	entries := mountSpec.MountEntries()
	c.Assert(entries, HasLen, 1)

	const hostfs = "/var/lib/snapd/hostfs"
	c.Check(entries[0].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/boot"))
	c.Check(entries[0].Dir, Equals, "/boot")
	c.Check(entries[0].Options, DeepEquals, []string{"bind", "ro"})
}

func (s *SystemObserveInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
