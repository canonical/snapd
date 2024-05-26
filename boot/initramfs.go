// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package boot

import (
	"os/exec"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap"
)

// InitramfsRunModeSelectSnapsToMount returns a map of the snap paths to mount
// for the specified snap types.
func InitramfsRunModeSelectSnapsToMount(
	typs []snap.Type,
	modeenv *Modeenv,
	rootfsDir string,
) (map[snap.Type]snap.PlaceInfo, error) {
	var sn snap.PlaceInfo

	m := make(map[snap.Type]snap.PlaceInfo)
	for _, typ := range typs {
		// TODO: consider passing a bootStateUpdate20 instead?
		var selectSnapFn func(*Modeenv, string) (snap.PlaceInfo, error)
		switch typ {
		case snap.TypeBase:
			bs := &bootState20Base{}
			selectSnapFn = bs.selectAndCommitSnapInitramfsMount
		case snap.TypeGadget:
			// Do not mount if modeenv does not have gadget entry
			if modeenv.Gadget == "" {
				continue
			}
			selectSnapFn = selectGadgetSnap
		case snap.TypeKernel:
			blOpts := &bootloader.Options{
				Role:        bootloader.RoleRunMode,
				NoSlashBoot: true,
			}
			blDir := InitramfsUbuntuBootDir
			bs := &bootState20Kernel{
				blDir:  blDir,
				blOpts: blOpts,
			}
			selectSnapFn = bs.selectAndCommitSnapInitramfsMount
		}
		sn = mylog.Check2(selectSnapFn(modeenv, rootfsDir))

		m[typ] = sn
	}

	return m, nil
}

// EnsureNextBootToRunMode will mark the bootenv of the recovery bootloader such
// that recover mode is now ready to switch back to run mode upon any reboot.
func EnsureNextBootToRunMode(systemLabel string) error {
	// at the end of the initramfs we need to set the bootenv such that a reboot
	// now at any point will rollback to run mode without additional config or
	// actions

	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}

	bl := mylog.Check2(bootloader.Find(InitramfsUbuntuSeedDir, opts))

	m := map[string]string{
		"snapd_recovery_system": systemLabel,
		"snapd_recovery_mode":   "run",
	}
	return bl.SetBootVars(m)
}

// initramfsReboot triggers a reboot from the initramfs immediately
var initramfsReboot = func() error {
	if osutil.IsTestBinary() {
		panic("initramfsReboot must be mocked in tests")
	}

	out := mylog.Check2(exec.Command("/sbin/reboot").CombinedOutput())

	// reboot command in practice seems to not return, but apparently it is
	// theoretically possible it could return, so to account for this we will
	// loop for a "long" time waiting for the system to be rebooted, and panic
	// after a timeout so that if something goes wrong with the reboot we do
	// exit with some info about the expected reboot
	time.Sleep(10 * time.Minute)
	panic("expected reboot to happen within 10 minutes after calling /sbin/reboot")
}

func MockInitramfsReboot(f func() error) (restore func()) {
	osutil.MustBeTestBinary("initramfsReboot only can be mocked in tests")
	old := initramfsReboot
	initramfsReboot = f
	return func() {
		initramfsReboot = old
	}
}

// InitramfsReboot requests the system to reboot. Can be called while in
// initramfs.
func InitramfsReboot() error {
	return initramfsReboot()
}

// This function implements logic that is usually part of the
// bootloader, but that it is not possible to implement in, for
// instance, piboot. See handling of kernel_status in
// bootloader/assets/data/grub.cfg.
func updateNotScriptableBootloaderStatus(bl bootloader.NotScriptableBootloader) error {
	blVars := mylog.Check2(bl.GetBootVars("kernel_status"))

	curKernStatus := blVars["kernel_status"]
	if curKernStatus == "" {
		return nil
	}

	kVals := mylog.Check2(kcmdline.KeyValues("kernel_status"))

	// "" would be the value for the error case, which at this point is any
	// case different to kernel_status=trying in kernel command line and
	// kernel_status=try in configuration file. Note that kernel_status in
	// the file should be only "try" or empty, and for the latter we should
	// have returned a few lines up.
	newStatus := ""
	if kVals["kernel_status"] == "trying" && curKernStatus == "try" {
		newStatus = "trying"
	}

	logger.Debugf("setting %s kernel_status from %s to %s",
		bl.Name(), curKernStatus, newStatus)
	return bl.SetBootVarsFromInitramfs(map[string]string{"kernel_status": newStatus})
}

// InitramfsRunModeUpdateBootloaderVars updates bootloader variables
// from the initramfs. This is necessary only for piboot at the
// moment.
func InitramfsRunModeUpdateBootloaderVars() error {
	// For very limited bootloaders we need to change the kernel
	// status from the initramfs as we cannot do that from the
	// bootloader
	blOpts := &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	}

	bl := mylog.Check2(bootloader.Find(InitramfsUbuntuBootDir, blOpts))
	if err == nil {
		if nsb, ok := bl.(bootloader.NotScriptableBootloader); ok {
			mylog.Check(updateNotScriptableBootloaderStatus(nsb))
		}
	}

	return nil
}
