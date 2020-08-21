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
	"github.com/snapcore/snapd/bootloader"
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
			blOpts := &bootloader.Options{NoSlashBoot: true}
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
		Recovery: true,
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
