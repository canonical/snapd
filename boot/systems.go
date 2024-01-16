// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"strings"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

func dropFromRecoverySystemsList(systemsList []string, systemLabel string) (newList []string, found bool) {
	for idx, sys := range systemsList {
		if sys == systemLabel {
			return append(systemsList[:idx], systemsList[idx+1:]...), true
		}
	}
	return systemsList, false
}

// ClearTryRecoverySystem removes a given candidate recovery system and clears
// the try model in the modeenv state file, then reseals and clears related
// bootloader variables. An empty system label can be passed when the boot
// variables state is inconsistent.
func ClearTryRecoverySystem(dev snap.Device, systemLabel string) error {
	if !dev.HasModeenv() {
		return fmt.Errorf("internal error: recovery systems can only be used on UC20+")
	}
	modeenvLock()
	defer modeenvUnlock()

	return clearTryRecoverySystem(dev, systemLabel)
}

func clearTryRecoverySystem(dev snap.Device, systemLabel string) error {
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

	modified := false
	// we may be repeating the cleanup, in which case the system was already
	// removed from the modeenv and we don't need to rewrite the modeenv
	if updated, found := dropFromRecoverySystemsList(m.CurrentRecoverySystems, systemLabel); found {
		m.CurrentRecoverySystems = updated
		modified = true
	}
	if m.TryModel != "" {
		// recovery system is tried with a matching models
		m.clearTryModel()
		modified = true
	}
	if modified {
		if err := m.Write(); err != nil {
			return err
		}
	}

	// clear both variables, no matter the values they hold
	vars := map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	}
	// try to clear regardless of reseal failing
	blErr := bl.SetBootVars(vars)

	// but we still want to reseal, in case the cleanup did not reach this
	// point before
	const expectReseal = true
	const forceReseal = false
	resealErr := resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal, forceReseal, nil)

	if resealErr != nil {
		return resealErr
	}
	return blErr
}

// SetTryRecoverySystem sets up the boot environment for trying out a recovery
// system with given label in the context of the provided device. The call adds
// the new system to the list of current recovery systems in the modeenv, and
// optionally sets a try model, if the device model is different from the
// current one, which typically can happen during a remodel. Once done, the
// caller should request switching to the given recovery system.
func SetTryRecoverySystem(dev snap.Device, systemLabel string) (err error) {
	if !dev.HasModeenv() {
		return fmt.Errorf("internal error: recovery systems can only be used on UC20+")
	}
	modeenvLock()
	defer modeenvUnlock()

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

	modified := false
	// we could have rebooted before resealing the keys
	if !strutil.ListContains(m.CurrentRecoverySystems, systemLabel) {
		m.CurrentRecoverySystems = append(m.CurrentRecoverySystems, systemLabel)
		modified = true

	}
	// we either have the current device context, in which case the model
	// will match the current model in the modeenv, or a remodel device
	// context carrying a new model, for which we may need to set the try
	// model in the modeenv
	model := dev.Model()
	if modelUniqueID(model) != modelUniqueID(m.ModelForSealing()) {
		// recovery system is tried with a matching model
		m.setTryModel(model)
		modified = true
	}
	if modified {
		if err := m.Write(); err != nil {
			return err
		}
	}

	defer func() {
		if err == nil {
			return
		}
		if cleanupErr := clearTryRecoverySystem(dev, systemLabel); cleanupErr != nil {
			err = fmt.Errorf("%v (cleanup failed: %v)", err, cleanupErr)
		}
	}()

	// even when we unexpectedly reboot after updating the bootenv here, we
	// should not boot into the tried system, as the caller must explicitly
	// request that by other means
	vars := map[string]string{
		"try_recovery_system":    systemLabel,
		"recovery_system_status": "try",
	}
	if err := bl.SetBootVars(vars); err != nil {
		return err
	}

	// until the keys are resealed, even if we unexpectedly boot into the
	// tried system, data will still be inaccessible and the system will be
	// considered as nonoperational
	const expectReseal = true
	const forceReseal = false
	return resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal, forceReseal, nil)
}

type errInconsistentRecoverySystemState struct {
	why string
}

func (e *errInconsistentRecoverySystemState) Error() string { return e.why }
func IsInconsistentRecoverySystemState(err error) bool {
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
	// TryRecoverySystemOutcomeNoneTried indicates a state in which no
	// recovery system has been tried
	TryRecoverySystemOutcomeNoneTried
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

func observeSuccessfulSystems(m *Modeenv) (*Modeenv, error) {
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

// InspectTryRecoverySystemOutcome obtains a tried recovery system status. When
// no recovery system has been tried, the outcome will be
// TryRecoverySystemOutcomeNoneTried. The caller is responsible for clearing the
// bootenv once the status bas been properly acted on.
func InspectTryRecoverySystemOutcome(dev snap.Device) (outcome TryRecoverySystemOutcome, label string, err error) {
	modeenvLock()
	defer modeenvUnlock()

	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	// TODO:UC20: seed may need to be switched to RW
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return TryRecoverySystemOutcomeFailure, "", err
	}

	vars, err := bl.GetBootVars("try_recovery_system", "recovery_system_status")
	if err != nil {
		return TryRecoverySystemOutcomeFailure, "", err
	}
	status := vars["recovery_system_status"]
	trySystem := vars["try_recovery_system"]

	outcome = TryRecoverySystemOutcomeFailure
	switch {
	case status == "" && trySystem == "":
		// simplest case, not trying a system
		return TryRecoverySystemOutcomeNoneTried, "", nil
	case status != "try" && status != "tried":
		// system label is set, but the status is unexpected status
		return TryRecoverySystemOutcomeInconsistent, "", &errInconsistentRecoverySystemState{
			why: fmt.Sprintf("unexpected recovery system status %q", status),
		}
	case trySystem == "":
		// no system set, but we have status
		return TryRecoverySystemOutcomeInconsistent, "", &errInconsistentRecoverySystemState{
			why: fmt.Sprintf("try recovery system is unset but status is %q", status),
		}
	case status == "tried":
		// check that try_recovery_system ended up in the modeenv's
		// CurrentRecoverySystems
		m, err := loadModeenv()
		if err != nil {
			return TryRecoverySystemOutcomeFailure, trySystem, err
		}

		found := false
		for _, sys := range m.CurrentRecoverySystems {
			if sys == trySystem {
				found = true
			}
		}
		if !found {
			return TryRecoverySystemOutcomeFailure, trySystem, &errInconsistentRecoverySystemState{
				why: fmt.Sprintf("recovery system %q was tried, but is not present in the modeenv CurrentRecoverySystems", trySystem),
			}
		}

		outcome = TryRecoverySystemOutcomeSuccess
	}

	return outcome, trySystem, nil
}

// PromoteTriedRecoverySystem promotes the provided recovery system to be
// recognized as a good one, and ensures that the system is present in the list
// of good recovery systems and current recovery systems in modeenv. The
// provided list of tried systems should contain the system in question. If the
// system uses encryption, the keys will updated state. If resealing fails, an
// attempt to restore the previous state is made
func PromoteTriedRecoverySystem(dev snap.Device, systemLabel string, triedSystems []string) (err error) {
	if !dev.HasModeenv() {
		return fmt.Errorf("internal error: recovery systems can only be used on UC20+")
	}
	modeenvLock()
	defer modeenvUnlock()

	if !strutil.ListContains(triedSystems, systemLabel) {
		// system is not among the tried systems
		return fmt.Errorf("system has not been successfully tried")
	}

	m, err := loadModeenv()
	if err != nil {
		return err
	}
	rewriteModeenv := false
	if !strutil.ListContains(m.CurrentRecoverySystems, systemLabel) {
		m.CurrentRecoverySystems = append(m.CurrentRecoverySystems, systemLabel)
		rewriteModeenv = true
	}
	if !strutil.ListContains(m.GoodRecoverySystems, systemLabel) {
		m.GoodRecoverySystems = append(m.GoodRecoverySystems, systemLabel)
		rewriteModeenv = true
	}
	if rewriteModeenv {
		if err := m.Write(); err != nil {
			return err
		}
	}

	const expectReseal = true
	const forceReseal = false
	if err := resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal, forceReseal, nil); err != nil {
		if cleanupErr := dropRecoverySystem(dev, systemLabel); cleanupErr != nil {
			err = fmt.Errorf("%v (cleanup failed: %v)", err, cleanupErr)
		}
		return err
	}
	return nil
}

// DropRecoverySystem drops a provided system from the list of good and current
// recovery systems, updates the modeenv and reseals the keys a needed. Note,
// this call *DOES NOT* clear the boot environment variables.
func DropRecoverySystem(dev snap.Device, systemLabel string) error {
	if !dev.HasModeenv() {
		return fmt.Errorf("internal error: recovery systems can only be used on UC20+")
	}
	modeenvLock()
	defer modeenvUnlock()
	return dropRecoverySystem(dev, systemLabel)
}

func dropRecoverySystem(dev snap.Device, systemLabel string) error {
	m, err := loadModeenv()
	if err != nil {
		return err
	}

	rewriteModeenv := false
	if updatedGood, found := dropFromRecoverySystemsList(m.GoodRecoverySystems, systemLabel); found {
		m.GoodRecoverySystems = updatedGood
		rewriteModeenv = true
	}
	if updatedCurrent, found := dropFromRecoverySystemsList(m.CurrentRecoverySystems, systemLabel); found {
		m.CurrentRecoverySystems = updatedCurrent
		rewriteModeenv = true
	}
	if rewriteModeenv {
		if err := m.Write(); err != nil {
			return err
		}
	}

	const expectReseal = true
	const forceReseal = false
	return resealKeyToModeenv(dirs.GlobalRootDir, m, expectReseal, forceReseal, nil)
}

// MarkRecoveryCapableSystem records a given system as one that we can recover
// from.
func MarkRecoveryCapableSystem(systemLabel string) error {
	opts := &bootloader.Options{
		// setup the recovery bootloader
		Role: bootloader.RoleRecovery,
	}
	// TODO:UC20: seed may need to be switched to RW
	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}
	rbl, ok := bl.(bootloader.RecoveryAwareBootloader)
	if !ok {
		return nil
	}
	vars, err := rbl.GetBootVars("snapd_good_recovery_systems")
	if err != nil {
		return err
	}
	var systems []string
	if vars["snapd_good_recovery_systems"] != "" {
		systems = strings.Split(vars["snapd_good_recovery_systems"], ",")
	}
	// to be consistent with how modeeenv treats good recovery systems, we
	// append the system, also make sure that the system appears last, such
	// that the bootloader may pick the last entry and have a good default
	foundPos := -1
	for idx, sys := range systems {
		if sys == systemLabel {
			foundPos = idx
			break
		}
	}
	if foundPos == -1 {
		// not found in the list
		systems = append(systems, systemLabel)
	} else if foundPos < len(systems)-1 {
		// not a last entry in the list of systems
		systems = append(systems[0:foundPos], systems[foundPos+1:]...)
		systems = append(systems, systemLabel)
	}

	systemsForEnv := strings.Join(systems, ",")
	return rbl.SetBootVars(map[string]string{
		"snapd_good_recovery_systems": systemsForEnv,
	})
}

// UnmarkRecoveryCapableSystem removes a given system from the list of systems
// that we can recover from.
func UnmarkRecoveryCapableSystem(systemLabel string) error {
	opts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}

	bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
	if err != nil {
		return err
	}
	rbl, ok := bl.(bootloader.RecoveryAwareBootloader)
	if !ok {
		return nil
	}
	vars, err := rbl.GetBootVars("snapd_good_recovery_systems")
	if err != nil {
		return err
	}
	var systems []string
	if vars["snapd_good_recovery_systems"] != "" {
		systems = strings.Split(vars["snapd_good_recovery_systems"], ",")
	}

	for idx, sys := range systems {
		if sys == systemLabel {
			systems = append(systems[:idx], systems[idx+1:]...)
			break
		}
	}

	systemsForEnv := strings.Join(systems, ",")
	return rbl.SetBootVars(map[string]string{
		"snapd_good_recovery_systems": systemsForEnv,
	})
}
