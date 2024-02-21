// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type AppstreamMetadataInterfaceSuite struct {
	iface    interfaces.Interface
	slot     *interfaces.ConnectedSlot
	slotInfo *snap.SlotInfo
	plug     *interfaces.ConnectedPlug
	plugInfo *snap.PlugInfo
}

var _ = Suite(&AppstreamMetadataInterfaceSuite{
	iface: builtin.MustInterface("appstream-metadata"),
})

func (s *AppstreamMetadataInterfaceSuite) SetUpTest(c *C) {
	const coreYaml = `name: core
version: 0
type: os
slots:
  appstream-metadata:
    interface: appstream-metadata
`
	s.slot, s.slotInfo = MockConnectedSlot(c, coreYaml, nil, "appstream-metadata")

	const consumerYaml = `name: consumer
version: 0
apps:
  app:
    plugs: [appstream-metadata]
`
	s.plug, s.plugInfo = MockConnectedPlug(c, consumerYaml, nil, "appstream-metadata")
}

func (s *AppstreamMetadataInterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *AppstreamMetadataInterfaceSuite) TestName(c *C) {
	c.Check(s.iface.Name(), Equals, "appstream-metadata")
}

func (s *AppstreamMetadataInterfaceSuite) TestSanitize(c *C) {
	c.Check(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
	c.Check(interfaces.BeforePrepareSlot(s.iface, s.slotInfo), IsNil)
}

func (s *AppstreamMetadataInterfaceSuite) TestAppArmorConnectedPlug(c *C) {
	spec := apparmor.NewSpecification(interfaces.NewSnapAppSet(s.plug.Snap()))
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 1)
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, `/var/cache/{app-info,swcatalog}/** r,`)
	c.Check(spec.UpdateNS(), testutil.Contains, "  # Read-only access to /usr/share/metainfo\n")
}

func (s *AppstreamMetadataInterfaceSuite) TestMountConnectedPlug(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)

	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/metainfo"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/appdata"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/swcatalog"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/swcatalog"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/lib/swcatalog"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/lib/apt/lists"), 0777), IsNil)

	c.Assert(os.Symlink("swcatalog", filepath.Join(tmpdir, "/usr/share/app-info")), IsNil)
	c.Assert(os.Symlink("swcatalog", filepath.Join(tmpdir, "/var/cache/app-info")), IsNil)
	c.Assert(os.Symlink("swcatalog", filepath.Join(tmpdir, "/var/lib/app-info")), IsNil)

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.slot), IsNil)
	entries := spec.MountEntries()
	c.Assert(entries, HasLen, 9)

	const hostfs = "/var/lib/snapd/hostfs"
	c.Check(entries[0].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/usr/share/metainfo"))
	c.Check(entries[0].Dir, Equals, "/usr/share/metainfo")
	c.Check(entries[0].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[1].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/usr/share/appdata"))
	c.Check(entries[1].Dir, Equals, "/usr/share/appdata")
	c.Check(entries[1].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[2].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/usr/share/app-info"))
	c.Check(entries[2].Dir, Equals, "/usr/share/app-info")
	c.Check(entries[2].Options, DeepEquals, []string{"x-snapd.kind=symlink", "x-snapd.symlink=swcatalog"})
	c.Check(entries[3].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/usr/share/swcatalog"))
	c.Check(entries[3].Dir, Equals, "/usr/share/swcatalog")
	c.Check(entries[3].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[4].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/var/cache/app-info"))
	c.Check(entries[4].Dir, Equals, "/var/cache/app-info")
	c.Check(entries[4].Options, DeepEquals, []string{"x-snapd.kind=symlink", "x-snapd.symlink=swcatalog"})
	c.Check(entries[5].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/var/cache/swcatalog"))
	c.Check(entries[5].Dir, Equals, "/var/cache/swcatalog")
	c.Check(entries[5].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[6].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/var/lib/app-info"))
	c.Check(entries[6].Dir, Equals, "/var/lib/app-info")
	c.Check(entries[6].Options, DeepEquals, []string{"x-snapd.kind=symlink", "x-snapd.symlink=swcatalog"})
	c.Check(entries[7].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/var/lib/swcatalog"))
	c.Check(entries[7].Dir, Equals, "/var/lib/swcatalog")
	c.Check(entries[7].Options, DeepEquals, []string{"bind", "ro"})
	c.Check(entries[8].Name, Equals, filepath.Join(hostfs, dirs.GlobalRootDir, "/var/lib/apt/lists"))
	c.Check(entries[8].Dir, Equals, "/var/lib/apt/lists")
	c.Check(entries[8].Options, DeepEquals, []string{"bind", "ro"})
}

func (s *AppstreamMetadataInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Check(si.ImplicitOnCore, Equals, false)
	c.Check(si.ImplicitOnClassic, Equals, true)
	c.Check(si.Summary, Equals, "allows access to AppStream metadata")
	c.Check(si.BaseDeclarationSlots, testutil.Contains, "appstream-metadata")
	c.Check(si.AffectsPlugOnRefresh, Equals, true)
}

func (s *AppstreamMetadataInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}
