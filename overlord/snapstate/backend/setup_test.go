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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type setupSuite struct {
	testutil.BaseTest
	be                backend.Backend
	umount            *testutil.MockCmd
	systemctlRestorer func()
}

var _ = Suite(&setupSuite{})

func (s *setupSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	dirs.SetRootDir(c.MkDir())

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	s.systemctlRestorer = systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	})

	s.umount = testutil.MockCommand(c, "umount", "")
}

func (s *setupSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
	dirs.SetRootDir("")
	bootloader.Force(nil)
	s.umount.Restore()
	s.systemctlRestorer()
}

func (s *setupSuite) TestSetupDoUndoSimple(c *C) {
	snapPath := makeTestSnap(c, helloYaml1)

	si := snap.SideInfo{
		RealName: "hello",
		Revision: snap.R(14),
	}

	snapType, undoCtx, err := s.be.SetupSnap(snapPath, "hello", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)
	c.Check(snapType, Equals, snap.TypeApp)

	// after setup the snap file is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello_14.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "hello/14"))
	c.Assert(mup, testutil.FileMatches, fmt.Sprintf("(?ms).*^Where=%s", filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "hello/14")))
	c.Assert(mup, testutil.FileMatches, "(?ms).*^What=/var/lib/snapd/snaps/hello_14.snap")

	minInfo := snap.MinimalPlaceInfo("hello", snap.R(14))
	// mount dir was created
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	// undo undoes the mount unit and the instdir creation
	err = s.be.UndoSetupSnap(minInfo, "app", nil, progress.Null)
	c.Assert(err, IsNil)

	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, false)

}

func (s *setupSuite) TestSetupDoUndoInstance(c *C) {
	snapPath := makeTestSnap(c, helloYaml1)

	si := snap.SideInfo{
		RealName: "hello",
		Revision: snap.R(14),
	}

	snapType, undoCtx, err := s.be.SetupSnap(snapPath, "hello_instance", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)
	c.Check(snapType, Equals, snap.TypeApp)

	// after setup the snap file is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello_instance_14.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "hello_instance/14"))
	c.Assert(mup, testutil.FileMatches, fmt.Sprintf("(?ms).*^Where=%s", filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "hello_instance/14")))
	c.Assert(mup, testutil.FileMatches, "(?ms).*^What=/var/lib/snapd/snaps/hello_instance_14.snap")

	minInfo := snap.MinimalPlaceInfo("hello_instance", snap.R(14))
	// mount dir was created
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	// undo undoes the mount unit and the instdir creation
	err = s.be.UndoSetupSnap(minInfo, "app", nil, progress.Null)
	c.Assert(err, IsNil)

	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, false)
}

func (s *setupSuite) TestSetupDoUndoKernel(c *C) {
	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)

	// we don't get real mounting
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")

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

	snapType, undoCtx, err := s.be.SetupSnap(snapPath, "kernel", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Check(snapType, Equals, snap.TypeKernel)
	c.Assert(undoCtx, NotNil)
	c.Assert(loader.ExtractKernelAssetsCalls, HasLen, 1)
	c.Assert(loader.ExtractKernelAssetsCalls[0].InstanceName(), Equals, "kernel")
	minInfo := snap.MinimalPlaceInfo("kernel", snap.R(140))

	// undo deletes the kernel assets again
	err = s.be.UndoSetupSnap(minInfo, "kernel", nil, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(loader.RemoveKernelAssetsCalls, HasLen, 1)
	c.Assert(loader.RemoveKernelAssetsCalls[0].InstanceName(), Equals, "kernel")
}

func (s *setupSuite) TestSetupDoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work
	// use a kernel because that does and need to do strictly more

	// this cannot check systemd own behavior though around mounts!

	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)
	// we don't get real mounting
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")

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

	_, undoCtx, err := s.be.SetupSnap(snapPath, "kernel", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)
	c.Assert(loader.ExtractKernelAssetsCalls, HasLen, 1)
	c.Assert(loader.ExtractKernelAssetsCalls[0].InstanceName(), Equals, "kernel")

	// retry run
	_, undoCtx, err = s.be.SetupSnap(snapPath, "kernel", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)
	c.Assert(loader.ExtractKernelAssetsCalls, HasLen, 2)
	c.Assert(loader.ExtractKernelAssetsCalls[1].InstanceName(), Equals, "kernel")
	minInfo := snap.MinimalPlaceInfo("kernel", snap.R(140))

	// sanity checks
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 1)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, true)
}

func (s *setupSuite) TestSetupUndoIdempotent(c *C) {
	// make sure that a retry wouldn't stumble on partial work
	// use a kernel because that does and need to do strictly more

	// this cannot check systemd own behavior though around mounts!

	loader := boottest.NewMockBootloader("mock", c.MkDir())
	bootloader.Force(loader)
	// we don't get real mounting
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	defer os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")

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

	_, undoCtx, err := s.be.SetupSnap(snapPath, "kernel", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)

	minInfo := snap.MinimalPlaceInfo("kernel", snap.R(140))

	err = s.be.UndoSetupSnap(minInfo, "kernel", nil, progress.Null)
	c.Assert(err, IsNil)

	// retry run
	err = s.be.UndoSetupSnap(minInfo, "kernel", nil, progress.Null)
	c.Assert(err, IsNil)

	// sanity checks
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)

	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, false)

	// assets got extracted and then removed again
	c.Assert(loader.ExtractKernelAssetsCalls, HasLen, 1)
	c.Assert(loader.RemoveKernelAssetsCalls, HasLen, 1)
}

func (s *setupSuite) TestSetupUndoKeepsTargetSnap(c *C) {
	snapPath := makeTestSnap(c, helloYaml1)

	si := snap.SideInfo{
		RealName: "hello",
		Revision: snap.R(14),
	}

	snapType, undoCtx, err := s.be.SetupSnap(snapPath, "hello", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)
	c.Check(snapType, Equals, snap.TypeApp)

	// after setup the snap file is in the right dir
	c.Assert(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello_14.snap")), Equals, true)

	// ensure the right unit is created
	mup := systemd.MountUnitPath(filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "hello/14"))
	c.Assert(mup, testutil.FileMatches, fmt.Sprintf("(?ms).*^Where=%s", filepath.Join(dirs.StripRootDir(dirs.SnapMountDir), "hello/14")))
	c.Assert(mup, testutil.FileMatches, "(?ms).*^What=/var/lib/snapd/snaps/hello_14.snap")

	minInfo := snap.MinimalPlaceInfo("hello", snap.R(14))
	// mount dir was created
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	// undo keeps the target .snap file intact if requested
	undoCtx = &backend.InstallUndoContext{KeepTargetSnap: true}
	c.Assert(s.be.UndoSetupSnap(minInfo, "app", undoCtx, progress.Null), IsNil)

	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)
	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, true)
}

func (s *setupSuite) TestSetupCleanupAfterFail(c *C) {
	snapPath := makeTestSnap(c, helloYaml1)

	si := snap.SideInfo{
		RealName: "hello",
		Revision: snap.R(14),
	}

	r := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		// mount unit start fails
		if len(cmd) >= 2 && cmd[0] == "start" && strings.HasSuffix(cmd[1], ".mount") {
			return nil, fmt.Errorf("failed")
		}
		return []byte("ActiveState=inactive\n"), nil
	})
	defer r()

	_, undoCtx, err := s.be.SetupSnap(snapPath, "hello", &si, progress.Null)
	c.Assert(err, ErrorMatches, "failed")
	c.Check(undoCtx, IsNil)

	// everything is gone
	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Check(l, HasLen, 0)

	minInfo := snap.MinimalPlaceInfo("hello", snap.R(14))
	c.Check(osutil.FileExists(minInfo.MountDir()), Equals, false)
	c.Check(osutil.FileExists(minInfo.MountFile()), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapBlobDir, "hello_14.snap")), Equals, false)
}

func (s *setupSuite) TestRemoveSnapFilesDir(c *C) {
	snapPath := makeTestSnap(c, helloYaml1)

	si := snap.SideInfo{
		RealName: "hello",
		Revision: snap.R(14),
	}

	snapType, undoCtx, err := s.be.SetupSnap(snapPath, "hello_instance", &si, progress.Null)
	c.Assert(err, IsNil)
	c.Assert(undoCtx, NotNil)
	c.Check(snapType, Equals, snap.TypeApp)

	minInfo := snap.MinimalPlaceInfo("hello_instance", snap.R(14))
	// mount dir was created
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, true)

	undoCtx = &backend.InstallUndoContext{}
	s.be.RemoveSnapFiles(minInfo, snapType, undoCtx, progress.Null)
	c.Assert(err, IsNil)

	l, _ := filepath.Glob(filepath.Join(dirs.SnapServicesDir, "*.mount"))
	c.Assert(l, HasLen, 0)
	c.Assert(osutil.FileExists(minInfo.MountDir()), Equals, false)
	c.Assert(osutil.FileExists(minInfo.MountFile()), Equals, false)
	c.Assert(osutil.FileExists(snap.BaseDir(minInfo.InstanceName())), Equals, true)
	c.Assert(osutil.FileExists(snap.BaseDir(minInfo.SnapName())), Equals, true)

	// /snap/hello is kept as other instances exist
	err = s.be.RemoveSnapDir(minInfo, true)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(snap.BaseDir(minInfo.InstanceName())), Equals, false)
	c.Assert(osutil.FileExists(snap.BaseDir(minInfo.SnapName())), Equals, true)

	// /snap/hello is removed when there are no more instances
	err = s.be.RemoveSnapDir(minInfo, false)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(snap.BaseDir(minInfo.SnapName())), Equals, false)
}
