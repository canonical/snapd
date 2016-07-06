// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

// mockBootloader mocks a the bootloader interface and records all
// set/get calls
type mockBootloader struct {
	bootvars map[string]string
	bootdir  string
	name     string
}

func newMockBootloader(bootdir string) *mockBootloader {
	return &mockBootloader{
		bootvars: make(map[string]string),
		bootdir:  bootdir,
		name:     "mocky",
	}
}

func (b *mockBootloader) SetBootVar(key, value string) error {
	b.bootvars[key] = value
	return nil
}

func (b *mockBootloader) GetBootVar(key string) (string, error) {
	return b.bootvars[key], nil
}

func (b *mockBootloader) Dir() string {
	return b.bootdir
}

func (b *mockBootloader) Name() string {
	return b.name
}

type SquashfsTestSuite struct {
	testutil.BaseTest

	bootloader  *mockBootloader
	systemdCmds [][]string
}

func (s *SquashfsTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)
	os.MkdirAll(dirs.SnapSnapsDir, 0755)

	// ensure we do not run a real systemd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		s.systemdCmds = append(s.systemdCmds, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	// mock the boot variable writing for the tests
	s.bootloader = newMockBootloader(c.MkDir())
	partition.ForceBootloader(s.bootloader)

	s.AddCleanup(func() { partition.ForceBootloader(nil) })
}

func (s *SquashfsTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

var _ = Suite(&SquashfsTestSuite{})

const packageHello = `name: hello-snap
version: 1.10
`

func (s *SquashfsTestSuite) TestOpenSnapFile(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	info, snapf, err := openSnapFile(snapPkg, true, nil)
	c.Assert(err, IsNil)

	// ensure the right backend got picked up
	c.Assert(snapf, FitsTypeOf, &squashfs.Snap{})
	c.Check(info.Name(), Equals, "hello-snap")
}

func (s *SquashfsTestSuite) TestOpenSnapFilebSideInfo(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	si := snap.SideInfo{RealName: "blessed", Revision: snap.R(42)}
	info, _, err := openSnapFile(snapPkg, true, &si)
	c.Assert(err, IsNil)

	// check side info
	c.Check(info.Name(), Equals, "blessed")
	c.Check(info.Revision, Equals, snap.R(42))
}

func (s *SquashfsTestSuite) TestInstallViaSquashfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	si := &snap.SideInfo{
		RealName: "hello-snap",
		Revision: snap.R(16),
	}
	_, err := (&Overlord{}).installWithSideInfo(snapPkg, si, LegacyInhibitHooks, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-snap_16.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath("/snap/hello-snap/16", "mount")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/snap/hello-snap/16")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snapd/snaps/hello-snap_16.snap")
}

func (s *SquashfsTestSuite) TestAddMountUnit(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}
	inter := &MockProgressMeter{}
	err := addMountUnit(info, true, inter)
	c.Assert(err, IsNil)

	// ensure correct mount unit
	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "snap-foo-13.mount"))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, `[Unit]
Description=Mount unit for foo

[Mount]
What=/var/lib/snapd/snaps/foo_13.snap
Where=/snap/foo/13
Type=squashfs

[Install]
WantedBy=multi-user.target
`)

}

func (s *SquashfsTestSuite) TestRemoveMountUnit(c *C) {
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(13),
		},
		Version:       "1.1",
		Architectures: []string{"all"},
	}
	inter := &MockProgressMeter{}
	err := addMountUnit(info, true, inter)
	c.Assert(err, IsNil)

	// ensure we have the files
	p := filepath.Join(dirs.SnapServicesDir, "snap-foo-13.mount")
	c.Assert(osutil.FileExists(p), Equals, true)

	// now call remove and ensure they are gone
	err = removeMountUnit(info.MountDir(), inter)
	c.Assert(err, IsNil)
	p = filepath.Join(dirs.SnapServicesDir, "snaps-foo-13.mount")
	c.Assert(osutil.FileExists(p), Equals, false)
}

func (s *SquashfsTestSuite) TestRemoveViaSquashfsWorks(c *C) {
	snapPath := makeTestSnapPackage(c, packageHello)
	si := &snap.SideInfo{
		RealName: "hello-snap",
		Revision: snap.R(16),
	}
	snap, err := (&Overlord{}).installWithSideInfo(snapPath, si, LegacyInhibitHooks, &MockProgressMeter{})
	c.Assert(err, IsNil)
	installedSnap, err := NewInstalledSnap(filepath.Join(snap.MountDir(), "meta", "snap.yaml"))
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-snap_16.snap")), Equals, true)

	// now remove and ensure its gone
	err = (&Overlord{}).uninstall(installedSnap, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-snap_16.snap")), Equals, false)

}

const packageOS = `
name: ubuntu-core
version: 15.10-1
type: os
vendor: Someone
`

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

func (s *SquashfsTestSuite) TestInstallKernelSnapUnpacksKernel(c *C) {
	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/bar.dtb", "hello, I'm bar.dtb"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	_, err := (&Overlord{}).installWithSideInfo(snapPkg, si, LegacyInhibitHooks, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	bootdir := s.bootloader.Dir()
	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		fullFn := filepath.Join(bootdir, "ubuntu-kernel_42.snap", def[0])
		content, err := ioutil.ReadFile(fullFn)
		c.Assert(err, IsNil)
		c.Assert(string(content), Equals, def[1])
	}
}

func (s *SquashfsTestSuite) TestInstallKernelSnapRemovesKernelAssets(c *C) {
	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	snap, err := (&Overlord{}).installWithSideInfo(snapPkg, si, LegacyInhibitHooks, &MockProgressMeter{})
	c.Assert(err, IsNil)
	installedSnap, err := NewInstalledSnap(filepath.Join(snap.MountDir(), "meta", "snap.yaml"))
	c.Assert(err, IsNil)
	installedSnap.isActive = false

	kernelAssetsDir := filepath.Join(s.bootloader.Dir(), "ubuntu-kernel_42.snap")
	c.Assert(osutil.FileExists(kernelAssetsDir), Equals, true)

	// ensure uninstall cleans the kernel assets
	err = (&Overlord{}).uninstall(installedSnap, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *SquashfsTestSuite) TestActiveKernelNotRemovable(c *C) {
	snapYaml, err := makeInstalledMockSnap(packageKernel, 11)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)

	snap.isActive = true
	c.Assert((&Overlord{}).uninstall(snap, &MockProgressMeter{}), Equals, ErrPackageNotRemovable)
}

func (s *SquashfsTestSuite) TestActiveOSNotRemovable(c *C) {
	snapYaml, err := makeInstalledMockSnap(packageOS, 11)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)

	snap.isActive = true
	c.Assert((&Overlord{}).uninstall(snap, &MockProgressMeter{}), Equals, ErrPackageNotRemovable)
}

func (s *SquashfsTestSuite) TestInstallOsRebootRequired(c *C) {
	snapYaml, err := makeInstalledMockSnap(packageOS, 160)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)

	snap.isActive = false
	s.bootloader.bootvars["snappy_os"] = "ubuntu-core_160.snap"
	c.Assert(snap.NeedsReboot(), Equals, true)
}

func (s *SquashfsTestSuite) TestInstallKernelRebootRequired(c *C) {
	snapYaml, err := makeInstalledMockSnap(packageKernel, 40)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml)
	c.Assert(err, IsNil)
	c.Assert(snap.NeedsReboot(), Equals, false)

	snap.isActive = false
	s.bootloader.bootvars["snappy_kernel"] = "ubuntu-kernel_40.snap"
	c.Assert(snap.NeedsReboot(), Equals, true)

	// simulate we booted the kernel successfully
	s.bootloader.bootvars["snappy_good_kernel"] = "ubuntu-kernel_40.snap"
	c.Assert(snap.NeedsReboot(), Equals, false)
}

func (s *SquashfsTestSuite) TestInstallKernelSnapNoUnpacksKernelForGrub(c *C) {
	// pretend to be a grub system
	s.bootloader.name = "grub"

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	_, err := (&Overlord{}).install(snapPkg, LegacyInhibitHooks, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// kernel is *not* here
	vmlinuz := filepath.Join(s.bootloader.Dir(), "ubuntu-kernel_4.0-1.snap", "kernel.img")
	c.Assert(osutil.FileExists(vmlinuz), Equals, false)
}

func (s *SquashfsTestSuite) TestInstallFailUnmountsSnap(c *C) {
	c.Skip("no easy path to this kind of late verification failure now!")
	snapPkg := makeTestSnapPackage(c, `name: hello
version: 1.10
apps:
 some-binary:
  command: some-binary
  plugs: [some-binary]

plugs:
 some-binary:
  interface: old-security
  security-template: not-there
`)
	// install but our missing security-template will break the install
	// revision will be 0
	_, err := (&Overlord{}).install(snapPkg, 0, &MockProgressMeter{})
	c.Assert(err, ErrorMatches, "could not find specified template: not-there.*")

	// ensure the mount unit is not there
	mup := systemd.MountUnitPath("/snap/hello/1.10", "mount")
	c.Assert(osutil.FileExists(mup), Equals, false)

	// ensure that the mount gets unmounted and stopped
	c.Assert(s.systemdCmds, DeepEquals, [][]string{
		{"start", "snap-hello-0.mount"},
		{"--root", dirs.GlobalRootDir, "disable", "snap-hello-0.mount"},
		{"stop", "snap-hello-0.mount"},
		{"show", "--property=ActiveState", "snap-hello-0.mount"},
	})
}
