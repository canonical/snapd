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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/snap/squashfs"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/testutil"

	. "gopkg.in/check.v1"
)

// mockBootloader mocks a the bootloader interface and records all
// set/get calls
type mockBootloader struct {
	bootvars map[string]string
	bootdir  string
}

func newMockBootloader(bootdir string) *mockBootloader {
	return &mockBootloader{
		bootvars: make(map[string]string),
		bootdir:  bootdir,
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
	findBootloader = func() (partition.Bootloader, error) {
		return s.bootloader, nil
	}

	s.AddCleanup(func() { findBootloader = partition.FindBootloader })
}

func (s *SquashfsTestSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

var _ = Suite(&SquashfsTestSuite{})

const packageHello = `name: hello-snap
version: 1.10
`

func (s *SquashfsTestSuite) TestMakeSnapMakesSquashfs(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapFile(snapPkg, "developer", true)
	c.Assert(err, IsNil)

	// ensure the right backend got picked up
	c.Assert(part.deb, FitsTypeOf, &squashfs.Snap{})
}

func (s *SquashfsTestSuite) TestInstallViaSquashfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-snap.developer_1.10.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath("/snaps/hello-snap.developer/1.10", "mount")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/snaps/hello-snap.developer/1.10")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snappy/snaps/hello-snap.developer_1.10.snap")
}

func (s *SquashfsTestSuite) TestAddSquashfsMount(c *C) {
	m := &snapYaml{
		Name:          "foo.developer",
		Version:       "1.0",
		Architectures: []string{"all"},
	}
	inter := &MockProgressMeter{}
	err := addSquashfsMount(m, filepath.Join(dirs.SnapSnapsDir, "foo.developer/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure correct mount unit
	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "snaps-foo.developer-1.0.mount"))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, `[Unit]
Description=Squashfs mount unit for foo.developer

[Mount]
What=/var/lib/snappy/snaps/foo.developer_1.0.snap
Where=/snaps/foo.developer/1.0
`)

}

func (s *SquashfsTestSuite) TestRemoveSquashfsMountUnit(c *C) {
	m := &snapYaml{}
	inter := &MockProgressMeter{}
	err := addSquashfsMount(m, filepath.Join(dirs.SnapSnapsDir, "foo.developer/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure we have the files
	p := filepath.Join(dirs.SnapServicesDir, "snaps-foo.developer-1.0.mount")
	c.Assert(osutil.FileExists(p), Equals, true)

	// now call remove and ensure they are gone
	err = removeSquashfsMount(m, filepath.Join(dirs.SnapSnapsDir, "foo.developer/1.0"), inter)
	c.Assert(err, IsNil)
	p = filepath.Join(dirs.SnapServicesDir, "snaps-foo.developer-1.0.mount")
	c.Assert(osutil.FileExists(p), Equals, false)
}

func (s *SquashfsTestSuite) TestRemoveViaSquashfsWorks(c *C) {
	snapFile := makeTestSnapPackage(c, packageHello)
	_, err := (&Overlord{}).Install(snapFile, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-snap.developer_1.10.snap")), Equals, true)

	// now remove and ensure its gone
	part, err := NewSnapFile(snapFile, "developer", true)
	c.Assert(err, IsNil)
	installedPart, err := newSnapFromYaml(filepath.Join(part.instdir, "meta", "package.yaml"), part.developer, part.m)
	err = (&Overlord{}).Uninstall(installedPart, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-snap.developer_1.10.snap")), Equals, false)

}

const packageOS = `
name: ubuntu-core
version: 15.10-1
type: os
vendor: Someone
`

func (s *SquashfsTestSuite) TestInstallOsSnapUpdatesBootloader(c *C) {
	snapPkg := makeTestSnapPackage(c, packageOS)
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.bootvars, DeepEquals, map[string]string{
		"snappy_os":   "ubuntu-core.developer_15.10-1.snap",
		"snappy_mode": "try",
	})
}

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone

kernel: vmlinuz-4.2
initrd: initrd.img-4.2
`

func (s *SquashfsTestSuite) TestInstallKernelSnapUpdatesBootloader(c *C) {
	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
		{"initrd.img-4.2", "...and I'm an initrd"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.bootvars, DeepEquals, map[string]string{
		"snappy_kernel": "ubuntu-kernel.developer_4.0-1.snap",
		"snappy_mode":   "try",
	})
}

func (s *SquashfsTestSuite) TestInstallKernelSnapUnpacksKernel(c *C) {
	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
		{"initrd.img-4.2", "...and I'm an initrd"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	bootdir := s.bootloader.Dir()

	// kernel is here and normalized
	vmlinuz := filepath.Join(bootdir, "ubuntu-kernel.developer_4.0-1.snap", "vmlinuz")
	content, err := ioutil.ReadFile(vmlinuz)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, files[0][1])

	// and so is initrd
	initrd := filepath.Join(bootdir, "ubuntu-kernel.developer_4.0-1.snap", "initrd.img")
	content, err = ioutil.ReadFile(initrd)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, files[1][1])
}

func (s *SquashfsTestSuite) TestInstallKernelSnapRemovesKernelAssets(c *C) {
	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
		{"initrd.img-4.2", "...and I'm an initrd"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)
	kernelAssetsDir := filepath.Join(s.bootloader.Dir(), "ubuntu-kernel.developer_4.0-1.snap")
	c.Assert(osutil.FileExists(kernelAssetsDir), Equals, true)

	// ensure uninstall cleans the kernel assets
	part, err := NewSnapFile(snapPkg, "developer", true)
	c.Assert(err, IsNil)
	installedPart, err := newSnapFromYaml(filepath.Join(part.instdir, "meta", "package.yaml"), part.developer, part.m)
	installedPart.isActive = false
	err = (&Overlord{}).Uninstall(installedPart, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *SquashfsTestSuite) TestActiveKernelNotRemovable(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageKernel)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml, testDeveloper)
	c.Assert(err, IsNil)

	snap.isActive = true
	c.Assert((&Overlord{}).Uninstall(snap, &MockProgressMeter{}), Equals, ErrPackageNotRemovable)
}

func (s *SquashfsTestSuite) TestInstallKernelSnapUnpacksKernelErrors(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapFile(snapPkg, "developer", true)
	c.Assert(err, IsNil)

	err = extractKernelAssets(part, nil, 0)
	c.Assert(err, ErrorMatches, `can not extract kernel assets from snap type "app"`)
}

func (s *SquashfsTestSuite) TestInstallKernelSnapRemoveAssetsWrongType(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageHello)
	c.Assert(err, IsNil)

	part, err := NewInstalledSnap(snapYaml, testDeveloper)
	c.Assert(err, IsNil)

	err = removeKernelAssets(part, nil)
	c.Assert(err, ErrorMatches, `can not remove kernel assets from snap type "app"`)
}

func (s *SquashfsTestSuite) TestActiveOSNotRemovable(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageOS)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml, testDeveloper)
	c.Assert(err, IsNil)

	snap.isActive = true
	c.Assert((&Overlord{}).Uninstall(snap, &MockProgressMeter{}), Equals, ErrPackageNotRemovable)
}

func (s *SquashfsTestSuite) TestInstallOsRebootRequired(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageOS)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(snapYaml, testDeveloper)
	c.Assert(err, IsNil)

	snap.isActive = false
	s.bootloader.bootvars["snappy_os"] = "ubuntu-core." + testDeveloper + "_15.10-1.snap"
	c.Assert(snap.NeedsReboot(), Equals, true)
}

func (s *SquashfsTestSuite) TestInstallKernelRebootRequired(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageKernel)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnap(snapYaml, testDeveloper)
	c.Assert(err, IsNil)
	c.Assert(snap.NeedsReboot(), Equals, false)

	snap.isActive = false
	s.bootloader.bootvars["snappy_kernel"] = "ubuntu-kernel." + testDeveloper + "_4.0-1.snap"
	c.Assert(snap.NeedsReboot(), Equals, true)

	// simulate we booted the kernel successfully
	s.bootloader.bootvars["snappy_good_kernel"] = "ubuntu-kernel." + testDeveloper + "_4.0-1.snap"
	c.Assert(snap.NeedsReboot(), Equals, false)
}

func getFakeGrubGadget() (*snapYaml, error) {
	return &snapYaml{
		Gadget: Gadget{
			Hardware: Hardware{
				Bootloader: "grub",
			},
		},
	}, nil
}

func (s *SquashfsTestSuite) TestInstallKernelSnapNoUnpacksKernelForGrub(c *C) {
	// pretend to be a grub system
	origGetGadget := getGadget
	s.AddCleanup(func() { getGadget = origGetGadget })
	getGadget = getFakeGrubGadget

	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, IsNil)

	// kernel is *not* here
	vmlinuz := filepath.Join(s.bootloader.Dir(), "ubuntu-kernel.developer_4.0-1.snap", "vmlinuz")
	c.Assert(osutil.FileExists(vmlinuz), Equals, false)
}

func (s *SquashfsTestSuite) TestInstallFailUnmountsSnap(c *C) {
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
	_, err := (&Overlord{}).Install(snapPkg, "developer", 0, &MockProgressMeter{})
	c.Assert(err, ErrorMatches, "could not find specified template: not-there.*")

	// ensure the mount unit is not there
	mup := systemd.MountUnitPath("/snaps/hello.developer/1.10", "mount")
	c.Assert(osutil.FileExists(mup), Equals, false)

	// ensure that the mount gets unmounted and stopped
	c.Assert(s.systemdCmds, DeepEquals, [][]string{
		{"start", "snaps-hello.developer-1.10.mount"},
		{"--root", dirs.GlobalRootDir, "disable", "snaps-hello.developer-1.10.mount"},
		{"stop", "snaps-hello.developer-1.10.mount"},
		{"show", "--property=ActiveState", "snaps-hello.developer-1.10.mount"},
	})
}
