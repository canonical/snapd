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
