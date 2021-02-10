// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/bootloader"
)

// InitramfsTryingRecoverySystem typically called while in initramfs of recovery
// mode system, checks whether the boot variables indicate that the given
// recovery system is only being tried.
func InitramfsTryingRecoverySystem(currentSystemLabel string) (bool, error) {
	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return false, err
	}

	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	if err != nil {
		return false, err
	}

	status := vars["recovery_system_status"]
	if status == "" {
		// not trying any recovery systems right now
		return false, nil
	}

	trySystem := vars["try_recovery_system"]
	if trySystem == "" {
		// XXX: could we end up with one variable set and the other not?
		return false, fmt.Errorf("try recovery system is unset")
	}

	if trySystem == currentSystemLabel {
		// we are running a recovery system indicated in the boot
		// variables, which may or may not be considered good at this
		// point, nonetheless we are in recover mode and thus consider
		// the system as being tried
		return true, nil
	}
	// we may still be running an actual recovery system if such mode was
	// requested
	return false, nil
}

// InitramfsMarkTryRecoverySystemResultForRunMode typically called while in
// initramfs, updates the boot environment to indicate that the outcome of
// trying out a recovery system and sets up the system to boot into run mode. It
// is up to the caller to ensure the status is updated for the right recovery
// system, typically by calling InitramfsTryingRecoverySystem beforehand.
func InitramfsMarkTryRecoverySystemResultForRunMode(success bool) error {
	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	// TODO:UC20: seed may need to be switched to RW
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}
	vars := map[string]string{
		// always going to back to run mode
		"snapd_recovery_mode":    "run",
		"snapd_recovery_system":  "",
		"recovery_system_status": "try",
	}
	if success {
		vars["recovery_system_status"] = "tried"
	}
	return bl.SetBootVars(vars)
}
