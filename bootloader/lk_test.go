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
	"regexp"
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
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

// wedgeBootImageMatrix puts the boot image matrix into the state left behind by
// the duplicate boot image partition bug: the same kernel revision recorded in
// both boot_a and boot_b, which leaves no free boot image partition. The kernel
// is pointed at by snap_kernel, i.e. it is referenced for booting, unless
// referenced is false. It returns the paths to the boot_a and boot_b partitions.
func (s *lkTestSuite) wedgeBootImageMatrix(c *C, kernel string, referenced bool) (env *lkenv.Env, bootA, bootB string) {
	disk, err := disks.DiskFromDeviceName("lk-boot-disk")
	c.Assert(err, IsNil)

	partUUIDFor := func(label string) string {
		partUUID, err := disk.FindMatchingPartitionUUIDWithPartLabel(label)
		c.Assert(err, IsNil)
		return filepath.Join(s.rootdir, "/dev/disk/by-partuuid", partUUID)
	}

	env = lkenv.NewEnv(partUUIDFor("snapbootsel"), "", lkenv.V2Run)
	c.Assert(env.Load(), IsNil)
	c.Assert(env.SetBootPartitionKernel("boot_a", kernel), IsNil)
	c.Assert(env.SetBootPartitionKernel("boot_b", kernel), IsNil)
	if referenced {
		env.Set("snap_kernel", kernel)
	}
	c.Assert(env.Save(), IsNil)

	return env, partUUIDFor("boot_a"), partUUIDFor("boot_b")
}

// installKernelSnap puts a kernel snap shipping the given boot image where the
// snap files of installed kernel revisions are kept.
func (s *lkTestSuite) installKernelSnap(c *C, kernel, bootImg string) {
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, [][]string{
		{"boot.img", bootImg},
	})
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)
	c.Assert(osutil.CopyFile(fn, filepath.Join(dirs.SnapBlobDir, kernel), osutil.CopyFlagOverwrite), IsNil)
}

func (s *lkTestSuite) TestExtractKernelAssetsRepairsDuplicateBootPartitions(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
	s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")

	// both boot image partitions hold the boot image of the kernel, as they
	// would after the duplicate was created by re-extracting the same kernel,
	// but what follows it differs: a boot image is smaller than its partition
	// and extracting it leaves the tail of earlier, larger boot images behind
	c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image, then leftovers of an older one"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("kernel 42 boot image, then leftovers of a different older one"), 0755), IsNil)

	// a device in this state cannot find a free boot image partition
	_, err := env.FindFreeKernelBootPartition("ubuntu-kernel_43.snap")
	c.Assert(err, ErrorMatches, "cannot find free boot image partition")

	// extracting a new kernel repairs the matrix and then succeeds
	files := [][]string{
		{"boot.img", "kernel 43 boot image"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(43),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)
	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)

	c.Check(logbuf.String(), testutil.Contains, "repairing lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in both boot image partitions boot_a and boot_b, freeing boot_b")

	// the old kernel keeps the first of the two boot image partitions, and the
	// new kernel got the freed one
	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_a")
	bootPart, err = env.GetKernelBootPartition("ubuntu-kernel_43.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_b")

	// and the new boot image really was written to the freed partition, over
	// the start of what was there before
	content, err := os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 43 boot image, then leftovers of a different older one")

	// the duplicate is gone for good
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 0)
}

func (s *lkTestSuite) TestExtractKernelAssetsRepairsUnreferencedDuplicateWithDifferingContent(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// nothing points at the duplicated kernel, so the bootloader never looks it
	// up in the matrix and neither of the boot image partitions it is recorded
	// in can be selected for booting
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", false)
	c.Check(env.IsKernelReferenced("ubuntu-kernel_42.snap"), Equals, false)

	// the boot image partitions hold different content, which for a referenced
	// kernel would block the repair, but here there is no boot image to lose
	c.Assert(os.WriteFile(bootA, []byte("some boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("a different boot image"), 0755), IsNil)

	files := [][]string{
		{"boot.img", "kernel 43 boot image"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(43),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)
	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)

	c.Check(logbuf.String(), testutil.Contains, "repairing lk boot image matrix: unreferenced kernel ubuntu-kernel_42.snap is recorded in both boot image partitions boot_a and boot_b, freeing boot_b")

	// the new kernel got a boot image partition and the duplicate is gone
	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_43.snap")
	c.Assert(err, IsNil)
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 0)

	// the repair dropped the redundant reference rather than leaving it for the
	// extraction to overwrite: both references to the unreferenced kernel are
	// gone, one cleared by the repair and one reused for the new kernel
	_, err = env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Check(err, ErrorMatches, `cannot find kernel "ubuntu-kernel_42.snap": no boot image partition has value "ubuntu-kernel_42.snap"`)

	// and the new boot image really was written to it
	bootPartPath := map[string]string{"boot_a": bootA, "boot_b": bootB}[bootPart]
	c.Assert(bootPartPath, Not(Equals), "")
	content, err := os.ReadFile(bootPartPath)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 43 boot image")
}

func (s *lkTestSuite) TestExtractKernelAssetsRepairsDuplicateFreeingPartitionWithoutBootImage(c *C) {
	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	for _, t := range []struct {
		comment      string
		bootA, bootB string
		// keep is the boot image partition that holds the boot image of the
		// kernel, free the one that does not
		keep, free string
	}{{
		comment: "boot_b holds a different boot image",
		bootA:   "kernel 42 boot image",
		bootB:   "a different boot image",
		keep:    "boot_a",
		free:    "boot_b",
	}, {
		// a partition too short to hold the whole boot image does not hold
		// it, even though what it does hold matches the start of it
		comment: "boot image cut short in boot_b",
		bootA:   "kernel 42 boot image",
		bootB:   "kernel 42 boot",
		keep:    "boot_a",
		free:    "boot_b",
	}} {
		logbuf, r := logger.MockLogger()
		defer r()

		env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
		s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")
		c.Assert(os.WriteFile(bootA, []byte(t.bootA), 0755), IsNil)
		c.Assert(os.WriteFile(bootB, []byte(t.bootB), 0755), IsNil)
		bootPartPath := map[string]string{"boot_a": bootA, "boot_b": bootB}

		info, snapf := makeKernelSnap(c, 43, "kernel 43 boot image")
		c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil, Commentf(t.comment))
		c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("repairing lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in boot image partition %[2]s which does not hold its boot image, keeping %[1]s and freeing %[2]s", t.keep, t.free), Commentf(t.comment))

		// the kernel is left recorded only in the boot image partition that
		// holds its boot image, which is left untouched
		c.Assert(env.Load(), IsNil)
		bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
		c.Assert(err, IsNil)
		c.Check(bootPart, Equals, t.keep, Commentf(t.comment))
		content, err := os.ReadFile(bootPartPath[t.keep])
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, "kernel 42 boot image", Commentf(t.comment))

		// and the new kernel got the freed one
		bootPart, err = env.GetKernelBootPartition("ubuntu-kernel_43.snap")
		c.Assert(err, IsNil)
		c.Check(bootPart, Equals, t.free, Commentf(t.comment))
		content, err = os.ReadFile(bootPartPath[t.free])
		c.Assert(err, IsNil)
		c.Check(string(content), testutil.Contains, "kernel 43 boot image", Commentf(t.comment))
	}
}

func (s *lkTestSuite) TestExtractKernelAssetsRepairsDuplicateComparingWholeBootImage(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// a boot image spanning several of the chunks it is compared in
	bootImg := strings.Repeat("kernel 42 boot image ", 10000)
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
	s.installKernelSnap(c, "ubuntu-kernel_42.snap", bootImg)

	// boot_b holds the boot image only up to its very last byte
	c.Assert(os.WriteFile(bootA, []byte(bootImg+"then leftovers"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte(bootImg[:len(bootImg)-1]+"X, then leftovers"), 0755), IsNil)

	info, snapf := makeKernelSnap(c, 43, "kernel 43 boot image")
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)
	c.Check(logbuf.String(), testutil.Contains, "repairing lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in boot image partition boot_b which does not hold its boot image, keeping boot_a and freeing boot_b")

	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_a")
}

func (s *lkTestSuite) TestExtractKernelAssetsRepairsTryKernelFreeingPartitionWithoutBootImage(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// the duplicated kernel is the try kernel of an ongoing refresh rather than
	// the current one, which makes it just as referenced for booting: leave
	// snap_kernel unset so that only snap_try_kernel can account for it
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", false)
	env.Set("snap_try_kernel", "ubuntu-kernel_42.snap")
	c.Assert(env.Save(), IsNil)
	c.Check(env.IsKernelReferenced("ubuntu-kernel_42.snap"), Equals, true)
	s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")

	c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("some boot image"), 0755), IsNil)

	// re-extract the try kernel itself: only snap_kernel keeps a boot image
	// partition from being handed out to a different kernel, which here would
	// let any other kernel take the boot image partition of the try kernel
	info, snapf := makeKernelSnap(c, 42, "kernel 42 boot image")
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)

	c.Check(logbuf.String(), testutil.Contains, "repairing lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in boot image partition boot_b which does not hold its boot image, keeping boot_a and freeing boot_b")

	// the try kernel is still recorded in the boot image partition holding its
	// boot image, so the bootloader can still find it
	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_a")
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 42 boot image")
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 0)
}

// makeKernelSnap makes a kernel snap of the given revision shipping the given
// boot image.
func makeKernelSnap(c *C, revision int, bootImg string) (*snap.Info, snap.Container) {
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, [][]string{
		{"boot.img", bootImg},
	})
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)
	info, err := snap.ReadInfoFromSnapFile(snapf, &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(revision),
	})
	c.Assert(err, IsNil)
	return info, snapf
}

func (s *lkTestSuite) TestExtractKernelAssetsDoesNotRepairDuplicateNotHoldingItsBootImage(c *C) {
	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	noneHolds := "kernel ubuntu-kernel_42.snap is recorded in boot image partitions boot_a, boot_b but none of them holds its boot image"
	for _, t := range []struct {
		comment      string
		bootA, bootB string
		reason       string
	}{{
		// the two boot image partitions agree with each other, but neither
		// holds the boot image of the kernel recorded in them, so there is no
		// telling which kernel the bootloader really boots from either of them
		comment: "identical content that is not the boot image",
		bootA:   "some other boot image",
		bootB:   "some other boot image",
		reason:  noneHolds,
	}, {
		comment: "different content that is not the boot image",
		bootA:   "some other boot image",
		bootB:   "yet another boot image",
		reason:  noneHolds,
	}, {
		// the bootloader boots the kernel from boot_a, which does not hold
		// its boot image, and dropping it would switch the kernel to boot_b,
		// which has never booted the device, outside of try mode
		comment: "only boot_b holds the boot image",
		bootA:   "a different boot image",
		bootB:   "kernel 42 boot image",
		reason:  "kernel ubuntu-kernel_42.snap boots from boot image partition boot_a which does not hold its boot image",
	}} {
		logbuf, r := logger.MockLogger()
		defer r()

		// neither reference may be dropped
		env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
		s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")
		c.Assert(os.WriteFile(bootA, []byte(t.bootA), 0755), IsNil)
		c.Assert(os.WriteFile(bootB, []byte(t.bootB), 0755), IsNil)

		info, snapf := makeKernelSnap(c, 43, "kernel 43 boot image")
		err := lk.ExtractKernelAssets(info, snapf)
		// and the reason why is reported along with the failure, as the
		// device needs to be repaired manually
		c.Assert(err, ErrorMatches, regexp.QuoteMeta("cannot find free boot image partition: cannot repair lk boot image matrix: "+t.reason), Commentf(t.comment))
		c.Check(logbuf.String(), testutil.Contains, "cannot repair lk boot image matrix: "+t.reason+"\n", Commentf(t.comment))
		c.Check(logbuf.String(), Not(testutil.Contains), "repairing", Commentf(t.comment))

		// both boot image partitions are left exactly as they were
		content, err := os.ReadFile(bootA)
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, t.bootA, Commentf(t.comment))
		content, err = os.ReadFile(bootB)
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, t.bootB, Commentf(t.comment))

		// and so is the matrix
		c.Assert(env.Load(), IsNil)
		duplicates, err := env.DuplicateKernelBootPartitions()
		c.Assert(err, IsNil)
		c.Check(duplicates, DeepEquals, map[string][]string{
			"ubuntu-kernel_42.snap": {"boot_a", "boot_b"},
		}, Commentf(t.comment))
	}
}

func (s *lkTestSuite) TestExtractKernelAssetsDoesNotRepairDuplicateWithoutKernelSnap(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// the snap file of the duplicated kernel is not there, so there is nothing
	// to confirm the boot image partitions hold its boot image against
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
	c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("kernel 42 boot image"), 0755), IsNil)

	info, snapf := makeKernelSnap(c, 43, "kernel 43 boot image")
	err := lk.ExtractKernelAssets(info, snapf)
	c.Assert(err, ErrorMatches, `cannot find free boot image partition: cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in more than one boot image partition but its boot image cannot be read: .*ubuntu-kernel_42.snap: no such file or directory`)
	c.Check(logbuf.String(), Matches, `(?s).*cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in more than one boot image partition but its boot image cannot be read: .*ubuntu-kernel_42.snap: no such file or directory.*`)

	c.Assert(env.Load(), IsNil)
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 1)
}

func (s *lkTestSuite) TestExtractKernelAssetsRepairsDuplicateOfKernelBeingExtracted(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// the duplicated kernel is re-extracted, and only boot_a holds its boot
	// image; its snap file is where installed kernel revisions are kept, as
	// that is where it is put before its kernel assets are extracted
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
	s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")
	c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("a different boot image"), 0755), IsNil)

	info, snapf := makeKernelSnap(c, 42, "kernel 42 boot image")
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)
	c.Check(logbuf.String(), testutil.Contains, "repairing lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in boot image partition boot_b which does not hold its boot image, keeping boot_a and freeing boot_b")

	// the kernel stays in the boot image partition holding its boot image,
	// and only there, and was extracted into it again
	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_a")
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 0)
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 42 boot image")

	// the boot image partition that was freed was not written to
	content, err = os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "a different boot image")
}

func (s *lkTestSuite) TestExtractKernelAssetsDuplicateOfKernelBeingExtractedNotBootingFromItsBootImage(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// the duplicated kernel is re-extracted, but boot_a, which the bootloader
	// boots it from, does not hold its boot image, so the repair is refused
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
	s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")
	c.Assert(os.WriteFile(bootA, []byte("some boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("kernel 42 boot image"), 0755), IsNil)

	// which does not stop the kernel from being extracted, into the boot image
	// partition the bootloader boots it from
	info, snapf := makeKernelSnap(c, 42, "kernel 42 boot image")
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)
	c.Check(logbuf.String(), testutil.Contains, "cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap boots from boot image partition boot_a which does not hold its boot image\n")
	c.Check(logbuf.String(), Not(testutil.Contains), "repairing")

	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_a")
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 42 boot image")
	content, err = os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 42 boot image")
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 1)
}

func (s *lkTestSuite) TestExtractKernelAssetsDuplicateOfKernelBeingExtractedWithoutKernelSnap(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// the duplicated kernel is re-extracted, but there is no snap file to
	// read its boot image from, so the repair cannot be done
	env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
	c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("kernel 42 boot image"), 0755), IsNil)

	// which does not stop the kernel from being extracted, into the boot image
	// partition the bootloader already resolves it to
	info, snapf := makeKernelSnap(c, 42, "kernel 42 new boot image")
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)
	c.Check(logbuf.String(), Matches, `(?s).*cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in more than one boot image partition but its boot image cannot be read: .*ubuntu-kernel_42.snap: no such file or directory.*`)

	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_a")
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 42 new boot image")
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 1)
}

func (s *lkTestSuite) TestExtractKernelAssetsDoesNotRepairWhenBootPartitionCannotBeRead(c *C) {
	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r := bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	for _, t := range []struct {
		comment string
		breakIt func(path string)
		errRe   string
	}{{
		comment: "boot_b cannot be opened",
		breakIt: func(path string) {
			c.Assert(os.Remove(path), IsNil)
		},
		errRe: `.*: no such file or directory`,
	}, {
		comment: "boot_b cannot be read",
		breakIt: func(path string) {
			c.Assert(os.Remove(path), IsNil)
			c.Assert(os.Mkdir(path, 0755), IsNil)
		},
		errRe: `cannot read boot image partition boot_b: .*is a directory`,
	}} {
		logbuf, r := logger.MockLogger()
		defer r()

		// boot_a holds the boot image of the kernel, and boot_b would have
		// been freed for not holding it, if only it could have been read
		env, bootA, bootB := s.wedgeBootImageMatrix(c, "ubuntu-kernel_42.snap", true)
		s.installKernelSnap(c, "ubuntu-kernel_42.snap", "kernel 42 boot image")
		c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image"), 0755), IsNil)
		c.Assert(os.WriteFile(bootB, []byte("kernel 42 boot image"), 0755), IsNil)
		t.breakIt(bootB)

		// a read error is not taken for a boot image partition not holding
		// the boot image, and the repair leaves the matrix alone
		info, snapf := makeKernelSnap(c, 43, "kernel 43 boot image")
		err := lk.ExtractKernelAssets(info, snapf)
		c.Assert(err, ErrorMatches, `cannot find free boot image partition: cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in more than one boot image partition but boot image partition boot_b cannot be checked: `+t.errRe, Commentf(t.comment))
		c.Check(logbuf.String(), Matches, `(?s).*cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in more than one boot image partition but boot image partition boot_b cannot be checked: `+t.errRe+`\n`, Commentf(t.comment))
		c.Check(logbuf.String(), Not(testutil.Contains), "repairing", Commentf(t.comment))

		c.Assert(env.Load(), IsNil)
		duplicates, err := env.DuplicateKernelBootPartitions()
		c.Assert(err, IsNil)
		c.Check(duplicates, DeepEquals, map[string][]string{
			"ubuntu-kernel_42.snap": {"boot_a", "boot_b"},
		}, Commentf(t.comment))
		content, err := os.ReadFile(bootA)
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, "kernel 42 boot image", Commentf(t.comment))

		// restore boot_b for the next iteration
		c.Assert(os.RemoveAll(bootB), IsNil)
		c.Assert(os.WriteFile(bootB, nil, 0755), IsNil)
	}
}

func (s *lkTestSuite) TestExtractKernelAssetsDoesNotRepairWhenPreparingImage(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		PrepareImageTime: true,
		Role:             bootloader.RoleSole,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	// the env comes from the gadget, and a duplicate in it is not something
	// left behind on a device to recover from
	env := lkenv.NewEnv(filepath.Join(s.rootdir, "boot", "lk", "snapbootsel.bin"), "", lkenv.V1)
	c.Assert(env.Load(), IsNil)
	c.Assert(env.SetBootPartitionKernel("boot_a", "ubuntu-kernel_41.snap"), IsNil)
	c.Assert(env.SetBootPartitionKernel("boot_b", "ubuntu-kernel_41.snap"), IsNil)
	env.Set("snap_kernel", "ubuntu-kernel_41.snap")
	c.Assert(env.Save(), IsNil)

	info, snapf := makeKernelSnap(c, 42, "kernel 42 boot image")
	err := lk.ExtractKernelAssets(info, snapf)
	c.Assert(err, ErrorMatches, "cannot find free boot image partition")
	c.Check(logbuf.String(), Equals, "")
}

func (s *lkTestSuite) TestExtractKernelAssetsNoRepairNeeded(c *C) {
	logbuf, r := logger.MockLogger()
	defer r()

	opts := &bootloader.Options{
		Role: bootloader.RoleRunMode,
	}
	r = bootloader.MockLkFiles(c, s.rootdir, opts)
	defer r()
	lk := bootloader.NewLk(s.rootdir, opts)
	c.Assert(lk, NotNil)

	files := [][]string{
		{"boot.img", "kernel 42 boot image"},
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

	// extracting into a healthy matrix must not log any repair, and in
	// particular must not be confused by the two boot image partitions both
	// being empty
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)
	c.Check(logbuf.String(), Equals, "")

	// re-extracting the very same kernel reuses its partition rather than
	// creating a duplicate
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)
	c.Check(logbuf.String(), Equals, "")

	disk, err := disks.DiskFromDeviceName("lk-boot-disk")
	c.Assert(err, IsNil)
	partUUID, err := disk.FindMatchingPartitionUUIDWithPartLabel("snapbootsel")
	c.Assert(err, IsNil)
	env := lkenv.NewEnv(filepath.Join(s.rootdir, "/dev/disk/by-partuuid", partUUID), "", lkenv.V2Run)
	c.Assert(env.Load(), IsNil)

	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, HasLen, 0)
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
