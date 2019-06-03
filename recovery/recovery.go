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

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/logger"
)

const (
	sizeSector = 512
)

func Install() error {
	logger.Noticef("Execute install actions")
	if err := createWritable(); err != nil {
		logger.Noticef("fail: %s", err)
		return err
	}

	if err := copyWritableData(); err != nil {
		logger.Noticef("fail: %s", err)
		return err
	}

	if err := updateBootloader(); err != nil {
		logger.Noticef("fail: %s", err)
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
		return err
	}

	return nil
}

func copyWritableData() error {
	logger.Noticef("Populating new writable")
	if err := os.Mkdir("/mnt/new_writable", 0755); err != nil {
		return err
	}

	if err := mount("writable", "/mnt/new_writable"); err != nil {
		return err
	}

	dirs := []string{"system-data", "user-data"}
	if err := mkdirs("/mnt/new_writable", dirs, 0755); err != nil {
		return err
	}

	dirs = []string{"boot", "etc", "snap", "var"}
	if err := mkdirs("/mnt/new_writable", dirs, 0755); err != nil {
		return err
	}

	if err := mkdirs("/mnt/new_writable", []string{"root"}, 0700); err != nil {
		return err
	}

	if err := copyTree("/writable/etc", "/mnt/new_writable/etc"); err != nil {
		return err
	}

	if err := copyTree("/writable/root", "/mnt/new_writable/root"); err != nil {
		return err
	}

	if err := copyTree("/writable/var", "/mnt/new_writable/var"); err != nil {
		return err
	}

	if err := umount("/mnt/new_writable"); err != nil {
		return err
	}

	return nil
}

func updateBootloader() error {
	logger.Noticef("Updating bootloader")
	b, err := bootloader.Find()
	if err != nil {
		return err
	}
	if err := b.SetBootVars(map[string]string{"snap_mode": ""}); err != nil {
		return err
	}
	return nil
}

func mkdirs(base string, dirlist []string, mode os.FileMode) error {
	for _, dir := range dirlist {
		logger.Noticef("mkdir %s/%s", base, dir)
		if err := os.Mkdir(path.Join(base, dir), mode); err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	// FIXME
	if err := exec.Command("cp", "-rap", src, dst).Run(); err != nil {
		return err
	}
	return nil
}
