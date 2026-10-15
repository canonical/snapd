// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package disks_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/disks"
)

// mockUFSSysfs creates the sysfs tree of the UFS device of a MediaTek Genio
// 1200 EVK: two raw boot logical units, a partitioned one and the RPMB well
// known logical unit, which has a unit descriptor but no block device. The
// kernel does not expose lu_write_protect unless withWriteProtect is set.
func mockUFSSysfs(c *C, withWriteProtect bool) (targetDir string) {
	targetDir = filepath.Join(dirs.SysfsDir, "devices/platform/soc/11270000.ufshci/host0/target0:0:0")

	for _, lu := range []struct {
		hctl, blockDev, bootLunID string
		sectors                   string
		partitions                []string
	}{
		{"0:0:0:0", "sda", "0x01", "8192", nil},
		{"0:0:0:1", "sdb", "0x02", "8192", nil},
		{"0:0:0:2", "sdc", "0x00", "124960768", []string{"sdc1", "sdc2"}},
		{"0:0:0:49476", "", "0x00", "", nil},
	} {
		luDir := filepath.Join(targetDir, lu.hctl)
		unitDescDir := filepath.Join(luDir, "unit_descriptor")
		c.Assert(os.MkdirAll(unitDescDir, 0755), IsNil)
		c.Assert(os.WriteFile(filepath.Join(unitDescDir, "boot_lun_id"), []byte(lu.bootLunID+"\n"), 0644), IsNil)
		if withWriteProtect {
			c.Assert(os.WriteFile(filepath.Join(unitDescDir, "lu_write_protect"), []byte("0x00\n"), 0644), IsNil)
		}
		if lu.blockDev == "" {
			continue
		}
		blockDir := filepath.Join(luDir, "block", lu.blockDev)
		c.Assert(os.MkdirAll(blockDir, 0755), IsNil)
		c.Assert(os.WriteFile(filepath.Join(blockDir, "size"), []byte(lu.sectors+"\n"), 0644), IsNil)
		c.Assert(os.WriteFile(filepath.Join(blockDir, "ro"), []byte("0\n"), 0644), IsNil)
		for _, part := range lu.partitions {
			c.Assert(os.MkdirAll(filepath.Join(blockDir, part), 0755), IsNil)
			c.Assert(os.WriteFile(filepath.Join(blockDir, part, "partition"), []byte("1\n"), 0644), IsNil)
		}
	}
	return targetDir
}

func (s *diskSuite) TestUFSLogicalUnitsForDiskDevicePath(c *C) {
	for _, withWriteProtect := range []bool{false, true} {
		dirs.SetRootDir(c.MkDir())
		targetDir := mockUFSSysfs(c, withWriteProtect)

		var writeProtect *uint8
		if withWriteProtect {
			writeProtect = new(uint8)
		}

		lus, err := disks.UFSLogicalUnitsForDiskDevicePath(filepath.Join(targetDir, "0:0:0:2/block/sdc"))
		c.Assert(err, IsNil)
		c.Check(lus, DeepEquals, []disks.UFSLogicalUnit{
			{
				HCTL:             "0:0:0:0",
				LUN:              0,
				KernelDevicePath: filepath.Join(targetDir, "0:0:0:0/block/sda"),
				KernelDeviceNode: "/dev/sda",
				BootLUNID:        1,
				WriteProtect:     writeProtect,
				SizeInBytes:      4 * 1024 * 1024,
			},
			{
				HCTL:             "0:0:0:1",
				LUN:              1,
				KernelDevicePath: filepath.Join(targetDir, "0:0:0:1/block/sdb"),
				KernelDeviceNode: "/dev/sdb",
				BootLUNID:        2,
				WriteProtect:     writeProtect,
				SizeInBytes:      4 * 1024 * 1024,
			},
			{
				HCTL:             "0:0:0:2",
				LUN:              2,
				KernelDevicePath: filepath.Join(targetDir, "0:0:0:2/block/sdc"),
				KernelDeviceNode: "/dev/sdc",
				BootLUNID:        0,
				WriteProtect:     writeProtect,
				HasPartitions:    true,
				SizeInBytes:      124960768 * 512,
			},
		})
	}
}

func (s *diskSuite) TestUFSLogicalUnitsForDiskDevicePathNotUFS(c *C) {
	mmcDevPath := filepath.Join(dirs.SysfsDir, "devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0")
	c.Assert(os.MkdirAll(mmcDevPath, 0755), IsNil)

	_, err := disks.UFSLogicalUnitsForDiskDevicePath(mmcDevPath)
	c.Assert(err, ErrorMatches, `disk .*/mmcblk0 is not a UFS logical unit`)
}
