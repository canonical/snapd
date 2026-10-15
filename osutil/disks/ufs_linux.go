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

package disks

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// UFSLogicalUnitsForDiskDevicePath returns the normal logical units of the
// UFS device that the disk with the given sysfs device path (as returned by
// Disk.KernelDevicePath()) belongs to. Well known logical units and logical
// units without a block device are skipped. An error is returned if the disk
// is not a logical unit of a UFS device.
func UFSLogicalUnitsForDiskDevicePath(diskDevPath string) ([]UFSLogicalUnit, error) {
	// diskDevPath is of the form:
	// /sys/devices/.../hostH/targetH:C:T/H:C:T:L/block/sdX
	blockDir := filepath.Dir(diskDevPath)
	scsiDevDir := filepath.Dir(blockDir)
	if filepath.Base(blockDir) != "block" || !osutil.IsDirectory(filepath.Join(scsiDevDir, "unit_descriptor")) {
		return nil, fmt.Errorf("disk %s is not a UFS logical unit", diskDevPath)
	}

	targetDir := filepath.Dir(scsiDevDir)
	hctlPrefix := strings.TrimPrefix(filepath.Base(targetDir), "target") + ":"
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return nil, fmt.Errorf("cannot list logical units of UFS device: %v", err)
	}

	var lus []UFSLogicalUnit
	for _, entry := range entries {
		hctl := entry.Name()
		if !strings.HasPrefix(hctl, hctlPrefix) {
			continue
		}
		lun, err := strconv.ParseUint(strings.TrimPrefix(hctl, hctlPrefix), 10, 64)
		if err != nil {
			continue
		}
		// skip well known logical units (REPORT LUNS, UFS device, boot,
		// RPMB), which Linux maps to 0xc1xx
		if lun&0xff00 == 0xc100 {
			continue
		}
		luDir := filepath.Join(targetDir, hctl)
		if !osutil.IsDirectory(filepath.Join(luDir, "unit_descriptor")) {
			continue
		}
		blockDevs, err := filepath.Glob(filepath.Join(luDir, "block", "*"))
		if err != nil {
			return nil, fmt.Errorf("internal error: %v", err)
		}
		if len(blockDevs) != 1 {
			continue
		}

		lu, err := ufsLogicalUnitFromSysfs(luDir, blockDevs[0])
		if err != nil {
			return nil, fmt.Errorf("cannot read UFS logical unit %s: %v", hctl, err)
		}
		lu.HCTL = hctl
		lu.LUN = lun
		lus = append(lus, lu)
	}
	return lus, nil
}

func ufsLogicalUnitFromSysfs(luDir, blockDevPath string) (UFSLogicalUnit, error) {
	lu := UFSLogicalUnit{
		KernelDevicePath: blockDevPath,
		KernelDeviceNode: filepath.Join("/dev", filepath.Base(blockDevPath)),
	}

	bootLunID, err := readSysfsHexUint8(filepath.Join(luDir, "unit_descriptor", "boot_lun_id"))
	if err != nil {
		return lu, err
	}
	lu.BootLUNID = bootLunID

	// lu_write_protect is not exposed by all kernels
	wpPath := filepath.Join(luDir, "unit_descriptor", "lu_write_protect")
	if _, err := os.Stat(wpPath); err == nil {
		wp, err := readSysfsHexUint8(wpPath)
		if err != nil {
			return lu, err
		}
		lu.WriteProtect = &wp
	}

	// size is in 512 bytes sectors, regardless of the logical block size
	sizeStr, err := os.ReadFile(filepath.Join(blockDevPath, "size"))
	if err != nil {
		return lu, err
	}
	sectors, err := strconv.ParseUint(strings.TrimSpace(string(sizeStr)), 10, 64)
	if err != nil {
		return lu, fmt.Errorf("cannot parse size: %v", err)
	}
	lu.SizeInBytes = sectors * 512

	roStr, err := os.ReadFile(filepath.Join(blockDevPath, "ro"))
	if err != nil {
		return lu, err
	}
	lu.ReadOnly = strings.TrimSpace(string(roStr)) != "0"

	// partitions of the block device show up as sub-directories that
	// contain a "partition" file
	parts, err := filepath.Glob(filepath.Join(blockDevPath, "*", "partition"))
	if err != nil {
		return lu, fmt.Errorf("internal error: %v", err)
	}
	lu.HasPartitions = len(parts) != 0

	return lu, nil
}

func readSysfsHexUint8(path string) (uint8, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(content)), 0, 8)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %s: %v", filepath.Base(path), err)
	}
	return uint8(v), nil
}
