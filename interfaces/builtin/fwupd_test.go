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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/dbus"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/interfaces/seccomp"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type FwupdInterfaceSuite struct {
	iface        interfaces.Interface
	appSlotInfo  *snap.SlotInfo
	appSlot      *interfaces.ConnectedSlot
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

const mockPlugSnapInfoYaml = `name: uefi-fw-tools
version: 1.0
apps:
 app:
  command: foo
  plugs: [fwupd]
`

const mockAppSlotSnapInfoYaml = `name: uefi-fw-tools
version: 1.0
apps:
 app2:
  command: foo
  slots: [fwupd]
`

const mockCoreSlotSnapInfoYaml = `name: core
type: os
version: 1.0
slots:
  fwupd:
`

var _ = Suite(&FwupdInterfaceSuite{
	iface: builtin.MustInterface("fwupd"),
})

func (s *FwupdInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, mockPlugSnapInfoYaml, nil, "fwupd")
	s.appSlot, s.appSlotInfo = MockConnectedSlot(c, mockAppSlotSnapInfoYaml, nil, "fwupd")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, mockCoreSlotSnapInfoYaml, nil, "fwupd")
}

func (s *FwupdInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "fwupd")
}

// The label glob when all apps are bound to the fwupd slot
func (s *FwupdInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelAll(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	slot := &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "uefi-fw-tools",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2},
		},
		Name:      "fwupd",
		Interface: "fwupd",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}

	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, interfaces.NewConnectedSlot(slot, nil, nil))
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.uefi-fw-tools.app"), testutil.Contains, `peer=(label="snap.uefi-fw-tools.*"),`)
}

// The label uses alternation when some, but not all, apps is bound to the fwupd slot
func (s *FwupdInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelSome(c *C) {
	app1 := &snap.AppInfo{Name: "app1"}
	app2 := &snap.AppInfo{Name: "app2"}
	app3 := &snap.AppInfo{Name: "app3"}
	slot := &snap.SlotInfo{
		Snap: &snap.Info{
			SuggestedName: "uefi-fw-tools",
			Apps:          map[string]*snap.AppInfo{"app1": app1, "app2": app2, "app3": app3},
		},
		Name:      "fwupd",
		Interface: "fwupd",
		Apps:      map[string]*snap.AppInfo{"app1": app1, "app2": app2},
	}

	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, interfaces.NewConnectedSlot(slot, nil, nil))
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.uefi-fw-tools.app"), testutil.Contains, `peer=(label="snap.uefi-fw-tools.{app1,app2}"),`)
}

// The label uses short form when exactly one app is bound to the fwupd slot
func (s *FwupdInterfaceSuite) TestConnectedPlugSnippetUsesSlotLabelOne(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.appSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.uefi-fw-tools.app"), testutil.Contains, `peer=(label="snap.uefi-fw-tools.app2"),`)
}

func (s *FwupdInterfaceSuite) TestConnectedPlugSnippetToImplicitSlot(c *C) {
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app"})
	c.Assert(apparmorSpec.SnippetForTag("snap.uefi-fw-tools.app"), testutil.Contains, `peer=(label=unconfined),`)
}

func (s *FwupdInterfaceSuite) TestUsedSecuritySystems(c *C) {
	// connected plugs have a non-nil security snippet for apparmor
	apparmorSpec := &apparmor.Specification{}
	err := apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.appSlot)
	c.Assert(err, IsNil)
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.appSlot)
	c.Assert(err, IsNil)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.appSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app", "snap.uefi-fw-tools.app2"})

	dbusSpec := &dbus.Specification{}
	err = dbusSpec.AddPermanentSlot(s.iface, s.appSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 1)

	// UpdateNS rules are only applied if the permanent slot is provided by
	// an app snap
	updateNS := apparmorSpec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Read-write access to /boot\n")

	// When connecting to the implicit slot on Classic systems, we
	// don't generate slot-side AppArmor rules.
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedPlug(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app"})
	apparmorSpec = &apparmor.Specification{}
	err = apparmorSpec.AddConnectedSlot(s.iface, s.plug, s.coreSlot)
	c.Assert(err, IsNil)
	err = apparmorSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(apparmorSpec.SecurityTags(), HasLen, 0)

	// The same is true for D-Bus rules
	dbusSpec = &dbus.Specification{}
	err = dbusSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)
}

func (s *FwupdInterfaceSuite) TestMountPermanentSlot(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)

	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/boot"), 0777), IsNil)

	// If the permanent slot is provided by an app snap, the boot partition
	// should be bind mounted from the host system if they exist.
	mountSpec := &mount.Specification{}
	c.Assert(mountSpec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)

	entries := mountSpec.MountEntries()
	c.Assert(entries, HasLen, 1)

	const hostfs = "/var/lib/snapd/hostfs"
	c.Check(entries[0].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/boot"))
	c.Check(entries[0].Dir, Equals, "/boot")
	c.Check(entries[0].Options, DeepEquals, []string{"rbind", "rw"})
}

func (s *FwupdInterfaceSuite) TestPermanentSlotSnippetSecComp(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddPermanentSlot(s.iface, s.appSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app2"})
	c.Check(seccompSpec.SnippetForTag("snap.uefi-fw-tools.app2"), testutil.Contains, "bind\n")

	// On classic systems, fwupd is an implicit slot
	seccompSpec = &seccomp.Specification{}
	err = seccompSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), HasLen, 0)
}

func (s *FwupdInterfaceSuite) TestPermanentSlotDBus(c *C) {
	dbusSpec := &dbus.Specification{}
	err := dbusSpec.AddPermanentSlot(s.iface, s.appSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app2"})
	c.Assert(dbusSpec.SnippetForTag("snap.uefi-fw-tools.app2"), testutil.Contains, `<allow own="org.freedesktop.fwupd"/>`)

	// The implicit slot found on classic systems does not generate any rules
	dbusSpec = &dbus.Specification{}
	err = dbusSpec.AddPermanentSlot(s.iface, s.coreSlotInfo)
	c.Assert(err, IsNil)
	c.Assert(dbusSpec.SecurityTags(), HasLen, 0)
}

func (s *FwupdInterfaceSuite) TestConnectedPlugSnippetSecComp(c *C) {
	seccompSpec := &seccomp.Specification{}
	err := seccompSpec.AddConnectedPlug(s.iface, s.plug, s.appSlot)
	c.Assert(err, IsNil)
	c.Assert(seccompSpec.SecurityTags(), DeepEquals, []string{"snap.uefi-fw-tools.app"})
	c.Check(seccompSpec.SnippetForTag("snap.uefi-fw-tools.app"), testutil.Contains, "bind\n")
}

func (s *FwupdInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
