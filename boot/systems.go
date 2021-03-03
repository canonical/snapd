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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
)

type errInconsistentRecoverySystemState struct {
	why string
}

func (e *errInconsistentRecoverySystemState) Error() string { return e.why }
func IsInconsystemRecoverySystemState(err error) bool {
	_, ok := err.(*errInconsistentRecoverySystemState)
	return ok
}

// InitramfsIsTryingRecoverySystem, typically called while in initramfs of
// recovery mode system, checks whether the boot variables indicate that the
// given recovery system is only being tried. When the state of boot variables
// is inconsistent, eg. status indicates that a recovery system is to be tried,
// but the label is unset, a specific error which can be tested with
// IsInconsystemRecoverySystemState() is returned.
func InitramfsIsTryingRecoverySystem(currentSystemLabel string) (bool, error) {
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
	switch status {
	case "":
		// not trying any recovery systems right now
		return false, nil
	case "try", "tried":
		// both are valid options, where tried may indicate there was an
		// unexpected reboot somewhere along the path of getting back to
		// the run system
	default:
		return false, &errInconsistentRecoverySystemState{
			why: fmt.Sprintf("unexpected recovery system status %q", status),
		}
	}

	trySystem := vars["try_recovery_system"]
	if trySystem == "" {
		// XXX: could we end up with one variable set and the other not?
		return false, &errInconsistentRecoverySystemState{
			why: fmt.Sprintf("try recovery system is unset but status is %q", status),
		}
	}

	if trySystem == currentSystemLabel {
		// we are running a recovery system indicated in the boot
		// variables, which may or may not be considered good at this
		// point, nonetheless we are in recover mode and thus consider
		// the system as being tried

		// note, with status set to 'tried', we may be back to the
		// tried system again, most likely due to an unexpected reboot
		// when coming back to run mode
		return true, nil
	}
	// we may still be running an actual recovery system if such mode was
	// requested
	return false, nil
}

type TryRecoverySystemOutcome int

const (
	TryRecoverySystemOutcomeFailure TryRecoverySystemOutcome = iota
	TryRecoverySystemOutcomeSuccess
	// TryRecoverySystemOutcomeInconsistent indicates that the booted try
	// recovery system state was incorrect and corresponding boot variables
	// need to be cleared
	TryRecoverySystemOutcomeInconsistent
)

// EnsureNextBootToRunModeWithTryRecoverySystemOutcome, typically called while
// in initramfs, updates the boot environment to indicate an outcome of trying
// out a recovery system and sets the system up to boot into run mode. It is up
// to the caller to ensure the status is updated for the right recovery system,
// typically by calling InitramfsIsTryingRecoverySystem beforehand.
func EnsureNextBootToRunModeWithTryRecoverySystemOutcome(outcome TryRecoverySystemOutcome) error {
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
	switch outcome {
	case TryRecoverySystemOutcomeFailure:
		// already set up for this scenario
	case TryRecoverySystemOutcomeSuccess:
		vars["recovery_system_status"] = "tried"
	case TryRecoverySystemOutcomeInconsistent:
		// there may be an unexpected status, or the tried system label
		// is unset, in either case, clear the status
		vars["recovery_system_status"] = ""
	}
	return bl.SetBootVars(vars)
}

func observeSuccessfulSystems(model *asserts.Model, m *Modeenv) (*Modeenv, error) {
	// updates happen in run mode only
	if m.Mode != "run" {
		return m, nil
	}

	// compatibility scenario, no good systems are tracked in modeenv yet,
	// and there is a single entry in the current systems list
	if len(m.GoodRecoverySystems) == 0 && len(m.CurrentRecoverySystems) == 1 {
		newM, err := m.Copy()
		if err != nil {
			return nil, err
		}
		newM.GoodRecoverySystems = []string{m.CurrentRecoverySystems[0]}
		return newM, nil
	}
	return m, nil
}
