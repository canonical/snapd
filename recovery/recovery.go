// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

// Package recovery implements core recovery and install
package recovery

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/snapcore/snapd/logger"
)

const (
	sizeSector = 512
)

func Install() error {
	logger.Noticef("Execute install actions")
	if err := createWritable(); err != nil {
		logger.Noticef("cannot create writable: %s", err)
		return err
	}

	if err := copyWritableData(); err != nil {
		logger.Noticef("cannot populate writable: %s", err)
		return err
	}

	if err := updateBootloader(); err != nil {
		logger.Noticef("cannot update bootloader: %s", err)
		return err
	}

	return nil
}

func createWritable() error {
	logger.Noticef("Creating new writable")
	disk := &DiskDevice{}
	if err := disk.FindFromPartLabel("system-boot"); err != nil {
		return fmt.Errorf("cannot determine boot device: %s", err)
	}

	// FIXME: get values from gadget, system
	err := disk.CreatePartition(4, 4504*sizeSector, (4504+3160)*sizeSector, "writable")
	if err != nil {
		return fmt.Errorf("cannot create new writable: %s", err)
	}

	return nil
}

func copyWritableData() error {
	logger.Noticef("Populating new writable")
	if err := os.Mkdir("/mnt/new_writable", 0755); err != nil {
		return fmt.Errorf("cannot create writable mountpoint: %s", err)
	}

	if err := mount("writable", "/mnt/new_writable"); err != nil {
		return err
	}

	dirs := []string{"system-data", "user-data"}
	if err := mkdirs("/mnt/new_writable", dirs, 0755); err != nil {
		return err
	}

	//dirs = []string{"boot", "snap"}
	//if err := mkdirs("/mnt/new_writable/system-data", dirs, 0755); err != nil {
	//	return err
	//}

	//if err := mkdirs("/mnt/new_writable", []string{"root"}, 0700); err != nil {
	//	return err
	//}

	src := "/writable/system-data/"
	dest := "/mnt/new_writable/system-data/"

	if err := copyTree(src, dest); err != nil {
		return err
	}

	logger.Noticef("Unmount new writable")
	if err := umount("/mnt/new_writable"); err != nil {
		return err
	}

	return nil
}

func updateBootloader() error {
	logger.Noticef("Updating bootloader")
	// FIXME: do ir in a bootloader-agnostic way, can't use bootloader.Find() as is
	// because we have to update the recovery bootloader
	//
	//b, err := bootloader.Find()
	//if err != nil {
	//	return err
	//}
	//if err := b.SetBootVars(map[string]string{"snap_mode": ""}); err != nil {
	//	return err
	//}

	if err := os.Mkdir("/mnt/sys-recover", 0755); err != nil {
		return fmt.Errorf("cannot create system recovery mountpoint: %s", err)
	}

	if err := mount("sys-recover", "/mnt/sys-recover"); err != nil {
		return err
	}
	err := exec.Command("grub-editenv", "/mnt/sys-recover/EFI/ubuntu/grubenv", "unset", "snap_mode").Run()
	if err != nil {
		return fmt.Errorf("cannot unset snap mode: %s", err)
	}

	logger.Noticef("Unmount system recovery")
	if err := umount("/mnt/sys-recover"); err != nil {
		return err
	}

	return nil
}

func mkdirs(base string, dirlist []string, mode os.FileMode) error {
	for _, dir := range dirlist {
		logger.Noticef("mkdir %s/%s", base, dir)
		if err := os.Mkdir(path.Join(base, dir), mode); err != nil {
			return fmt.Errorf("cannot create directory %s/%s: %s", base, dir, err)
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	// FIXME
	logger.Noticef("copy tree from %s to %s", src, dst)
	if err := exec.Command("rsync", "-azlx", src, dst).Run(); err != nil {
		return fmt.Errorf("cannot copy tree from %s to %s: %s", src, dst, err)
	}
	return nil
}
