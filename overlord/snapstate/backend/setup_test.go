// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package backend_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type setupSuite struct {
	be           backend.Backend
	nullProgress progress.NullProgress
	prevctlCmd   func(...string) ([]byte, error)
	umount       *testutil.MockCmd
}

var _ = Suite(&setupSuite{})

func (s *setupSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	s.prevctlCmd = systemd.SystemctlCmd
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}
	s.umount = testutil.MockCommand(c, "umount", "")
}

func (s *setupSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	partition.ForceBootloader(nil)
	systemd.SystemctlCmd = s.prevctlCmd
	s.umount.Restore()
}

func (s *setupSuite) TestSetupDoUndoSimple(c *C) {
	// we don't get real chattr
	os.Setenv("SNAPPY_SKIP_CHATTR_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SKIP_CHATTR_FOR_TESTS")

	snapPath := makeTestSnap(c, helloYaml1)

	si := snap.SideInfo{
		RealName: "hello",
		Revision: snap.R(14),
	}

	err := s.be.SetupSnap(snapPath, &si, &s.nullProgress)
	c.Assert(err, IsNil)

	// after setup the snap file is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello_14.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath("/snap/hello/14")
	content, err := ioutil.ReadFile(mup)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?ms).*^Where=/snap/hello/14")
	c.Assert(string(content), Matches, "(?ms).*^What=/var/lib/snapd/snaps/hello_14.snap")

	minInfo := snap.MinimalPlaceInfo("hello", snap.R(14))
	// mount dir was created
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	// undo undoes the mount unit and the instdir creation
	err = s.be.UndoSetupSnap(minInfo, "app", &s.nullProgress)
	c.Assert(err, IsNil)

	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, false)

}

func (s *setupSuite) TestSetupDoUndoKernelUboot(c *C) {
	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	// we don't get real mounting
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	// nor real chattr
	os.Setenv("SNAPPY_SKIP_CHATTR_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SKIP_CHATTR_FOR_TESTS")

	testFiles := [][]string{
		{"kernel.img", "kernel"},
		{"initrd.img", "initrd"},
		{"modules/4.4.0-14-generic/foo.ko", "a module"},
		{"firmware/bar.bin", "some firmware"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := snaptest.MakeTestSnapWithFiles(c, `name: kernel
version: 1.0
type: kernel
`, testFiles)

	si := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(140),
	}

	err := s.be.SetupSnap(snapPath, &si, &s.nullProgress)
	c.Assert(err, IsNil)
	l, _ := filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 1)

	minInfo := snap.MinimalPlaceInfo("kernel", snap.R(140))

	// undo deletes the kernel assets again
	err = s.be.UndoSetupSnap(minInfo, "kernel", &s.nullProgress)
	c.Assert(err, IsNil)

	l, _ = filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 0)
}

func (s *setupSuite) TestSetupDoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work
	// use a kernel because that does and need to do strictly more

	// this cannot check systemd own behavior though around mounts!

	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	// we don't get real mounting
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	// nor real chattr
	os.Setenv("SNAPPY_SKIP_CHATTR_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SKIP_CHATTR_FOR_TESTS")

	testFiles := [][]string{
		{"kernel.img", "kernel"},
		{"initrd.img", "initrd"},
		{"modules/4.4.0-14-generic/foo.ko", "a module"},
		{"firmware/bar.bin", "some firmware"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := snaptest.MakeTestSnapWithFiles(c, `name: kernel
version: 1.0
type: kernel
`, testFiles)

	si := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(140),
	}

	err := s.be.SetupSnap(snapPath, &si, &s.nullProgress)
	c.Assert(err, IsNil)

	// retry run
	err = s.be.SetupSnap(snapPath, &si, &s.nullProgress)
	c.Assert(err, IsNil)

	minInfo := snap.MinimalPlaceInfo("kernel", snap.R(140))

	// sanity checks
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 1)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, true)

	l, _ = filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 1)
}

func (s *setupSuite) TestSetupUndoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work
	// use a kernel because that does and need to do strictly more

	// this cannot check systemd own behavior though around mounts!

	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	// we don't get real mounting
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	// nor real chattr
	os.Setenv("SNAPPY_SKIP_CHATTR_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SKIP_CHATTR_FOR_TESTS")

	testFiles := [][]string{
		{"kernel.img", "kernel"},
		{"initrd.img", "initrd"},
		{"modules/4.4.0-14-generic/foo.ko", "a module"},
		{"firmware/bar.bin", "some firmware"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	snapPath := snaptest.MakeTestSnapWithFiles(c, `name: kernel
version: 1.0
type: kernel
`, testFiles)

	si := snap.SideInfo{
		RealName: "kernel",
		Revision: snap.R(140),
	}

	err := s.be.SetupSnap(snapPath, &si, &s.nullProgress)
	c.Assert(err, IsNil)

	minInfo := snap.MinimalPlaceInfo("kernel", snap.R(140))

	err = s.be.UndoSetupSnap(minInfo, "kernel", &s.nullProgress)
	c.Assert(err, IsNil)

	// retry run
	err = s.be.UndoSetupSnap(minInfo, "kernel", &s.nullProgress)
	c.Assert(err, IsNil)

	// sanity checks
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, false)

	l, _ = filepath.Glob(filepath.Join(bootloader.Dir(), "*"))
	c.Assert(l, HasLen, 0)
}
