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
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/strutil"
)

// clearTryRecoverySystem removes a given candidate recovery system from the
// modeenv state file, reseals and clears related bootloader variables.
func clearTryRecoverySystem(dev Device, systemLabel string) error {
	if !dev.HasModeenv() {
		return fmt.Errorf("internal error: recovery systems can only be used on UC20")
	}

	m, err := loadModeenv()
	if err != nil {
		return err
	}
	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}

	found := false
	for idx, sys := range m.CurrentRecoverySystems {
		if sys == systemLabel {
			found = true
			m.CurrentRecoverySystems = append(m.CurrentRecoverySystems[:idx],
				m.CurrentRecoverySystems[idx+1:]...)
			break
		}
	}
	if found {
		// we may be repeating the cleanup, in which case the system may
		// not be present in modeenv already
		if err := m.Write(); err != nil {
			return err
		}
	}
	// but we still want to reseal, in case the cleanup did not reach this
	// point before
	const expectReseal = true
	resealErr := resealKeyToModeenv(dirs.GlobalRootDir, dev.Model(), m, expectReseal)

	// clear both variables, no matter the values they hold
	vars := map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	}
	// try to clear regardless of reseal failing
	blErr := bl.SetBootVars(vars)

	if resealErr != nil {
		return resealErr
	}
	return blErr
}

// SetTryRecoverySystem sets up the boot environment for trying out a recovery
// system with given label. Once done, the caller should request switching to a
// given recovery system.
func SetTryRecoverySystem(dev Device, systemLabel string) (err error) {
	if !dev.HasModeenv() {
		return fmt.Errorf("internal error: recovery systems can only be used on UC20")
	}

	m, err := loadModeenv()
	if err != nil {
		return err
	}

	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	// TODO:UC20: seed may need to be switched to RW
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}

	// we could have rebooted before resealing the keys
	if !strutil.ListContains(m.CurrentRecoverySystems, systemLabel) {
		m.CurrentRecoverySystems = append(m.CurrentRecoverySystems, systemLabel)
	}
	if err := m.Write(); err != nil {
		return err
	}

	defer func() {
		if err == nil {
			return
		}
		if cleanupErr := clearTryRecoverySystem(dev, systemLabel); cleanupErr != nil {
			err = fmt.Errorf("%v (cleanup failed: %v)", err, cleanupErr)
		}
	}()
	const expectReseal = true
	if err := resealKeyToModeenv(dirs.GlobalRootDir, dev.Model(), m, expectReseal); err != nil {
		return err
	}
	vars := map[string]string{
		"try_recovery_system":    systemLabel,
		"recovery_system_status": "try",
	}
	return bl.SetBootVars(vars)
}

// MaybeMarkTryRecoverySystemSuccessful updates the boot environment to indicate
// that the candidate recovery system of a matching label has successfully
// booted up to a point that this code can be called and the health check
// executed inside the system indicated no errors. Returns true if the candidate
// recovery system is the same as current, false when otherwise or when the
// state cannot be determined due to errors. Note, it is possible to get true
// and an error, if the health check of the current system failed or bootloader
// variables cannot be updated.
func MaybeMarkTryRecoverySystemSuccessful(currentSystemLabel string, healthCheck func() error) (isCurrentTryRecovery bool, err error) {
	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	// TODO:UC20: seed may need to be switched to RW
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

	if trySystem != currentSystemLabel {
		// this may still be ok, eg. if we're running the actual recovery system
		return false, nil
	}

	if status == "tried" {
		// the current recovery system has already been tried and worked
		return true, nil
	}

	if healthCheck != nil {
		if err := healthCheck(); err != nil {
			return true, fmt.Errorf("system health check failed: %v", err)
		}
	}

	tried := map[string]string{
		"recovery_system_status": "tried",
	}
	return true, bl.SetBootVars(tried)
}
