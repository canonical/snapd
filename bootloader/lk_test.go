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

	present, err := l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockLkFiles(c, s.rootdir, nil)
	present, err = l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	f, err := bootloader.LkConfigFile(l)
	c.Assert(err, IsNil)
	c.Check(f, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partlabel", "snapbootsel"))
}

func (s *lkTestSuite) TestNewLkPresentChecksBackupStorageToo(c *C) {
	// no files means bl is not present, but we can still create the bl object
	l := bootloader.NewLk(s.rootdir, &bootloader.Options{
		Role: bootloader.RoleSole,
	})
	c.Assert(l, NotNil)
	c.Assert(l.Name(), Equals, "lk")

	present, err := l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now mock just the backup env file
	f, err := bootloader.LkConfigFile(l)
	c.Assert(err, IsNil)
	c.Check(f, Equals, filepath.Join(s.rootdir, "/dev/disk/by-partlabel", "snapbootsel"))

	err = os.MkdirAll(filepath.Dir(f), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(f+"bak", nil, 0644)
	c.Assert(err, IsNil)

	// now the bootloader is present because the backup exists
	present, err = l.Present()
	c.Assert(err, IsNil)
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

	present, err := l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	present, err = l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	f, err := bootloader.LkConfigFile(l)
	c.Assert(err, IsNil)
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

	present, err := l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	present, err = l.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
	c.Check(bootloader.LkRuntimeMode(l), Equals, true)
	f, err := bootloader.LkConfigFile(l)
	c.Assert(err, IsNil)
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
		f, err := bootloader.LkConfigFile(l)
		c.Assert(err, IsNil)
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

		v, err := l.GetBootVars(t.key)
		c.Assert(err, IsNil)
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
		snapf, err := snapfile.Open(fn)
		c.Assert(err, IsNil)

		info, err := snap.ReadInfoFromSnapFile(snapf, si)
		c.Assert(err, IsNil)

		if role == bootloader.RoleSole {
			err = l.ExtractKernelAssets(info, snapf)
		} else {
			// this isn't quite how ExtractRecoveryKernel is typically called,
			// typically it will be called with an actual recovery system dir,
			// but for our purposes this is close enough, we just extract files
			// to some directory
			err = l.ExtractRecoveryKernelAssets(s.rootdir, info, snapf)
		}
		c.Assert(err, IsNil)

		// just boot.img and snapbootsel.bin are there, no kernel.img
		infos, err := os.ReadDir(filepath.Join(s.rootdir, "boot", "lk", ""))
		c.Assert(err, IsNil)
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
	f, err := bootloader.LkConfigFile(l)
	c.Assert(err, IsNil)
	env := lkenv.NewEnv(f, "", lkenv.V1)
	env.Load()
	env.Set("bootimg_file_name", "boot-2.img")
	err = env.Save()
	c.Assert(err, IsNil)

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
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	err = l.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

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
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	// now extract
	err = lk.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// and validate it went to the "boot_a" partition
	bootA := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/boot_a")
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I'm the default boot image name")

	// also validate that bootB is empty
	bootB := filepath.Join(s.rootdir, "/dev/disk/by-partlabel/boot_b")
	content, err = os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Assert(content, HasLen, 0)

	// test that boot partition got set
	err = lkenv.Load()
	c.Assert(err, IsNil)
	bootPart, err := lkenv.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Assert(bootPart, Equals, "boot_a")

	// now remove the kernel
	err = lk.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	// and ensure its no longer available in the boot partitions
	err = lkenv.Load()
	c.Assert(err, IsNil)
	bootPart, err = lkenv.GetKernelBootPartition("ubuntu-kernel_42.snap")
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
	disk, err := disks.DiskFromDeviceName("lk-boot-disk")
	c.Assert(err, IsNil)

	partuuid, err := disk.FindMatchingPartitionUUIDWithPartLabel("snapbootsel")
	c.Assert(err, IsNil)

	// also confirm that we can load the backup file partition too
	backupPartuuid, err := disk.FindMatchingPartitionUUIDWithPartLabel("snapbootselbak")
	c.Assert(err, IsNil)

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
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	// now extract
	err = lk.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// and validate it went to the "boot_a" partition
	bootAPartUUID, err := disk.FindMatchingPartitionUUIDWithPartLabel("boot_a")
	c.Assert(err, IsNil)
	bootA := filepath.Join(s.rootdir, "/dev/disk/by-partuuid", bootAPartUUID)
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "I'm the default boot image name")

	// also validate that bootB is empty
	bootBPartUUID, err := disk.FindMatchingPartitionUUIDWithPartLabel("boot_b")
	c.Assert(err, IsNil)
	bootB := filepath.Join(s.rootdir, "/dev/disk/by-partuuid", bootBPartUUID)
	content, err = os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Assert(content, HasLen, 0)

	// test that boot partition got set
	err = env.Load()
	c.Assert(err, IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Assert(bootPart, Equals, "boot_a")

	// in the backup too
	err = backupEnv.Load()
	c.Assert(logbuf.String(), Equals, "")
	c.Assert(err, IsNil)

	bootPart, err = backupEnv.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Assert(bootPart, Equals, "boot_a")

	// now remove the kernel
	err = lk.RemoveKernelAssets(info)
	c.Assert(err, IsNil)
	// and ensure its no longer available in the boot partitions
	err = env.Load()
	c.Assert(err, IsNil)
	_, err = env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find kernel %[1]q: no boot image partition has value %[1]q", "ubuntu-kernel_42.snap"))
	err = backupEnv.Load()
	c.Assert(err, IsNil)
	// in the backup too
	_, err = backupEnv.GetKernelBootPartition("ubuntu-kernel_42.snap")
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
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	relativeRecoverySystemDir := "systems/1234"
	c.Assert(os.MkdirAll(filepath.Join(s.rootdir, relativeRecoverySystemDir), 0755), IsNil)
	err = l.ExtractRecoveryKernelAssets(relativeRecoverySystemDir, info, snapf)
	c.Assert(err, ErrorMatches, "internal error: extracting recovery kernel assets is not supported for a runtime lk bootloader")
}

// TODO:UC20: when runtime addition (and deletion) of recovery systems is
//            implemented, add tests for that here with lkenv
