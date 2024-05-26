// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package bootloader_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
)

type lkTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&lkTestSuite{})

func (s *lkTestSuite) TestNewLk(c *C) {
	// TODO: update this test when v1 lk uses the kernel command line parameter
	//       too

	// no files means bl is not present, but we can still create the bl object
	l := bootloader.NewLk(s.rootdir, nil)
	c.Assert(l, NotNil)
	c.Assert(l.Name(), Equals, "lk")

	present := mylog.Check2(l.Present())

	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockLkFiles(c, s.rootdir, nil)
	present = mylog.Check2(l.Present())

	c.Assert(present, Equals, true)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	f := mylog.Check2(bootloader.LkConfigFile(l))

	c.Check(f, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partlabel", "snapbootsel"))
}

func (s *lkTestSuite) TestNewLkPresentChecksBackupStorageToo(c *C) {
	// no files means bl is not present, but we can still create the bl object
	l := bootloader.NewLk(s.rootdir, &bootloader.Options{
		Role: bootloader.RoleSole,
	})
	c.Assert(l, NotNil)
	c.Assert(l.Name(), Equals, "lk")

	present := mylog.Check2(l.Present())

	c.Assert(present, Equals, false)

	// now mock just the backup env file
	f := mylog.Check2(bootloader.LkConfigFile(l))

	c.Check(f, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partlabel", "snapbootsel"))
	mylog.Check(os.MkdirAll(filepath.Dir(f), 0755))

	mylog.Check(os.WriteFile(f+"bak", nil, 0644))


	// now the bootloader is present because the backup exists
	present = mylog.Check2(l.Present())

	c.Assert(present, Equals, true)
}

func (s *lkTestSuite) TestNewLkUC20Run(c *C) {
	// no files means bl is not present, but we can still create the bl object
	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	// use ubuntu-boot as the root dir
	l := bootloader.NewLk(boot.InitramfsUbuntuBootDir, opts)
	c.Assert(l, NotNil)
	c.Assert(l.Name(), Equals, "lk")

	present := mylog.Check2(l.Present())

	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	present = mylog.Check2(l.Present())

	c.Assert(present, Equals, true)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	f := mylog.Check2(bootloader.LkConfigFile(l))

	// note that the config file here is not relative to ubuntu-boot dir we used
	// when creating the bootloader, it is relative to the rootdir
	c.Check(f, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partuuid", "snapbootsel-partuuid"))
}

func (s *lkTestSuite) TestNewLkUC20Recovery(c *C) {
	// no files means bl is not present, but we can still create the bl object
	opts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}
	// use ubuntu-seed as the root dir
	l := bootloader.NewLk(boot.InitramfsUbuntuSeedDir, opts)
	c.Assert(l, NotNil)
	c.Assert(l.Name(), Equals, "lk")

	present := mylog.Check2(l.Present())

	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	present = mylog.Check2(l.Present())

	c.Assert(present, Equals, true)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	f := mylog.Check2(bootloader.LkConfigFile(l))

	// note that the config file here is not relative to ubuntu-boot dir we used
	// when creating the bootloader, it is relative to the rootdir
	c.Check(f, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partuuid", "snaprecoverysel-partuuid"))
}

func (s *lkTestSuite) TestNewLkImageBuildingTime(c *C) {
	for _, role := range []bootloader.Role{bootloader.RoleSole, bootloader.RoleRecovery} {
		opts := &bootloader.Options{
			PrepareImageTime: true,
			Role:             role,
		}
		r := bootloader.MockLkFiles(c, s.rootdir, opts)
		defer r()
		l := bootloader.NewLk(s.rootdir, opts)
		c.Assert(l, NotNil)
		c.Check(bootloader.LkRuntimeMode(l), Equals, false)
		f := mylog.Check2(bootloader.LkConfigFile(l))

		switch role {
		case bootloader.RoleSole:
			c.Check(f, Equals, filepath.Join(s.rootdir, "/boot/lk", "snapbootsel.bin"))
		case bootloader.RoleRecovery:
			c.Check(f, Equals, filepath.Join(s.rootdir, "/boot/lk", "snaprecoverysel.bin"))
		}
	}
}

func (s *lkTestSuite) TestSetGetBootVar(c *C) {
	tt := []struct {
		role  bootloader.Role
		key   string
		value string
	}{
		{
			bootloader.RoleSole,
			"snap_mode",
			boot.TryingStatus,
		},
		{
			bootloader.RoleRecovery,
			"snapd_recovery_mode",
			boot.ModeRecover,
		},
		{
			bootloader.RoleRunMode,
			"kernel_status",
			boot.TryStatus,
		},
	}
	for _, t := range tt {
		opts := &bootloader.Options{
			Role: t.role,
		}
		r := bootloader.MockLkFiles(c, s.rootdir, opts)
		defer r()
		l := bootloader.NewLk(s.rootdir, opts)
		bootVars := map[string]string{t.key: t.value}
		l.SetBootVars(bootVars)

		v := mylog.Check2(l.GetBootVars(t.key))

		c.Check(v, HasLen, 1)
		c.Check(v[t.key], Equals, t.value)
	}
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksBootimgImageBuilding(c *C) {
	for _, role := range []bootloader.Role{bootloader.RoleSole, bootloader.RoleRecovery} {
		opts := &bootloader.Options{
			PrepareImageTime: true,
			Role:             role,
		}
		r := bootloader.MockLkFiles(c, s.rootdir, opts)
		defer r()
		l := bootloader.NewLk(s.rootdir, opts)

		c.Assert(l, NotNil)

		files := [][]string{
			{"kernel.img", "I'm a kernel"},
			{"initrd.img", "...and I'm an initrd"},
			{"boot.img", "...and I'm an boot image"},
			{"dtbs/foo.dtb", "g'day, I'm foo.dtb"},
			{"dtbs/bar.dtb", "hello, I'm bar.dtb"},
			// must be last
			{"meta/kernel.yaml", "version: 4.2"},
		}
		si := &snap.SideInfo{
			RealName: "ubuntu-kernel",
			Revision: snap.R(42),
		}
		fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
		snapf := mylog.Check2(snapfile.Open(fn))


		info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))


		if role == bootloader.RoleSole {
			mylog.Check(l.ExtractKernelAssets(info, snapf))
		} else {
			mylog.
				// this isn't quite how ExtractRecoveryKernel is typically called,
				// typically it will be called with an actual recovery system dir,
				// but for our purposes this is close enough, we just extract files
				// to some directory
				Check(l.ExtractRecoveryKernelAssets(s.rootdir, info, snapf))
		}


		// just boot.img and snapbootsel.bin are there, no kernel.img
		infos := mylog.Check2(os.ReadDir(filepath.Join(s.rootdir, "boot", "lk", "")))

		var fnames []string
		for _, info := range infos {
			fnames = append(fnames, info.Name())
		}
		sort.Strings(fnames)
		c.Assert(fnames, HasLen, 2)
		expFiles := []string{"boot.img"}
		if role == bootloader.RoleSole {
			expFiles = append(expFiles, "snapbootsel.bin")
		} else {
			expFiles = append(expFiles, "snaprecoverysel.bin")
		}
		c.Assert(fnames, DeepEquals, expFiles)

		// clean up the rootdir for the next iteration
		c.Assert(os.RemoveAll(s.rootdir), IsNil)
	}
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksCustomBootimgImageBuilding(c *C) {
	opts := &bootloader.Options{
		PrepareImageTime: true,
		Role:             bootloader.RoleSole,
	}
	bootloader.MockLkFiles(c, s.rootdir, opts)
	l := bootloader.NewLk(s.rootdir, opts)

	c.Assert(l, NotNil)

	// first configure custom boot image file name
	f := mylog.Check2(bootloader.LkConfigFile(l))

	env := lkenv.NewEnv(f, "", lkenv.V1)
	env.Load()
	env.Set("bootimg_file_name", "boot-2.img")
	mylog.Check(env.Save())


	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"boot-2.img", "...and I'm an boot image"},
		{"dtbs/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/bar.dtb", "hello, I'm bar.dtb"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.Check(l.ExtractKernelAssets(info, snapf))


	// boot-2.img is there
	bootimg := filepath.Join(s.rootdir, "boot", "lk", "boot-2.img")
	c.Assert(osutil.FileExists(bootimg), Equals, true)
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksAndRemoveInRuntimeMode(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()
	opts := &bootloader.Options{
		Role: bootloader.RoleSole,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// ensure we have a valid boot env
	// TODO: this will follow the same logic as RoleRunMode eventually
	bootselPartition := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/snapbootsel")
	lkenv := lkenv.NewEnv(bootselPartition, "", lkenv.V1)

	// don't need to initialize this env, the same file will already have been
	// setup by MockLkFiles()

	// mock a kernel snap that has a boot.img
	files := [][]string{
		{"boot.img", "I'm the default boot image name"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.

		// now extract
		Check(lk.ExtractKernelAssets(info, snapf))


	// and validate it went to the "boot_a" partition
	bootA := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/boot_a")
	content := mylog.Check2(os.ReadFile(bootA))

	c.Assert(string(content), Equals, "I'm the default boot image name")

	// also validate that bootB is empty
	bootB := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/boot_b")
	content = mylog.Check2(os.ReadFile(bootB))

	c.Assert(content, HasLen, 0)
	mylog.

		// test that boot partition got set
		Check(lkenv.Load())

	bootPart := mylog.Check2(lkenv.GetKernelBootPartition("ubuntu-kernel_42.snap"))

	c.Assert(bootPart, Equals, "boot_a")
	mylog.

		// now remove the kernel
		Check(lk.RemoveKernelAssets(info))

	mylog.
		// and ensure its no longer available in the boot partitions
		Check(lkenv.Load())

	bootPart = mylog.Check2(lkenv.GetKernelBootPartition("ubuntu-kernel_42.snap"))
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find kernel %[1]q: no boot image partition has value %[1]q", "ubuntu-kernel_42.snap"))
	c.Assert(bootPart, Equals, "")

	c.Assert(logbuf.String(), Equals, "")
}

func (s *lkTestSuite) TestExtractKernelAssetsUnpacksAndRemoveInRuntimeModeUC20(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// all expected files are created for RoleRunMode bootloader in
	// MockLkFiles

	// ensure we have a valid boot env
	disk := mylog.Check2(disks.DiskFromDeviceName("lk-boot-disk"))


	partuuid := mylog.Check2(disk.FindMatchingPartitionUUIDWithPartLabel("snapbootsel"))


	// also confirm that we can load the backup file partition too
	backupPartuuid := mylog.Check2(disk.FindMatchingPartitionUUIDWithPartLabel("snapbootselbak"))


	bootselPartition := filepath.Join(s.rootdir, "/dev/disk/by-partuuid", partuuid)
	bootselPartitionBackup := filepath.Join(s.rootdir, "/dev/disk/by-partuuid", backupPartuuid)
	env := lkenv.NewEnv(bootselPartition, "", lkenv.V2Run)
	backupEnv := lkenv.NewEnv(bootselPartitionBackup, "", lkenv.V2Run)

	// mock a kernel snap that has a boot.img
	files := [][]string{
		{"boot.img", "I'm the default boot image name"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.

		// now extract
		Check(lk.ExtractKernelAssets(info, snapf))


	// and validate it went to the "boot_a" partition
	bootAPartUUID := mylog.Check2(disk.FindMatchingPartitionUUIDWithPartLabel("boot_a"))

	bootA := filepath.Join(s.rootdir, "/dev/disk/by-partuuid", bootAPartUUID)
	content := mylog.Check2(os.ReadFile(bootA))

	c.Assert(string(content), Equals, "I'm the default boot image name")

	// also validate that bootB is empty
	bootBPartUUID := mylog.Check2(disk.FindMatchingPartitionUUIDWithPartLabel("boot_b"))

	bootB := filepath.Join(s.rootdir, "/dev/disk/by-partuuid", bootBPartUUID)
	content = mylog.Check2(os.ReadFile(bootB))

	c.Assert(content, HasLen, 0)
	mylog.

		// test that boot partition got set
		Check(env.Load())

	bootPart := mylog.Check2(env.GetKernelBootPartition("ubuntu-kernel_42.snap"))

	c.Assert(bootPart, Equals, "boot_a")
	mylog.

		// in the backup too
		Check(backupEnv.Load())
	c.Assert(logbuf.String(), Equals, "")


	bootPart = mylog.Check2(backupEnv.GetKernelBootPartition("ubuntu-kernel_42.snap"))

	c.Assert(bootPart, Equals, "boot_a")
	mylog.

		// now remove the kernel
		Check(lk.RemoveKernelAssets(info))

	mylog.
		// and ensure its no longer available in the boot partitions
		Check(env.Load())

	_ = mylog.Check2(env.GetKernelBootPartition("ubuntu-kernel_42.snap"))
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find kernel %[1]q: no boot image partition has value %[1]q", "ubuntu-kernel_42.snap"))
	mylog.Check(backupEnv.Load())

	// in the backup too
	_ = mylog.Check2(backupEnv.GetKernelBootPartition("ubuntu-kernel_42.snap"))
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find kernel %[1]q: no boot image partition has value %[1]q", "ubuntu-kernel_42.snap"))

	c.Assert(logbuf.String(), Equals, "")
}

func (s *lkTestSuite) TestExtractRecoveryKernelAssetsAtRuntime(c *C) {
	opts := &bootloader.Options{
		// as called when creating a recovery system at runtime
		PrepareImageTime: false,
		Role:             bootloader.RoleRecovery,
	}
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	l := bootloader.NewLk(s.rootdir, opts)

	c.Assert(l, NotNil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"boot.img", "...and I'm an boot image"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))


	relativeRecoverySystemDir := "systems/1234"
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, relativeRecoverySystemDir), 0755), IsNil)
	mylog.Check(l.ExtractRecoveryKernelAssets(relativeRecoverySystemDir, info, snapf))
	c.Assert(err, ErrorMatches, "internal error: extracting recovery kernel assets is not supported for a runtime lk bootloader")
}

// TODO:UC20: when runtime addition (and deletion) of recovery systems is
//            implemented, add tests for that here with lkenv
