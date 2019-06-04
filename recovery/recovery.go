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
	"io/ioutil"
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
		logger.Noticef("cannot create writable: %s", err)
		return err
	}

	mntWritable := "/mnt/new-writable"
	mntSysRecover := "/mnt/sys-recover"

	// Create mountpoints
	mountpoints := []string{mntWritable, mntSysRecover}
	if err := mkdirs("/", mountpoints, 0755); err != nil {
		return err
	}

	// Mount new writable and recovery
	if err := mount("writable", mntWritable); err != nil {
		return err
	}
	if err := mount("sys-recover", mntSysRecover); err != nil {
		return err
	}

	// Copy selected recovery to the new writable
	core, kernel, err := prepareWritable(mntWritable, mntSysRecover)
	if err != nil {
		logger.Noticef("cannot populate writable: %s", err)
		return err
	}

	if err := umount(mntWritable); err != nil {
		return err
	}

	// Update our bootloader
	if err := updateBootloader(mntSysRecover, core, kernel); err != nil {
		logger.Noticef("cannot update bootloader: %s", err)
		return err
	}

	if err := umount(mntSysRecover); err != nil {
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

func prepareWritable(mntWritable, mntSysRecover string) (string, string, error) {
	logger.Noticef("Populating new writable")

	recovery := GetKernelParameter("snap_recovery_system")
	seedPath := "system-data/var/lib/snapd/seed"
	snapPath := "system-data/var/lib/snapd/snaps"

	logger.Noticef("recovery: %s", recovery)

	dirs := []string{seedPath, snapPath, "user-data"}
	if err := mkdirs(mntWritable, dirs, 0755); err != nil {
		return "", "", err
	}

	src := path.Join(mntSysRecover, "system", recovery)
	dest := path.Join(mntWritable, seedPath)

	seedFiles, err := ioutil.ReadDir(src)
	if err != nil {
		return "", "", err
	}
	for _, f := range seedFiles {
		if err := copyTree(path.Join(src, f.Name()), dest); err != nil {
			return "", "", err
		}
	}

	seedDest := path.Join(dest, "snaps")
	core := path.Base(globFile(seedDest, "core18_*.snap"))
	kernel := path.Base(globFile(seedDest, "kernel*.snap"))

	logger.Noticef("core: %s", core)
	logger.Noticef("kernel: %s", kernel)

	err = os.Symlink(path.Join("../seed/snaps", core), path.Join(mntWritable, snapPath, core))
	if err != nil {
		return "", "", err
	}

	err = os.Symlink(path.Join("../seed/snaps", kernel), path.Join(mntWritable, snapPath, kernel))
	if err != nil {
		return "", "", err
	}

	return core, kernel, nil
}

func updateBootloader(mntSysRecover, core, kernel string) error {
	logger.Noticef("Updating bootloader")

	b, err := bootloader.Find()
	if err != nil {
		return err
	}

	bootVars := map[string]string{
		"snap_core":   core,
		"snap_kernel": kernel,
		"snap_mode":   "",
	}

	if err := b.SetBootVars(bootVars); err != nil {
		return fmt.Errorf("cannot set boot vars: %s", err)
	}

	// FIXME: update recovery boot vars
	// must do it in a bootloader-agnostic way
	err = exec.Command("grub-editenv", path.Join(mntSysRecover, "EFI/ubuntu/grubenv"),
		"unset", "snap_mode").Run()
	if err != nil {
		return fmt.Errorf("cannot unset snap mode: %s", err)
	}

	return nil
}
