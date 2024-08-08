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
	appSlotInfo  *snap.SlotInfo
	appSlot      *interfaces.ConnectedSlot
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

const desktopAppSlotYaml = `name: provider
version: 0
apps:
  app:
slots:
  desktop:
`

const desktopCoreYaml = `name: core
version: 0
type: os
slots:
  desktop:
`

func (s *DesktopInterfaceSuite) SetUpTest(c *C) {
	s.plug, s.plugInfo = MockConnectedPlug(c, desktopConsumerYaml, nil, "desktop")
	s.appSlot, s.appSlotInfo = MockConnectedSlot(c, desktopAppSlotYaml, nil, "desktop")
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

	// On an all-snaps system, the desktop interface grants access
	// to system fonts.
	appSet, err := interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec := apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access basic graphical desktop resources")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "#include <abstractions/fonts>")

	// check desktop interface uses correct label for Mutter when provided
	// by a snap
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "  member=\"GetIdletime\"\n    peer=(label=\"snap.provider.app\"),\n")

	// There are UpdateNS rules to allow mounting the font directories too
	updateNS := spec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /usr/share/fonts\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /usr/local/share/fonts\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /var/cache/fontconfig\n")

	// There are permanent rules on the slot side
	appSet, err = interfaces.NewSnapAppSet(s.appSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.appSlotInfo), IsNil)
	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.provider.app"})
	c.Check(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "# Description: Can provide various desktop services")
	c.Check(spec.SnippetForTag("snap.provider.app"), testutil.Contains, "interface=org.freedesktop.impl.portal.*")

	// On a classic system, additional permissions are granted
	restore = release.MockOnClassic(true)
	defer restore()
	appSet, err = interfaces.NewSnapAppSet(s.plug.Snap(), nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)

	c.Assert(spec.SecurityTags(), DeepEquals, []string{"snap.consumer.app"})
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Description: Can access basic graphical desktop resources")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "/etc/gtk-3.0/settings.ini r,")
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "# Allow access to xdg-desktop-portal and xdg-document-portal")

	// check desktop interface uses correct label for Mutter when provided
	// by the system
	c.Check(spec.SnippetForTag("snap.consumer.app"), testutil.Contains, "  member=\"GetIdletime\"\n    peer=(label=unconfined),\n")

	// As well as the font directories, the document portal can be mounted
	updateNS = spec.UpdateNS()
	c.Check(updateNS, testutil.Contains, "  # Mount the document portal\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /usr/share/fonts\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /usr/local/share/fonts\n")
	c.Check(updateNS, testutil.Contains, "  # Read-only access to /var/cache/fontconfig\n")

	// connected plug to core slot
	appSet, err = interfaces.NewSnapAppSet(s.coreSlotInfo.Snap, nil)
	c.Assert(err, IsNil)
	spec = apparmor.NewSpecification(appSet)
	c.Assert(spec.AddPermanentSlot(s.iface, s.coreSlotInfo), IsNil)
	c.Assert(spec.AddConnectedSlot(s.iface, s.plug, s.coreSlot), IsNil)
	c.Assert(spec.SecurityTags(), HasLen, 0)
}

func (s *DesktopInterfaceSuite) TestMountSpec(c *C) {
	tmpdir := c.MkDir()
	dirs.SetRootDir(tmpdir)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/usr/local/share/fonts"), 0777), IsNil)
	c.Assert(os.MkdirAll(filepath.Join(tmpdir, "/var/cache/fontconfig"), 0777), IsNil)

	// mock an Ubuntu Core like system
	restore := release.MockOnClassic(false)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	// On all-snaps systems like Ubuntu Core, the mounts are present
	spec := &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.appSlot), IsNil)
	c.Check(spec.MountEntries(), HasLen, 3)
	c.Check(spec.UserMountEntries(), HasLen, 1)

	// On classic systems, a number of font related directories
	// are bind mounted from the host system if they exist.
	restore = release.MockOnClassic(true)
	defer restore()
	// distro is already mocked to be Ubuntu

	spec = &mount.Specification{}
	c.Assert(spec.AddConnectedPlug(s.iface, s.plug, s.coreSlot), IsNil)

	entries := spec.MountEntries()
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
	c.Check(entries[0].Name, Equals, "$XDG_RUNTIME_DIR/doc/by-app/snap.consumer")
	c.Check(entries[0].Dir, Equals, "$XDG_RUNTIME_DIR/doc")
	c.Check(entries[0].Options, DeepEquals, []string{"bind", "rw", "x-snapd.ignore-missing"})

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

func (s *DesktopInterfaceSuite) TestDesktopFileIDsValidation(c *C) {
	const mockSnapYaml = `name: desktop-snap
version: 1.0
plugs:
  desktop:
    desktop-file-ids:
      - org.example
      - org.example.Foo
      - org._example
      - org.example-foo
`
	_, plugInfo := MockConnectedPlug(c, mockSnapYaml, nil, "desktop")
	c.Check(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)

	const mockSnapYamlEmpty = `name: desktop-snap
version: 1.0
plugs:
  desktop:
`
	_, plugInfo = MockConnectedPlug(c, mockSnapYaml, nil, "desktop")
	c.Check(interfaces.BeforePreparePlug(s.iface, plugInfo), IsNil)
}

func (s *DesktopInterfaceSuite) TestDesktopFileIDsValidationTypeError(c *C) {
	for _, tc := range []string{
		"not-a-list-of-strings",
		"1",
		"true",
		"[[string],1]",
	} {
		const mockSnapYaml = `name: desktop-snap
version: 1.0
plugs:
  desktop:
    desktop-file-ids: %s
`
		_, plugInfo := MockConnectedPlug(c, fmt.Sprintf(mockSnapYaml, tc), nil, "desktop")

		err := interfaces.BeforePreparePlug(s.iface, plugInfo)
		c.Check(err, ErrorMatches, "cannot add desktop plug: \"desktop-file-ids\" must be a list of strings", Commentf(tc))
	}
}

func (s *DesktopInterfaceSuite) TestDesktopFileIDsValidationFormatError(c *C) {
	// Invalid D-Bus names
	for _, tc := range []string{
		"[1starts-with-a-digit]",
		"[ends-with-dot.]",
		"[org.$pecial-char.Example]",
		`[""]`,
	} {
		const mockSnapYaml = `name: desktop-snap
version: 1.0
plugs:
  desktop:
    desktop-file-ids: %s
`
		_, plugInfo := MockConnectedPlug(c, fmt.Sprintf(mockSnapYaml, tc), nil, "desktop")

		err := interfaces.BeforePreparePlug(s.iface, plugInfo)
		c.Check(err, ErrorMatches, "desktop-file-ids entry .* is not a valid D-Bus well-known name", Commentf(tc))
	}
}
