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

	// both boot image partitions hold the same boot image, as they would after
	// the duplicate was created by re-extracting the same kernel
	c.Assert(os.WriteFile(bootA, []byte("kernel 42 boot image"), 0755), IsNil)
	c.Assert(os.WriteFile(bootB, []byte("kernel 42 boot image"), 0755), IsNil)

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

	// and the new boot image really was written to the freed partition
	content, err := os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "kernel 43 boot image")

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

func (s *lkTestSuite) TestExtractKernelAssetsDoesNotRepairDuplicateWithDifferingContent(c *C) {
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

	// the boot image partitions hold *different* content, so one of the two
	// references is the only record of a distinct boot image and must not be
	// dropped - clearing it would leave a boot image the bootloader can no
	// longer find and let the extraction below overwrite it
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

	// the extraction fails rather than silently discarding a boot image
	err = lk.ExtractKernelAssets(info, snapf)
	c.Assert(err, ErrorMatches, "cannot find free boot image partition")

	c.Check(logbuf.String(), testutil.Contains, "cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in boot image partitions boot_a and boot_b but their contents differ")

	// both boot image partitions are left exactly as they were
	content, err := os.ReadFile(bootA)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "some boot image")
	content, err = os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "a different boot image")

	// and the matrix is untouched
	c.Assert(env.Load(), IsNil)
	duplicates, err := env.DuplicateKernelBootPartitions()
	c.Assert(err, IsNil)
	c.Check(duplicates, DeepEquals, map[string][]string{
		"ubuntu-kernel_42.snap": {"boot_a", "boot_b"},
	})
}

func (s *lkTestSuite) TestExtractKernelAssetsDoesNotRepairTryKernelWithDifferingContent(c *C) {
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

	// note that the extraction itself still succeeds: it takes boot_a because
	// FindFreeKernelBootPartition() only reserves snap_kernel, which is a
	// separate pre-existing problem. What matters here is that the repair did
	// not drop the reference from boot_b on the way
	c.Assert(lk.ExtractKernelAssets(info, snapf), IsNil)

	c.Check(logbuf.String(), testutil.Contains, "cannot repair lk boot image matrix: kernel ubuntu-kernel_42.snap is recorded in boot image partitions boot_a and boot_b but their contents differ")

	// the try kernel is still recorded in the boot image partition holding its
	// boot image, so the bootloader can still find it
	c.Assert(env.Load(), IsNil)
	bootPart, err := env.GetKernelBootPartition("ubuntu-kernel_42.snap")
	c.Assert(err, IsNil)
	c.Check(bootPart, Equals, "boot_b")
	content, err := os.ReadFile(bootB)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "a different boot image")
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
