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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type DesktopInterfaceSuite struct {
	iface        interfaces.Interface
	coreSlotInfo *snap.SlotInfo
	coreSlot     *interfaces.ConnectedSlot
	plugInfo     *snap.PlugInfo
	plug         *interfaces.ConnectedPlug
}

var _ = Suite(&DesktopInterfaceSuite{
	iface: builtin.MustInterface("desktop"),
})

const desktopConsumerYaml = `name: consumer
version: 0
apps:
 app:
  plugs: [desktop]
`

const desktopCoreYaml = `name: core
version: 0
type: os
slots:
  desktop:
`

func (s *DesktopInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, desktopConsumerYaml, nil, "desktop")
	s.coreSlot, s.coreSlotInfo = MockConnectedSlot(c, desktopCoreYaml, nil, "desktop")
}

func (s *DesktopInterfaceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *DesktopInterfaceSuite) TestName(c *C) {
	c.Assert(s.iface.Name(), Equals, "desktop")
}

func (s *DesktopInterfaceSuite) TestSanitizeSlot(c *C) {
	c.Assert(interfaces.BeforePrepareSlot(s.iface, s.coreSlotInfo), IsNil)
}

func (s *DesktopInterfaceSuite) TestSanitizePlug(c *C) {
	c.Assert(interfaces.BeforePreparePlug(s.iface, s.plugInfo), IsNil)
}

func (s *DesktopInterfaceSuite) TestAppArmorSpec(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/local/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/fontconfig"), 0777), IsNil)
	restore := release.MockOnClassic(false)
	defer restore()

	// connected plug to core slot
	spec := &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access basic graphical desktop resources")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/fonts>")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/etc/gtk-3.0/settings.ini r,")
	c.Assert(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow access to xdg-desktop-portal and xdg-document-portal")

	// On an all-snaps system, the only UpdateNS rule is for the
	// document portal.
	updateNS := spec.UpdateNS()
	profile0 := `  # Mount the document portal
  mount options=(bind) /run/user/[0-9]*/doc/by-app/snap.consumer/ -> /run/user/[0-9]*/doc/,
  umount /run/user/[0-9]*/doc/,

`
	c.Assert(strings.Join(updateNS, ""), Equals, profile0)

	// On a classic system, there are UpdateNS rules for the host
	// system font mounts
	restore = release.MockOnClassic(true)
	defer restore()
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	updateNS = spec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Mount the document portal\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /usr/share/fonts\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /usr/local/share/fonts\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /var/cache/fontconfig\n")

	// connected plug to core slot
	spec = &apparmor.Specification{}
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *DesktopInterfaceSuite) TestMountSpec(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/local/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/fontconfig"), 0777), IsNil)

	restore := release.MockOnClassic(false)
	defer restore()

	// On all-snaps systems, the font related mount entries are missing
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
	c.Check(spec.MountEntries(), HasLen, 0)

	entries := spec.UserMountEntries()
	c.Check(entries, HasLen, 1)
	c.Check(entries[0].Name, Equals, "$XDG_RUNTIME_DIR/doc/by-app/snap.consumer")
	c.Check(entries[0].Dir, Equals, "$XDG_RUNTIME_DIR/doc")
	c.Check(entries[0].Options, DeepEquals, []string{"bind", "rw", "x-snapd.ignore-missing"})

	// On classic systems, a number of font related directories
	// are bind mounted from the host system if they exist.
	restore = release.MockOnClassic(true)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()
	spec = &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)

	entries = spec.MountEntries()
	c.Assert(entries, HasLen, 3)

	const hostfs = "/var/lib/snapd/hostfs"
	c.Check(entries[0].Name, Equals, hostfs+dirs.SystemFontsDir)
	c.Check(entries[0].Dir, Equals, "/usr/share/fonts")
	c.Check(entries[0].Options, DeepEquals, []string{"bind", "ro"})

	c.Check(entries[1].Name, Equals, hostfs+dirs.SystemLocalFontsDir)
	c.Check(entries[1].Dir, Equals, "/usr/local/share/fonts")
	c.Check(entries[1].Options, DeepEquals, []string{"bind", "ro"})

	c.Check(entries[2].Name, Equals, hostfs+dirs.SystemFontconfigCacheDirs[0])
	c.Check(entries[2].Dir, Equals, "/var/cache/fontconfig")
	c.Check(entries[2].Options, DeepEquals, []string{"bind", "ro"})

	entries = spec.UserMountEntries()
	c.Assert(entries, HasLen, 1)
	c.Check(entries[0].Dir, Equals, "$XDG_RUNTIME_DIR/doc")

	for _, distroWithQuirks := range []string{"fedora", "arch"} {
		restore = release.MockReleaseInfo(&release.OS{ID: distroWithQuirks})
		defer restore()

		tmpdir = c.MkDir()
		dirs.SetRootDir(tmpdir)
		if distroWithQuirks == "fedora" {
			// Fedora is a little special with their fontconfig cache location(s) and how we handle them
			c.Assert(dirs.SystemFontconfigCacheDirs, DeepEquals, []string{filepath.Join(tmpdir, "/var/cache/fontconfig"), filepath.Join(tmpdir, "/usr/lib/fontconfig/cache")})
		} else {
			c.Assert(dirs.SystemFontconfigCacheDirs, DeepEquals, []string{filepath.Join(tmpdir, "/var/cache/fontconfig")})
		}
		c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/fonts"), 0777), IsNil)
		c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/local/share/fonts"), 0777), IsNil)
		c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/lib/fontconfig/cache"), 0777), IsNil)
		c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/fontconfig"), 0777), IsNil)
		spec = &mount.Specification{}
		c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)
		entries = spec.MountEntries()
		c.Assert(entries, HasLen, 2)

		for _, en := range entries {
			if en.Dir == "/var/cache/fontconfig" || en.Dir == "/usr/lib/fontconfig/cache" {
				c.Fatalf("unpexected cache mount entry: %q", en.Dir)
			}
		}
	}
}

func (s *DesktopInterfaceSuite) TestStaticInfo(c *C) {
	si := interfaces.StaticInfoOf(s.iface)
	c.Assert(si.ImplicitOnCore, Equals, false)
	c.Assert(si.ImplicitOnClassic, Equals, true)
	c.Assert(si.Summary, Equals, `allows access to basic graphical desktop resources`)
	c.Assert(si.BaseDeclarationSlots, testutil.Contains, "desktop")
	c.Assert(si.AffectsPlugOnRefresh, Equals, true)
}

func (s *DesktopInterfaceSuite) TestInterfaces(c *C) {
	c.Check(builtin.Interfaces(), testutil.DeepContains, s.iface)
}

func (s *DesktopInterfaceSuite) TestDisableMountHostFontCache(c *C) {
	const mockSnapYaml = `name: desktop-snap
version: 1.0
plugs:
  desktop:
    mount-host-font-cache: false
`
	plug, plugInfo := MockConnectedPlug(c, mockSnapYaml, nil, "desktop")
	c.Check(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)

	// The fontconfig cache is not mounted.
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/local/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/fontconfig"), 0777), IsNil)
	restore := release.MockOnClassic(true)
	defer restore()
	// mock a distribution where the fontconfig cache would always be
	// mounted
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, s.coreSlot), IsNil)
	var mounts []string
	for _, ent := range spec.MountEntries() {
		mounts = append(mounts, ent.Dir)
	}
	c.Check(mounts, Not(testutil.Contains), "/var/cache/fontconfig")
}

func (s *DesktopInterfaceSuite) TestMountFontCacheTrue(c *C) {
	const mockSnapYaml = `name: desktop-snap
version: 1.0
plugs:
  desktop:
    mount-font-cache: true
`
	plug, plugInfo := MockConnectedPlug(c, mockSnapYaml, nil, "desktop")
	c.Check(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)

	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/fontconfig"), 0777), IsNil)
	restore := release.MockOnClassic(true)
	defer restore()
	// mock a distribution where the fontconfig cache would always be
	// mounted
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, plug, s.coreSlot), IsNil)
	var mounts []string
	for _, ent := range spec.MountEntries() {
		mounts = append(mounts, ent.Dir)
	}
	c.Check(mounts, testutil.Contains, "/var/cache/fontconfig")
}

func (s *DesktopInterfaceSuite) TestMountHostFontCacheNotBool(c *C) {
	const mockSnapYamlTemplate = `name: desktop-snap
version: 1.0
plugs:
  desktop:
    mount-host-font-cache: %s
`
	for _, value := range []string{
		`"hello world"`,
		`""`,
		"42",
		"[1,2,3,4]",
		`{"foo":"bar"}`,
	} {
		_, plugInfo := MockConnectedPlug(c, fmt.Sprintf(mockSnapYamlTemplate, value), nil, "desktop")
		c.Check(interfaces.BeforePreparePlug(s.iface, plugInfo), ErrorMatches, "desktop plug requires bool with 'mount-host-font-cache'", Commentf(value))
	}
}
