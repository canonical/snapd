// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2024 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

var (
	secbootProvisionTPM              = secboot.ProvisionTPM
	secbootSealKeys                  = secboot.SealKeys
	secbootSealKeysWithFDESetupHook  = secboot.SealKeysWithFDESetupHook
	secbootResealKeys                = secboot.ResealKeys
	secbootPCRHandleOfSealedKey      = secboot.PCRHandleOfSealedKey
	secbootReleasePCRResourceHandles = secboot.ReleasePCRResourceHandles

	seedReadSystemEssential = seed.ReadSystemEssential
)

// Hook functions setup by devicestate to support device-specific full
// disk encryption implementations. The state must be locked when these
// functions are called.
var (
	// HasFDESetupHook purpose is to detect if the target kernel has a
	// fde-setup-hook. If kernelInfo is nil the current kernel is checked
	// assuming it is representative` of the target one.
	HasFDESetupHook = func(kernelInfo *snap.Info) (bool, error) {
		return false, nil
	}
	RunFDESetupHook fde.RunSetupHookFunc = func(req *fde.SetupRequest) ([]byte, error) {
		return nil, fmt.Errorf("internal error: RunFDESetupHook not set yet")
	}
)

// MockSecbootResealKeys is only useful in testing. Note that this is a very low
// level call and may need significant environment setup.
func MockSecbootResealKeys(f func(params *secboot.ResealKeysParams) error) (restore func()) {
	osutil.MustBeTestBinary("secbootResealKeys only can be mocked in tests")
	old := secbootResealKeys
	secbootResealKeys = f
	return func() {
		secbootResealKeys = old
	}
}

// MockResealKeyToModeenv is only useful in testing.
func MockResealKeyToModeenv(f func(rootdir string, modeenv *Modeenv, expectReseal bool, unlocker Unlocker) error) (restore func()) {
	osutil.MustBeTestBinary("resealKeyToModeenv only can be mocked in tests")
	old := resealKeyToModeenv
	resealKeyToModeenv = f
	return func() {
		resealKeyToModeenv = old
	}
}

// MockSealKeyToModeenvFlags is used for testing from other packages.
type MockSealKeyToModeenvFlags = sealKeyToModeenvFlags

// MockSealKeyToModeenv is used for testing from other packages.
func MockSealKeyToModeenv(f func(resetter, saveResetter secboot.KeyResetter, model *asserts.Model, modeenv *Modeenv, flags MockSealKeyToModeenvFlags) error) (restore func()) {
	old := sealKeyToModeenv
	sealKeyToModeenv = f
	return func() {
		sealKeyToModeenv = old
	}
}

func bootChainsFileUnder(rootdir string) string {
	return filepath.Join(dirs.SnapFDEDirUnder(rootdir), "boot-chains")
}

func recoveryBootChainsFileUnder(rootdir string) string {
	return filepath.Join(dirs.SnapFDEDirUnder(rootdir), "recovery-boot-chains")
}

type sealKeyToModeenvFlags struct {
	// HasFDESetupHook is true if the kernel has a fde-setup hook to use
	HasFDESetupHook bool
	// FactoryReset indicates that the sealing is happening during factory
	// reset.
	FactoryReset bool
	// SnapsDir is set to provide a non-default directory to find
	// run mode snaps in.
	SnapsDir string
	// SeedDir is the path where to find mounted seed with
	// essential snaps.
	SeedDir string
	// Unlocker is used unlock the snapd state for long operations
	StateUnlocker Unlocker
}

// sealKeyToModeenvImpl seals the supplied keys to the parameters specified
// in modeenv.
// It assumes to be invoked in install mode.
func sealKeyToModeenvImpl(resetter, saveResetter secboot.KeyResetter, model *asserts.Model, modeenv *Modeenv, flags sealKeyToModeenvFlags) error {
	if !IsModeeenvLocked() {
		return fmt.Errorf("internal error: cannot seal without the modeenv lock")
	}

	// make sure relevant locations exist
	for _, p := range []string{
		InitramfsSeedEncryptionKeyDir,
		InitramfsBootEncryptionKeyDir,
		InstallHostFDEDataDir(model),
		InstallHostFDESaveDir,
	} {
		// XXX: should that be 0700 ?
		if err := os.MkdirAll(p, 0755); err != nil {
			return err
		}
	}

	if flags.HasFDESetupHook {
		return sealKeyToModeenvUsingFDESetupHook(resetter, saveResetter, model, modeenv, flags)
	}

	if flags.StateUnlocker != nil {
		relock := flags.StateUnlocker()
		defer relock()
	}
	return sealKeyToModeenvUsingSecboot(resetter, saveResetter, model, modeenv, flags)
}

func runKeySealRequests(resetter secboot.KeyResetter) []secboot.SealKeyRequest {
	return []secboot.SealKeyRequest{
		{
			KeyName:  "ubuntu-data",
			Role:     "run+recover",
			Resetter: resetter,
		},
	}
}

func fallbackKeySealRequests(resetter, saveResetter secboot.KeyResetter, factoryReset bool) []secboot.SealKeyRequest {
	return []secboot.SealKeyRequest{
		{
			KeyName:  "ubuntu-data",
			Role:     "recover",
			SlotName: "default-fallback",
			Resetter: resetter,
		},
		{
			KeyName:  "ubuntu-save",
			Role:     "recover",
			SlotName: "default-fallback",
			Resetter: saveResetter,
		},
	}
}

func sealKeyToModeenvUsingFDESetupHook(resetter, saveResetter secboot.KeyResetter, model *asserts.Model, modeenv *Modeenv, flags sealKeyToModeenvFlags) error {
	params := secboot.SealKeysWithFDESetupHookParams{
		Model:      modeenv.ModelForSealing(),
		AuxKeyFile: filepath.Join(InstallHostFDESaveDir, "aux-key"),
	}
	factoryReset := flags.FactoryReset
	skrs := append(runKeySealRequests(resetter), fallbackKeySealRequests(resetter, saveResetter, factoryReset)...)
	if err := secbootSealKeysWithFDESetupHook(RunFDESetupHook, skrs, &params); err != nil {
		return err
	}

	if err := device.StampSealedKeys(InstallHostWritableDir(model), device.SealingMethodNextGeneration); err != nil {
		return err
	}

	for _, resetter := range []secboot.KeyResetter{
		resetter,
		saveResetter,
	} {
		if resetter != nil {
			if err := resetter.RemoveInstallationKey(); err != nil {
				// This could be a warning
				return err
			}
		}
	}

	return nil
}

func sealKeyToModeenvUsingSecboot(resetter, saveResetter secboot.KeyResetter, model *asserts.Model, modeenv *Modeenv, flags sealKeyToModeenvFlags) error {
	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// TODO:UC20: later the exact kind of bootloaders we expect here might change
		return fmt.Errorf("internal error: cannot seal keys without a trusted assets bootloader")
	}

	includeTryModel := false
	systems := []string{modeenv.RecoverySystem}
	modes := map[string][]string{
		// the system we are installing from is considered current and
		// tested, hence allow both recover and factory reset modes
		modeenv.RecoverySystem: {ModeRecover, ModeFactoryReset},
	}
	recoveryBootChains, err := recoveryBootChainsForSystems(systems, modes, tbl, modeenv, includeTryModel, flags.SeedDir)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}
	logger.Debugf("recovery bootchain:\n%+v", recoveryBootChains)

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}

	// kernel command lines are filled during install
	cmdlines := modeenv.CurrentKernelCommandLines
	runModeBootChains, err := runModeBootChains(rbl, bl, modeenv, cmdlines, flags.SnapsDir)
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}
	logger.Debugf("run mode bootchain:\n%+v", runModeBootChains)

	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChains...))

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

	// the boot chains we seal the fallback object to
	rpbc := toPredictableBootChains(recoveryBootChains)

	runObjectKeyPCRHandle := uint32(secboot.RunObjectPCRPolicyCounterHandle)
	fallbackObjectKeyPCRHandle := uint32(secboot.FallbackObjectPCRPolicyCounterHandle)
	if flags.FactoryReset {
		// during factory reset we may need to rotate the PCR handles,
		// seal the new keys using a new set of handles such that the
		// old sealed ubuntu-save key is still usable, for this we
		// switch between two sets of handles in a round robin fashion,
		// first looking at the PCR handle used by the current fallback
		// key and then using the other set when sealing the new keys;
		// the currently used handles will be released during the first
		// boot of a new run system
		usesAlt, err := usesAltPCRHandles()
		if err != nil {
			return err
		}
		if !usesAlt {
			logger.Noticef("using alternative PCR handles")
			runObjectKeyPCRHandle = secboot.AltRunObjectPCRPolicyCounterHandle
			fallbackObjectKeyPCRHandle = secboot.AltFallbackObjectPCRPolicyCounterHandle
		}
	}

	// we are preparing a new system, hence the TPM needs to be provisioned
	lockoutAuthFile := device.TpmLockoutAuthUnder(InstallHostFDESaveDir)
	tpmProvisionMode := secboot.TPMProvisionFull
	if flags.FactoryReset {
		tpmProvisionMode = secboot.TPMPartialReprovision
	}
	if err := secbootProvisionTPM(tpmProvisionMode, lockoutAuthFile); err != nil {
		return err
	}

	if flags.FactoryReset {
		// it is possible that we are sealing the keys again, after a
		// previously running factory reset was interrupted by a reboot,
		// in which case the PCR handles of the new sealed keys might
		// have already been used
		if err := secbootReleasePCRResourceHandles(runObjectKeyPCRHandle, fallbackObjectKeyPCRHandle); err != nil {
			return err
		}
	}

	// TODO: refactor sealing functions to take a struct instead of so many
	// parameters
	primaryKey, err := sealRunObjectKeys(resetter, pbc, roleToBlName, runObjectKeyPCRHandle)
	if err != nil {
		return err
	}

	err = sealFallbackObjectKeys(resetter, saveResetter, rpbc, primaryKey, roleToBlName, flags.FactoryReset,
		fallbackObjectKeyPCRHandle)
	if err != nil {
		return err
	}

	for _, resetter := range []secboot.KeyResetter{
		resetter,
		saveResetter,
	} {
		if resetter != nil {
			if err := resetter.RemoveInstallationKey(); err != nil {
				// This could be a warning
				return err
			}
		}
	}

	if err := device.StampSealedKeys(InstallHostWritableDir(model), device.SealingMethodNextGeneration); err != nil {
		return err
	}

	installBootChainsPath := bootChainsFileUnder(InstallHostWritableDir(model))
	if err := writeBootChains(pbc, installBootChainsPath, 0); err != nil {
		return err
	}

	installRecoveryBootChainsPath := recoveryBootChainsFileUnder(InstallHostWritableDir(model))
	if err := writeBootChains(rpbc, installRecoveryBootChainsPath, 0); err != nil {
		return err
	}

	return nil
}

func usesAltPCRHandles() (bool, error) {
	saveFallbackKey := device.FallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir)
	// inspect the PCR handle of the ubuntu-save fallback key
	handle, err := secbootPCRHandleOfSealedKey(saveFallbackKey)
	if err != nil {
		return false, err
	}
	logger.Noticef("fallback sealed key %v PCR handle: %#x", saveFallbackKey, handle)
	return handle == secboot.AltFallbackObjectPCRPolicyCounterHandle, nil
}

func sealRunObjectKeys(resetter secboot.KeyResetter, pbc predictableBootChains, roleToBlName map[bootloader.Role]string, pcrHandle uint32) ([]byte, error) {
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return nil, fmt.Errorf("cannot prepare for key sealing: %v", err)
	}

	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:            modelParams,
		PrimaryKey:             nil,
		TPMPolicyAuthKeyFile:   filepath.Join(InstallHostFDESaveDir, "tpm-policy-auth-key"),
		PCRPolicyCounterHandle: pcrHandle,
	}

	logger.Debugf("sealing run key with PCR handle: %#x", sealKeyParams.PCRPolicyCounterHandle)
	// The run object contains only the ubuntu-data key; the ubuntu-save key
	// is then stored inside the encrypted data partition, so that the normal run
	// path only unseals one object because unsealing is expensive.
	// Furthermore, the run object key is stored on ubuntu-boot so that we do not
	// need to continually write/read keys from ubuntu-seed.
	primaryKey, err := secbootSealKeys(runKeySealRequests(resetter), sealKeyParams)
	if err != nil {
		return nil, fmt.Errorf("cannot seal the encryption keys: %v", err)
	}

	return primaryKey, nil
}

func sealFallbackObjectKeys(resetter, saveResetter secboot.KeyResetter, pbc predictableBootChains, primaryKey []byte, roleToBlName map[bootloader.Role]string, factoryReset bool, pcrHandle uint32) error {
	// also seal the keys to the recovery bootchains as a fallback
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key sealing: %v", err)
	}
	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:            modelParams,
		PrimaryKey:             primaryKey,
		PCRPolicyCounterHandle: pcrHandle,
	}
	logger.Debugf("sealing fallback key with PCR handle: %#x", sealKeyParams.PCRPolicyCounterHandle)
	// The fallback object contains the ubuntu-data and ubuntu-save keys. The
	// key files are stored on ubuntu-seed, separate from ubuntu-data so they
	// can be used if ubuntu-data and ubuntu-boot are corrupted or unavailable.

	if _, err := secbootSealKeys(fallbackKeySealRequests(resetter, saveResetter, factoryReset), sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the fallback encryption keys: %v", err)
	}

	return nil
}

var resealKeyToModeenv = resealKeyToModeenvImpl

func ProvideResealKeyToModeenv(f func(rootdir string, modeenv *Modeenv, expectReseal bool, unlocker Unlocker) error) {
	resealKeyToModeenv = f
}

// resealKeyToModeenv reseals the existing encryption key to the
// parameters specified in modeenv.
// It is *very intentional* that resealing takes the modeenv and only
// the modeenv as input. modeenv content is well defined and updated
// atomically.  In particular we want to avoid resealing against
// transient/in-memory information with the risk that successive
// reseals during in-progress operations produce diverging outcomes.
func resealKeyToModeenvImpl(rootdir string, modeenv *Modeenv, expectReseal bool, unlocker Unlocker) error {
	if !IsModeeenvLocked() {
		return fmt.Errorf("internal error: cannot reseal without the modeenv lock")
	}

	method, err := device.SealedKeysMethod(rootdir)
	if err == device.ErrNoSealedKeys {
		// nothing to do
		return nil
	}
	if err != nil {
		return err
	}
	switch method {
	case device.SealingMethodFDESetupHook:
		return ResealKeyToModeenvUsingFDESetupHook(rootdir, modeenv, expectReseal)
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		if unlocker != nil {
			// unlock/relock global state
			defer unlocker()()
		}
		return ResealKeyToModeenvSecboot(rootdir, modeenv, expectReseal)
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}
}

var ResealKeyToModeenvUsingFDESetupHook = resealKeyToModeenvUsingFDESetupHookImpl

func resealKeyToModeenvUsingFDESetupHookImpl(rootdir string, modeenv *Modeenv, expectReseal bool) error {
	var models []secboot.ModelForSealing
	models = append(models, modeenv.ModelForSealing())
	if modeenv.TryModel != "" {
		models = append(models, modeenv.TryModelForSealing())
	}

	keys := []string{
		device.DataSealedKeyUnder(InitramfsBootEncryptionKeyDir),
		device.FallbackDataSealedKeyUnder(InitramfsSeedEncryptionKeyDir),
		device.FallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir),
	}

	primaryKey := filepath.Join(InstallHostFDESaveDir, "aux-key")

	return secboot.ResealKeysWithFDESetupHook(keys, primaryKey, models)
}

// TODO:UC20: allow more than one model to accommodate the remodel scenario
func ResealKeyToModeenvSecboot(rootdir string, modeenv *Modeenv, expectReseal bool) error {
	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// TODO:UC20: later the exact kind of bootloaders we expect here might change
		return fmt.Errorf("internal error: sealed keys but not a trusted assets bootloader")
	}
	// derive the allowed modes for each system mentioned in the modeenv
	modes := modesForSystems(modeenv)

	// the recovery boot chains for the run key are generated for all
	// recovery systems, including those that are being tried; since this is
	// a run key, the boot chains are generated for both models to
	// accommodate the dynamics of a remodel
	includeTryModel := true
	recoveryBootChainsForRunKey, err := recoveryBootChainsForSystems(modeenv.CurrentRecoverySystems, modes, tbl,
		modeenv, includeTryModel, dirs.SnapSeedDir)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains for run key: %v", err)
	}

	// the boot chains for recovery keys include only those system that were
	// tested and are known to be good
	testedRecoverySystems := modeenv.GoodRecoverySystems
	if len(testedRecoverySystems) == 0 && len(modeenv.CurrentRecoverySystems) > 0 {
		// compatibility for systems where good recovery systems list
		// has not been populated yet
		testedRecoverySystems = modeenv.CurrentRecoverySystems[:1]
		logger.Noticef("no good recovery systems for reseal, fallback to known current system %v",
			testedRecoverySystems[0])
	}
	// use the current model as the recovery keys are not expected to be
	// used during a remodel
	includeTryModel = false
	recoveryBootChains, err := recoveryBootChainsForSystems(testedRecoverySystems, modes, tbl, modeenv, includeTryModel, dirs.SnapSeedDir)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}
	cmdlines, err := kernelCommandLinesForResealWithFallback(modeenv)
	if err != nil {
		return err
	}
	runModeBootChains, err := runModeBootChains(rbl, bl, modeenv, cmdlines, "")
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}
	saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDirUnder(rootdir))
	authKeyFile := filepath.Join(saveFDEDir, "tpm-policy-auth-key")

	// reseal the run object
	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChainsForRunKey...))

	needed, nextCount, err := isResealNeeded(pbc, bootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if needed {
		pbcJSON, _ := json.Marshal(pbc)
		logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

		if err := resealRunObjectKeys(pbc, authKeyFile, roleToBlName); err != nil {
			return err
		}
		logger.Debugf("resealing (%d) succeeded", nextCount)

		bootChainsPath := bootChainsFileUnder(rootdir)
		if err := writeBootChains(pbc, bootChainsPath, nextCount); err != nil {
			return err
		}
	} else {
		logger.Debugf("reseal not necessary")
	}

	// reseal the fallback object
	rpbc := toPredictableBootChains(recoveryBootChains)

	var nextFallbackCount int
	needed, nextFallbackCount, err = isResealNeeded(rpbc, recoveryBootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if needed {
		rpbcJSON, _ := json.Marshal(rpbc)
		logger.Debugf("resealing (%d) to recovery boot chains: %s", nextFallbackCount, rpbcJSON)

		if err := resealFallbackObjectKeys(rpbc, authKeyFile, roleToBlName); err != nil {
			return err
		}
		logger.Debugf("fallback resealing (%d) succeeded", nextFallbackCount)

		recoveryBootChainsPath := recoveryBootChainsFileUnder(rootdir)
		if err := writeBootChains(rpbc, recoveryBootChainsPath, nextFallbackCount); err != nil {
			return err
		}
	} else {
		logger.Debugf("fallback reseal not necessary")
	}

	return nil
}

func ResealKeyToModeenvNextGeneration(rootdir string, modeenv *Modeenv, expectReseal bool) error {
	deviceDir := filepath.Join(rootdir, "dev", "mapper")
	deviceNames, err := os.ReadDir(deviceDir)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var devices []string
	for _, de := range deviceNames {
		resolved, err := filepath.EvalSymlinks(filepath.Join(deviceDir, de.Name()))
		if err != nil {
			return err
		}
		devices = append(devices, resolved)
	}

	// build the recovery mode boot chain
	rbl, err := bootloader.Find(InitramfsUbuntuSeedDir, &bootloader.Options{
		Role: bootloader.RoleRecovery,
	})
	if err != nil {
		return fmt.Errorf("cannot find the recovery bootloader: %v", err)
	}
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		// TODO:UC20: later the exact kind of bootloaders we expect here might change
		return fmt.Errorf("internal error: sealed keys but not a trusted assets bootloader")
	}
	// derive the allowed modes for each system mentioned in the modeenv
	modes := modesForSystems(modeenv)

	// the recovery boot chains for the run key are generated for all
	// recovery systems, including those that are being tried; since this is
	// a run key, the boot chains are generated for both models to
	// accommodate the dynamics of a remodel
	includeTryModel := true
	recoveryBootChainsForRunKey, err := recoveryBootChainsForSystems(modeenv.CurrentRecoverySystems, modes, tbl,
		modeenv, includeTryModel, dirs.SnapSeedDir)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains for run key: %v", err)
	}

	// the boot chains for recovery keys include only those system that were
	// tested and are known to be good
	testedRecoverySystems := modeenv.GoodRecoverySystems
	if len(testedRecoverySystems) == 0 && len(modeenv.CurrentRecoverySystems) > 0 {
		// compatibility for systems where good recovery systems list
		// has not been populated yet
		testedRecoverySystems = modeenv.CurrentRecoverySystems[:1]
		logger.Noticef("no good recovery systems for reseal, fallback to known current system %v",
			testedRecoverySystems[0])
	}
	// use the current model as the recovery keys are not expected to be
	// used during a remodel
	includeTryModel = false
	recoveryBootChains, err := recoveryBootChainsForSystems(testedRecoverySystems, modes, tbl, modeenv, includeTryModel, dirs.SnapSeedDir)
	if err != nil {
		return fmt.Errorf("cannot compose recovery boot chains: %v", err)
	}

	// build the run mode boot chains
	bl, err := bootloader.Find(InitramfsUbuntuBootDir, &bootloader.Options{
		Role:        bootloader.RoleRunMode,
		NoSlashBoot: true,
	})
	if err != nil {
		return fmt.Errorf("cannot find the bootloader: %v", err)
	}
	cmdlines, err := kernelCommandLinesForResealWithFallback(modeenv)
	if err != nil {
		return err
	}
	runModeBootChains, err := runModeBootChains(rbl, bl, modeenv, cmdlines, "")
	if err != nil {
		return fmt.Errorf("cannot compose run mode boot chains: %v", err)
	}

	roleToBlName := map[bootloader.Role]string{
		bootloader.RoleRecovery: rbl.Name(),
		bootloader.RoleRunMode:  bl.Name(),
	}

	// reseal the run object
	pbc := toPredictableBootChains(append(runModeBootChains, recoveryBootChainsForRunKey...))

	neededRun, nextCount, err := isResealNeeded(pbc, bootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	modelParams := make(map[string][]*secboot.SealKeyModelParams)
	if neededRun {
		pbcJSON, _ := json.Marshal(pbc)
		logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

		params, err := sealKeyModelParams(pbc, roleToBlName)
		if err != nil {
			return err
		}
		modelParams["run"] = params

	} else {
		logger.Debugf("reseal not necessary")
	}

	// reseal the fallback object
	rpbc := toPredictableBootChains(recoveryBootChains)

	neededRecovery, nextFallbackCount, err := isResealNeeded(rpbc, recoveryBootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if neededRecovery {
		rpbcJSON, _ := json.Marshal(rpbc)
		logger.Debugf("resealing (%d) to recovery boot chains: %s", nextFallbackCount, rpbcJSON)

		params, err := sealKeyModelParams(rpbc, roleToBlName)
		if err != nil {
			return err
		}
		modelParams["recover"] = params
	} else {
		logger.Debugf("fallback reseal not necessary")
	}

	if err := secboot.ResealKeysNextGeneration(devices, modelParams); err != nil {
		return err
	}

	if neededRun {
		bootChainsPath := bootChainsFileUnder(rootdir)
		if err := writeBootChains(pbc, bootChainsPath, nextCount); err != nil {
			return err
		}
	}
	if neededRecovery {
		recoveryBootChainsPath := recoveryBootChainsFileUnder(rootdir)
		if err := writeBootChains(rpbc, recoveryBootChainsPath, nextFallbackCount); err != nil {
			return err
		}
	}

	return nil
}

func resealRunObjectKeys(pbc predictableBootChains, authKeyFile string, roleToBlName map[bootloader.Role]string) error {
	// get model parameters from bootchains
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for key resealing: %v", err)
	}

	// list all the key files to reseal
	keyFiles := []string{device.DataSealedKeyUnder(InitramfsBootEncryptionKeyDir)}

	resealKeyParams := &secboot.ResealKeysParams{
		ModelParams:          modelParams,
		KeyFiles:             keyFiles,
		TPMPolicyAuthKeyFile: authKeyFile,
	}
	if err := secbootResealKeys(resealKeyParams); err != nil {
		return fmt.Errorf("cannot reseal the encryption key: %v", err)
	}

	return nil
}

func resealFallbackObjectKeys(pbc predictableBootChains, authKeyFile string, roleToBlName map[bootloader.Role]string) error {
	// get model parameters from bootchains
	modelParams, err := sealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key resealing: %v", err)
	}

	// list all the key files to reseal
	keyFiles := []string{
		device.FallbackDataSealedKeyUnder(InitramfsSeedEncryptionKeyDir),
		device.FallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir),
	}

	resealKeyParams := &secboot.ResealKeysParams{
		ModelParams:          modelParams,
		KeyFiles:             keyFiles,
		TPMPolicyAuthKeyFile: authKeyFile,
	}
	if err := secbootResealKeys(resealKeyParams); err != nil {
		return fmt.Errorf("cannot reseal the fallback encryption keys: %v", err)
	}

	return nil
}

// recoveryModesForSystems returns a map for recovery modes for recovery systems
// mentioned in the modeenv. The returned map contains both tested and candidate
// recovery systems
func modesForSystems(modeenv *Modeenv) map[string][]string {
	if len(modeenv.GoodRecoverySystems) == 0 && len(modeenv.CurrentRecoverySystems) == 0 {
		return nil
	}

	systemToModes := map[string][]string{}

	// first go through tested recovery systems
	modesForTestedSystem := []string{ModeRecover, ModeFactoryReset}
	// tried systems can only boot to recovery mode
	modesForCandidateSystem := []string{ModeRecover}

	// go through current recovery systems which can contain both tried
	// systems and candidate ones
	for _, sys := range modeenv.CurrentRecoverySystems {
		systemToModes[sys] = modesForCandidateSystem
	}
	// go through recovery systems that were tested and update their modes
	for _, sys := range modeenv.GoodRecoverySystems {
		systemToModes[sys] = modesForTestedSystem
	}
	return systemToModes
}

// TODO:UC20: this needs to take more than one model to accommodate the remodel
// scenario
func recoveryBootChainsForSystems(systems []string, modesForSystems map[string][]string, trbl bootloader.TrustedAssetsBootloader, modeenv *Modeenv, includeTryModel bool, seedDir string) (chains []bootChain, err error) {
	trustedAssets, err := trbl.TrustedAssets()
	if err != nil {
		return nil, err
	}

	chainsForModel := func(model secboot.ModelForSealing) error {
		modelID := modelUniqueID(model)
		for _, system := range systems {
			// get kernel and gadget information from seed
			perf := timings.New(nil)
			seedSystemModel, snaps, err := seedReadSystemEssential(seedDir, system, []snap.Type{snap.TypeKernel, snap.TypeGadget}, perf)
			if err != nil {
				return fmt.Errorf("cannot read system %q seed: %v", system, err)
			}
			if len(snaps) != 2 {
				return fmt.Errorf("cannot obtain recovery system snaps")
			}
			seedModelID := modelUniqueID(seedSystemModel)
			// TODO: the generated unique ID contains the model's
			// sign key ID, consider relaxing this to ignore the key
			// ID when matching models, OTOH we would need to
			// properly take into account key expiration and
			// revocation
			if seedModelID != modelID {
				// could be an incompatible recovery system that
				// is still currently tracked in modeenv
				continue
			}
			seedKernel, seedGadget := snaps[0], snaps[1]
			if snaps[0].EssentialType == snap.TypeGadget {
				seedKernel, seedGadget = seedGadget, seedKernel
			}

			var cmdlines []string
			modes, ok := modesForSystems[system]
			if !ok {
				return fmt.Errorf("internal error: no modes for system %q", system)
			}
			for _, mode := range modes {
				// get the command line for this mode
				cmdline, err := composeCommandLine(currentEdition, mode, system, seedGadget.Path, model)
				if err != nil {
					return fmt.Errorf("cannot obtain kernel command line for mode %q: %v", mode, err)
				}
				cmdlines = append(cmdlines, cmdline)
			}

			var kernelRev string
			if seedKernel.SideInfo.Revision.Store() {
				kernelRev = seedKernel.SideInfo.Revision.String()
			}

			recoveryBootChains, err := trbl.RecoveryBootChains(seedKernel.Path)
			if err != nil {
				return err
			}

			foundChain := false

			// get asset chains
			for _, recoveryBootChain := range recoveryBootChains {
				assetChain, kbf, err := buildBootAssets(recoveryBootChain, modeenv, trustedAssets)
				if err != nil {
					return err
				}
				if assetChain == nil {
					// This chain is not used as
					// it is not in the modeenv,
					// we expect another chain to
					// work.
					continue
				}

				chains = append(chains, bootChain{
					BrandID: model.BrandID(),
					Model:   model.Model(),
					// TODO: test this
					Classic:        model.Classic(),
					Grade:          model.Grade(),
					ModelSignKeyID: model.SignKeyID(),
					AssetChain:     assetChain,
					Kernel:         seedKernel.SnapName(),
					KernelRevision: kernelRev,
					KernelCmdlines: cmdlines,
					kernelBootFile: kbf,
				})

				foundChain = true
			}

			if !foundChain {
				return fmt.Errorf("could not find any valid chain for this model")
			}
		}
		return nil
	}

	if err := chainsForModel(modeenv.ModelForSealing()); err != nil {
		return nil, err
	}

	if modeenv.TryModel != "" && includeTryModel {
		if err := chainsForModel(modeenv.TryModelForSealing()); err != nil {
			return nil, err
		}
	}

	return chains, nil
}

func runModeBootChains(rbl, bl bootloader.Bootloader, modeenv *Modeenv, cmdlines []string, runSnapsDir string) ([]bootChain, error) {
	tbl, ok := rbl.(bootloader.TrustedAssetsBootloader)
	if !ok {
		return nil, fmt.Errorf("recovery bootloader doesn't support trusted assets")
	}
	chains := make([]bootChain, 0, len(modeenv.CurrentKernels))

	trustedAssets, err := tbl.TrustedAssets()
	if err != nil {
		return nil, err
	}

	chainsForModel := func(model secboot.ModelForSealing) error {
		for _, k := range modeenv.CurrentKernels {
			info, err := snap.ParsePlaceInfoFromSnapFileName(k)
			if err != nil {
				return err
			}
			var kernelPath string
			if runSnapsDir == "" {
				kernelPath = info.MountFile()
			} else {
				kernelPath = filepath.Join(runSnapsDir, info.Filename())
			}
			runModeBootChains, err := tbl.BootChains(bl, kernelPath)
			if err != nil {
				return err
			}

			foundChain := false

			for _, runModeBootChain := range runModeBootChains {
				// get asset chains
				assetChain, kbf, err := buildBootAssets(runModeBootChain, modeenv, trustedAssets)
				if err != nil {
					return err
				}
				if assetChain == nil {
					// This chain is not used as
					// it is not in the modeenv,
					// we expect another chain to
					// work.
					continue
				}
				var kernelRev string
				if info.SnapRevision().Store() {
					kernelRev = info.SnapRevision().String()
				}
				chains = append(chains, bootChain{
					BrandID: model.BrandID(),
					Model:   model.Model(),
					// TODO: test this
					Classic:        model.Classic(),
					Grade:          model.Grade(),
					ModelSignKeyID: model.SignKeyID(),
					AssetChain:     assetChain,
					Kernel:         info.SnapName(),
					KernelRevision: kernelRev,
					KernelCmdlines: cmdlines,
					kernelBootFile: kbf,
				})
				foundChain = true
			}

			if !foundChain {
				return fmt.Errorf("could not find any valid chain for this model")
			}
		}
		return nil
	}
	if err := chainsForModel(modeenv.ModelForSealing()); err != nil {
		return nil, err
	}

	if modeenv.TryModel != "" {
		if err := chainsForModel(modeenv.TryModelForSealing()); err != nil {
			return nil, err
		}
	}
	return chains, nil
}

// buildBootAssets takes the BootFiles of a bootloader boot chain and
// produces corresponding bootAssets with the matching current asset
// hashes from modeenv plus it returns separately the last BootFile
// which is for the kernel.
func buildBootAssets(bootFiles []bootloader.BootFile, modeenv *Modeenv, trustedAssets map[string]string) (assets []bootAsset, kernel bootloader.BootFile, err error) {
	if len(bootFiles) == 0 {
		// useful in testing, when mocking is insufficient
		return nil, bootloader.BootFile{}, fmt.Errorf("internal error: cannot build boot assets without boot files")
	}
	assets = make([]bootAsset, len(bootFiles)-1)

	// the last element is the kernel which is not a boot asset
	for i, bf := range bootFiles[:len(bootFiles)-1] {
		path := bf.Path
		name, ok := trustedAssets[path]
		if !ok {
			return nil, kernel, fmt.Errorf("internal error: asset '%s' is not considered a trusted asset for the bootloader", path)
		}
		var hashes []string
		if bf.Role == bootloader.RoleRecovery {
			hashes, ok = modeenv.CurrentTrustedRecoveryBootAssets[name]
		} else {
			hashes, ok = modeenv.CurrentTrustedBootAssets[name]
		}
		if !ok {
			// We have not found an asset for this
			// chain. There are chains expected to not
			// exist. So we return without error.
			// recoveryBootChainsForSystems and
			// runModeBootChains will fail if no chain is
			// found
			return nil, kernel, nil
		}
		assets[i] = bootAsset{
			Role:   bf.Role,
			Name:   name,
			Hashes: hashes,
		}
	}

	return assets, bootFiles[len(bootFiles)-1], nil
}

func sealKeyModelParams(pbc predictableBootChains, roleToBlName map[bootloader.Role]string) ([]*secboot.SealKeyModelParams, error) {
	// seal parameters keyed by unique model ID
	modelToParams := map[string]*secboot.SealKeyModelParams{}
	modelParams := make([]*secboot.SealKeyModelParams, 0, len(pbc))

	for _, bc := range pbc {
		modelForSealing := bc.modelForSealing()
		modelID := modelUniqueID(modelForSealing)
		const expectNew = false
		loadChains, err := bootAssetsToLoadChains(bc.AssetChain, bc.kernelBootFile, roleToBlName, expectNew)
		if err != nil {
			return nil, fmt.Errorf("cannot build load chains with current boot assets: %s", err)
		}

		// group parameters by model, reuse an existing SealKeyModelParams
		// if the model is the same.
		if params, ok := modelToParams[modelID]; ok {
			params.KernelCmdlines = strutil.SortedListsUniqueMerge(params.KernelCmdlines, bc.KernelCmdlines)
			params.EFILoadChains = append(params.EFILoadChains, loadChains...)
		} else {
			param := &secboot.SealKeyModelParams{
				Model:          modelForSealing,
				KernelCmdlines: bc.KernelCmdlines,
				EFILoadChains:  loadChains,
			}
			modelParams = append(modelParams, param)
			modelToParams[modelID] = param
		}
	}

	return modelParams, nil
}

// isResealNeeded returns true when the predictable boot chains provided as
// input do not match the cached boot chains on disk under rootdir.
// It also returns the next value for the reseal count that is saved
// together with the boot chains.
// A hint expectReseal can be provided, it is used when the matching
// is ambigous because the boot chains contain unrevisioned kernels.
func isResealNeeded(pbc predictableBootChains, bootChainsFile string, expectReseal bool) (ok bool, nextCount int, err error) {
	previousPbc, c, err := readBootChains(bootChainsFile)
	if err != nil {
		return false, 0, err
	}

	switch predictableBootChainsEqualForReseal(pbc, previousPbc) {
	case bootChainEquivalent:
		return false, c + 1, nil
	case bootChainUnrevisioned:
		return expectReseal, c + 1, nil
	case bootChainDifferent:
	}
	return true, c + 1, nil
}

func postFactoryResetCleanupSecboot() error {
	// we are inspecting a key which was generated during factory reset, in
	// the simplest case the sealed key generated previously used the main
	// handles, while the current key uses alt handles, hence we need to
	// release the main handles corresponding to the old key
	handles := []uint32{secboot.RunObjectPCRPolicyCounterHandle, secboot.FallbackObjectPCRPolicyCounterHandle}
	usesAlt, err := usesAltPCRHandles()
	if err != nil {
		return fmt.Errorf("cannot inspect fallback key: %v", err)
	}
	if !usesAlt {
		// current fallback key using the main handles, which is
		// possible of there were subsequent factory reset steps,
		// release the alt handles associated with the old key
		handles = []uint32{secboot.AltRunObjectPCRPolicyCounterHandle, secboot.AltFallbackObjectPCRPolicyCounterHandle}
	}
	return secbootReleasePCRResourceHandles(handles...)
}

func postFactoryResetCleanup() error {
	hasHook, err := HasFDESetupHook(nil)
	if err != nil {
		return fmt.Errorf("cannot check for fde-setup hook %v", err)
	}

	saveFallbackKeyFactory := device.FactoryResetFallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir)
	saveFallbackKey := device.FallbackSaveSealedKeyUnder(InitramfsSeedEncryptionKeyDir)
	if err := os.Rename(saveFallbackKeyFactory, saveFallbackKey); err != nil {
		// it is possible that the key file was already renamed if we
		// came back here after an unexpected reboot
		if !os.IsNotExist(err) {
			return fmt.Errorf("cannot rotate fallback key: %v", err)
		}
	}

	if hasHook {
		// TODO: do we need to invoke FDE hook?
		return nil
	}

	if err := postFactoryResetCleanupSecboot(); err != nil {
		return fmt.Errorf("cannot cleanup secboot state: %v", err)
	}

	return nil
}

// resealExpectedByModeenvChange returns true if resealing is expected
// due to modeenv changes, false otherwise. Reseal might not be needed
// if the only change in modeenv is the gadget (if the boot assets
// change that is detected in resealKeyToModeenv() and reseal will
// happen anyway)
func resealExpectedByModeenvChange(m1, m2 *Modeenv) bool {
	auxModeenv := *m2
	auxModeenv.Gadget = m1.Gadget
	return !auxModeenv.deepEqual(m1)
}
