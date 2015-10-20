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

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/partition"
	"launchpad.net/snappy/pkg/snapfs"
	"launchpad.net/snappy/systemd"

	. "gopkg.in/check.v1"
)

var mockb mockBootloader

func newMockBootloader() (partition.BootLoader, error) {
	return &mockb, nil
}

type mockBootloader struct {
	bootvars map[string]string
}

func (b *mockBootloader) Name() partition.BootloaderName {
	return ""
}
func (b *mockBootloader) ToggleRootFS(otherRootfs string) error {
	return nil
}
func (b *mockBootloader) SyncBootFiles(bootAssets map[string]string) error {
	return nil
}
func (b *mockBootloader) HandleAssets() error {
	return nil
}
func (b *mockBootloader) GetBootVar(name string) (string, error) {
	return b.bootvars[name], nil
}
func (b *mockBootloader) SetBootVar(name, value string) error {
	b.bootvars[name] = value
	return nil
}
func (b *mockBootloader) GetNextBootRootFSName() (string, error) {
	return "", nil
}
func (b *mockBootloader) MarkCurrentBootSuccessful(currentRootfs string) error {
	return nil
}
func (b *mockBootloader) BootDir() string {
	return ""
}

const packageHello = `name: hello-app
version: 1.10
vendor: Somebody
icon: meta/hello.svg
`

const packageOS = `name: ubuntu-core
version: 15.10-1
type: os
vendor: Someone
`

const packageKernel = `name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone

kernel: vmlinuz-4.2
initrd: initrd.img-4.2
`

type SnapfsTestSuite struct {
	mockBootloaderDir string
}

func (s *SnapfsTestSuite) SetUpTest(c *C) {
	// mocks
	aaClickHookCmd = "/bin/true"
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(filepath.Join(dirs.SnapServicesDir, "multi-user.target.wants"), 0755)

	// ensure we do not run a real systemd (slows down tests)
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	// mock bootloader
	mockb = mockBootloader{
		bootvars: make(map[string]string),
	}
	partition.Bootloader = newMockBootloader

	// and bootloader dir
	s.mockBootloaderDir = c.MkDir()
	partition.BootloaderDir = func() string {
		return s.mockBootloaderDir
	}

	// ensure we use the right builder func (snapfs)
	snapBuilderFunc = BuildSnapfsSnap
}

func (s *SnapfsTestSuite) TearDownTest(c *C) {
	snapBuilderFunc = BuildLegacySnap
	partition.Bootloader = partition.BootloaderImpl
	partition.BootloaderDir = partition.BootloaderDirImpl
}

var _ = Suite(&SnapfsTestSuite{})

func (s *SnapfsTestSuite) TestMakeSnapMakesSnapfs(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	// ensure the right backend got picked up
	c.Assert(part.deb, FitsTypeOf, &snapfs.Snap{})
}

func (s *SnapfsTestSuite) TestInstallViaSnapfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath("/apps/hello-app.origin/1.10", "mount")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/apps/hello-app.origin/1.10")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snappy/snaps/hello-app.origin_1.10.snap")
}

func (s *SnapfsTestSuite) TestAddSnapfsAutomount(c *C) {
	m := packageYaml{
		Name:          "foo.origin",
		Version:       "1.0",
		Architectures: []string{"all"},
	}
	inter := &MockProgressMeter{}
	err := m.addSnapfsAutomount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure correct mount unit
	mount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.mount"))
	c.Assert(err, IsNil)
	c.Assert(string(mount), Equals, `[Unit]
Description=Snapfs mount unit for foo.origin

[Mount]
What=/var/lib/snappy/snaps/foo.origin_1.0.snap
Where=/apps/foo.origin/1.0
`)

	// and correct automount unit
	automount, err := ioutil.ReadFile(filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.automount"))
	c.Assert(err, IsNil)
	c.Assert(string(automount), Equals, `[Unit]
Description=Snapfs automount unit for foo.origin

[Automount]
Where=/apps/foo.origin/1.0
TimeoutIdleSec=30

[Install]
WantedBy=multi-user.target
`)
}

func (s *SnapfsTestSuite) TestRemoveSnapfsAutomount(c *C) {
	m := packageYaml{}
	inter := &MockProgressMeter{}
	err := m.addSnapfsAutomount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), true, inter)
	c.Assert(err, IsNil)

	// ensure we have the files
	for _, ext := range []string{"mount", "automount"} {
		p := filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.") + ext
		c.Assert(helpers.FileExists(p), Equals, true)
	}

	// now call remove and ensure they are gone
	err = m.removeSnapfsAutomount(filepath.Join(dirs.SnapAppsDir, "foo.origin/1.0"), inter)
	c.Assert(err, IsNil)
	for _, ext := range []string{"mount", "automount"} {
		p := filepath.Join(dirs.SnapServicesDir, "apps-foo.origin-1.0.") + ext
		c.Assert(helpers.FileExists(p), Equals, false)
	}
}

func (s *SnapfsTestSuite) TestRemoveViaSnapfsWorks(c *C) {
	snapPkg := makeTestSnapPackage(c, packageHello)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// after install the blob is in the right dir
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, true)

	// now remove and ensure its gone
	err = part.Uninstall(&MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapBlobDir, "hello-app.origin_1.10.snap")), Equals, false)

}

func (s *SnapfsTestSuite) TestInstallOsSnapWithDebFails(c *C) {
	// ensure we get a error when trying to install old style snap for OS
	snapBuilderFunc = BuildLegacySnap

	snapPkg := makeTestSnapPackage(c, packageOS)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, ErrorMatches, "kernel/os snap must be of type snapfs")
}

func (s *SnapfsTestSuite) TestInstallOsSnapUpdatesBootloader(c *C) {
	snapPkg := makeTestSnapPackage(c, packageOS)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	c.Assert(mockb.bootvars, DeepEquals, map[string]string{
		"snappy_os":   "ubuntu-core.origin_15.10-1.snap",
		"snappy_mode": "try",
	})
}

func (s *SnapfsTestSuite) TestInstallKernelSnapUpdatesBootloader(c *C) {
	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
		{"initrd.img-4.2", "...and I'm an initrd"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	c.Assert(mockb.bootvars, DeepEquals, map[string]string{
		"snappy_kernel": "ubuntu-kernel.origin_4.0-1.snap",
		"snappy_mode":   "try",
	})
}

func (s *SnapfsTestSuite) TestInstallKernelSnapUnpacksKernel(c *C) {
	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
		{"initrd.img-4.2", "...and I'm an initrd"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)

	// kernel is here and normalized
	vmlinuz := filepath.Join(s.mockBootloaderDir, "ubuntu-kernel.origin_4.0-1.snap", "vmlinuz")
	content, err := ioutil.ReadFile(vmlinuz)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, files[0][1])

	// and so is initrd
	initrd := filepath.Join(s.mockBootloaderDir, "ubuntu-kernel.origin_4.0-1.snap", "initrd.img")
	content, err = ioutil.ReadFile(initrd)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, files[1][1])
}

func (s *SnapfsTestSuite) TestInstallOsRebootRequired(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageOS)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap.NeedsReboot(), Equals, false)

	snap.isActive = false
	mockb.bootvars["snappy_os"] = "ubuntu-core." + testOrigin + "_15.10-1.snap"
	c.Assert(snap.NeedsReboot(), Equals, true)
}

func (s *SnapfsTestSuite) TestInstallKernelRebootRequired(c *C) {
	snapYaml, err := makeInstalledMockSnap(dirs.GlobalRootDir, packageKernel)
	c.Assert(err, IsNil)

	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap.NeedsReboot(), Equals, false)

	snap.isActive = false
	mockb.bootvars["snappy_kernel"] = "ubuntu-kernel." + testOrigin + "_4.0-1.snap"
	c.Assert(snap.NeedsReboot(), Equals, true)
}

func (s *SnapfsTestSuite) TestInstallKernelSnapRemovesKernelAssets(c *C) {
	files := [][]string{
		{"vmlinuz-4.2", "I'm a kernel"},
		{"initrd.img-4.2", "...and I'm an initrd"},
	}
	snapPkg := makeTestSnapPackageWithFiles(c, packageKernel, files)
	part, err := NewSnapPartFromSnapFile(snapPkg, "origin", true)
	c.Assert(err, IsNil)

	_, err = part.Install(&MockProgressMeter{}, 0)
	c.Assert(err, IsNil)
	kernelAssetsDir := filepath.Join(s.mockBootloaderDir, "ubuntu-kernel.origin_4.0-1.snap")
	c.Assert(helpers.FileExists(kernelAssetsDir), Equals, true)

	// ensure uninstall cleans the kernel assets
	err = part.Uninstall(&MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(kernelAssetsDir), Equals, false)
}
