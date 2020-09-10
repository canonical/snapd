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

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// InitramfsRunModeSelectSnapsToMount returns a map of the snap paths to mount
// for the specified snap types.
func InitramfsRunModeSelectSnapsToMount(
	typs []snap.Type,
	modeenv *Modeenv,
) (map[snap.Type]snap.PlaceInfo, error) {
	var sn snap.PlaceInfo
	var err error
	m := make(map[snap.Type]snap.PlaceInfo)
	for _, typ := range typs {
		// TODO: consider passing a bootStateUpdate20 instead?
		var selectSnapFn func(*Modeenv) (snap.PlaceInfo, error)
		switch typ {
		case snap.TypeBase:
			bs := &bootState20Base{}
			selectSnapFn = bs.selectAndCommitSnapInitramfsMount
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
		sn, err = selectSnapFn(modeenv)
		if err != nil {
			return nil, err
		}

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

	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}

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

	out, err := exec.Command("/sbin/reboot").CombinedOutput()
	if err != nil {
		return osutil.OutputErr(out, err)
	}

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
