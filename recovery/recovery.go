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
	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/logger"
)

const (
	sizeSector = 512
	sizeKB     = 1 << 10
	sizeMB     = 1 << 20
)

func Recover(version string) error {
	logger.Noticef("Run recovery %s", version)

	mntSysRecover := "/mnt/sys-recover"

	// reset env variables if we come from a recover reboot
	if GetKernelParameter("snap_mode") == "recover_reboot" {

		// update recovery mode
		logger.Noticef("after recover reboot: update bootloader env")

		env := grubenv.NewEnv(path.Join(mntSysRecover, "EFI/ubuntu/grubenv"))
		if err := env.Load(); err != nil {
			return fmt.Errorf("cannot load recovery boot vars: %s", err)
		}

		// set mode to normal run
		env.Set("snap_mode", "")

		if err := env.Save(); err != nil {
			return fmt.Errorf("cannot save recovery boot vars: %s", err)
		}
	}

	if err := umount(mntSysRecover); err != nil {
		return fmt.Errorf("cannot unmount recovery: %s", err)
	}

	mntRecovery := "/mnt/recovery"

	if err := mountFilesystem("writable", mntRecovery); err != nil {
		return err
	}

	// shortcut: we can do something better than just bind-mount everything

	extrausers := "/var/lib/extrausers"
	recoveryExtrausers := path.Join(mntRecovery, "system-data", extrausers)
	err := exec.Command("mount", "-o", "bind", recoveryExtrausers, extrausers).Run()
	if err != nil {
		return fmt.Errorf("cannot mount extrausers: %s", err)
	}

	recoveryHome := path.Join(mntRecovery, "user-data")
	err = exec.Command("mount", "-o", "bind", recoveryHome, "/home").Run()
	if err != nil {
		return fmt.Errorf("cannot mount home: %s", err)
	}

	logger.Noticef("done")

	return nil
}

func RecoverReboot(version string) error {
	logger.Noticef("Recover: must reboot to use %s", version)

	// different version, we need to reboot

	mntSysRecover := "/mnt/sys-recover"

	// -- already mounted
	//if err := mountFilesystem("sys-recover", mntSysRecover); err != nil {
	//	return err
	//}

	// update recovery mode
	logger.Noticef("update bootloader env")

	env := grubenv.NewEnv(path.Join(mntSysRecover, "EFI/ubuntu/grubenv"))
	if err := env.Load(); err != nil {
		return fmt.Errorf("cannot load recovery boot vars: %s", err)
	}

	// set version in grubenv
	env.Set("snap_recovery_system", version)

	// set mode to recover_reboot (no chooser)
	env.Set("snap_mode", "recover_reboot")

	if err := env.Save(); err != nil {
		return fmt.Errorf("cannot save recovery boot vars: %s", err)
	}

	if err := umount(mntSysRecover); err != nil {
		return fmt.Errorf("cannot unmount recovery: %s", err)
	}

	// We're on tmpfs, just pull the plug
	if err := Restart(); err != nil {
		logger.Noticef("[sad trombone] cannot reboot: %s", err)
	}

	return fmt.Errorf("something failed")
}

func Install(version string) error {
	logger.Noticef("Install recovery %s", version)
	if err := createWritable(); err != nil {
		logger.Noticef("cannot create writable: %s", err)
		return err
	}

	mntWritable := "/mnt/new-writable"
	mntSysRecover := "/mnt/sys-recover"

	if err := mountFilesystem("writable", mntWritable); err != nil {
		return err
	}

	if err := mountFilesystem("sys-recover", mntSysRecover); err != nil {
		return err
	}

	// Copy selected recovery to the new writable
	core, kernel, err := prepareWritable(mntWritable, mntSysRecover, version)
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
	err := disk.CreatePartition(1000*sizeMB, "writable")
	if err != nil {
		return fmt.Errorf("cannot create new writable: %s", err)
	}

	return nil
}

func mountFilesystem(label string, mountpoint string) error {
	// Create mountpoint
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	// Mount filesystem
	if err := mount(label, mountpoint); err != nil {
		return err
	}

	return nil
}

func prepareWritable(mntWritable, mntSysRecover, version string) (string, string, error) {
	logger.Noticef("Populating new writable")

	seedPath := "system-data/var/lib/snapd/seed"
	snapPath := "system-data/var/lib/snapd/snaps"

	dirs := []string{seedPath, snapPath, "user-data"}
	if err := mkdirs(mntWritable, dirs, 0755); err != nil {
		return "", "", err
	}

	src := path.Join(mntSysRecover, "system", version)
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
	kernel := path.Base(globFile(seedDest, "pc-kernel_*.snap"))

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
	env := grubenv.NewEnv(path.Join(mntSysRecover, "EFI/ubuntu/grubenv"))
	if err := env.Load(); err != nil {
		return fmt.Errorf("cannot load recovery boot vars: %s", err)
	}
	env.Set("snap_mode", "")
	if err := env.Save(); err != nil {
		return fmt.Errorf("cannot save recovery boot vars: %s", err)
	}

	return nil
}

func enableSysrq() error {
	f, err := os.Open("/proc/sys/kernel/sysrq")
	if err != nil {
		return err
	}
	defer f.Close()
	f.Write([]byte("1\n"))
	return nil
}

func Restart() error {
	if err := enableSysrq(); err != nil {
		return err
	}

	f, err := os.OpenFile("/proc/sysrq-trigger", os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	f.Write([]byte("b\n"))

	// look away
	select {}
	return nil
}
