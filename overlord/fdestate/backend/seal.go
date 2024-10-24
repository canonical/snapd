// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package backend

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootProvisionTPM              = secboot.ProvisionTPM
	secbootSealKeys                  = secboot.SealKeys
	secbootSealKeysWithFDESetupHook  = secboot.SealKeysWithFDESetupHook
	secbootReleasePCRResourceHandles = secboot.ReleasePCRResourceHandles
)

// Hook functions setup by devicestate to support device-specific full
// disk encryption implementations. The state must be locked when these
// functions are called.
var (
	RunFDESetupHook fde.RunSetupHookFunc = func(req *fde.SetupRequest) ([]byte, error) {
		return nil, fmt.Errorf("internal error: RunFDESetupHook not set yet")
	}
)

func runKeySealRequests(key secboot.BootstrappedContainer, useTokens bool) []secboot.SealKeyRequest {
	var keyFile string
	if !useTokens {
		keyFile = device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir)
	}
	return []secboot.SealKeyRequest{
		{
			BootstrappedContainer: key,
			KeyName:               "ubuntu-data",
			SlotName:              "default",
			KeyFile:               keyFile,
		},
	}
}

func fallbackKeySealRequests(key, saveKey secboot.BootstrappedContainer, factoryReset bool, useTokens bool) []secboot.SealKeyRequest {
	var dataFallbackKey, saveFallbackKey string
	if !useTokens {
		dataFallbackKey = device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)

		if factoryReset {
			// factory reset uses alternative sealed key location, such that
			// until we boot into the run mode, both sealed keys are present
			// on disk
			saveFallbackKey = device.FactoryResetFallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
		} else {
			saveFallbackKey = device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)
		}
	}

	return []secboot.SealKeyRequest{
		{
			BootstrappedContainer: key,
			KeyName:               "ubuntu-data",
			SlotName:              "default-fallback",
			KeyFile:               dataFallbackKey,
		},
		{
			BootstrappedContainer: saveKey,
			KeyName:               "ubuntu-save",
			SlotName:              "default-fallback",
			KeyFile:               saveFallbackKey,
		},
	}
}

func sealRunObjectKeys(key secboot.BootstrappedContainer, pbc boot.PredictableBootChains, roleToBlName map[bootloader.Role]string, pcrHandle uint32, useTokens bool) ([]byte, error) {
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return nil, fmt.Errorf("cannot prepare for key sealing: %v", err)
	}

	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:            modelParams,
		PrimaryKey:             nil,
		TPMPolicyAuthKeyFile:   filepath.Join(boot.InstallHostFDESaveDir, "tpm-policy-auth-key"),
		PCRPolicyCounterHandle: pcrHandle,
	}

	logger.Debugf("sealing run key with PCR handle: %#x", sealKeyParams.PCRPolicyCounterHandle)
	// The run object contains only the ubuntu-data key; the ubuntu-save key
	// is then stored inside the encrypted data partition, so that the normal run
	// path only unseals one object because unsealing is expensive.
	// Furthermore, the run object key is stored on ubuntu-boot so that we do not
	// need to continually write/read keys from ubuntu-seed.
	primaryKey, err := secbootSealKeys(runKeySealRequests(key, useTokens), sealKeyParams)
	if err != nil {
		return nil, fmt.Errorf("cannot seal the encryption keys: %v", err)
	}

	return primaryKey, nil
}

func sealFallbackObjectKeys(key, saveKey secboot.BootstrappedContainer, pbc boot.PredictableBootChains, primaryKey []byte, roleToBlName map[bootloader.Role]string, factoryReset bool, pcrHandle uint32, useTokens bool) error {
	// also seal the keys to the recovery bootchains as a fallback
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
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

	if _, err := secbootSealKeys(fallbackKeySealRequests(key, saveKey, factoryReset, useTokens), sealKeyParams); err != nil {
		return fmt.Errorf("cannot seal the fallback encryption keys: %v", err)
	}

	return nil
}

func sealKeyForBootChainsHook(key, saveKey secboot.BootstrappedContainer, params *boot.SealKeyForBootChainsParams) error {
	sealingParams := secboot.SealKeysWithFDESetupHookParams{}

	for _, runChain := range params.RunModeBootChains {
		sealingParams.Model = runChain.ModelForSealing()
		break
	}

	skrs := append(runKeySealRequests(key, params.UseTokens), fallbackKeySealRequests(key, saveKey, params.FactoryReset, params.UseTokens)...)
	if err := secbootSealKeysWithFDESetupHook(RunFDESetupHook, skrs, &sealingParams); err != nil {
		return err
	}

	if err := device.StampSealedKeys(params.InstallHostWritableDir, device.SealingMethodFDESetupHook); err != nil {
		return err
	}

	for _, container := range []secboot.BootstrappedContainer{
		key,
		saveKey,
	} {
		if container != nil {
			if err := container.RemoveBootstrapKey(); err != nil {
				// This could be a warning
				return err
			}
		}
	}

	return nil
}

func sealKeyForBootChainsBackend(method device.SealingMethod, key, saveKey secboot.BootstrappedContainer, params *boot.SealKeyForBootChainsParams) error {
	if method == device.SealingMethodFDESetupHook {
		return sealKeyForBootChainsHook(key, saveKey, params)
	}

	pbc := boot.ToPredictableBootChains(append(params.RunModeBootChains, params.RecoveryBootChains...))
	// the boot chains we seal the fallback object to
	rpbc := boot.ToPredictableBootChains(params.RecoveryBootChains)

	runObjectKeyPCRHandle := uint32(secboot.RunObjectPCRPolicyCounterHandle)
	fallbackObjectKeyPCRHandle := uint32(secboot.FallbackObjectPCRPolicyCounterHandle)
	if params.FactoryReset {
		// during factory reset we may need to rotate the PCR handles,
		// seal the new keys using a new set of handles such that the
		// old sealed ubuntu-save key is still usable, for this we
		// switch between two sets of handles in a round robin fashion,
		// first looking at the PCR handle used by the current fallback
		// key and then using the other set when sealing the new keys;
		// the currently used handles will be released during the first
		// boot of a new run system
		usesAlt, err := boot.UsesAltPCRHandles()
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
	lockoutAuthFile := device.TpmLockoutAuthUnder(boot.InstallHostFDESaveDir)
	tpmProvisionMode := secboot.TPMProvisionFull
	if params.FactoryReset {
		tpmProvisionMode = secboot.TPMPartialReprovision
	}
	if err := secbootProvisionTPM(tpmProvisionMode, lockoutAuthFile); err != nil {
		return err
	}

	if params.FactoryReset {
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
	primaryKey, err := sealRunObjectKeys(key, pbc, params.RoleToBlName, runObjectKeyPCRHandle, params.UseTokens)
	if err != nil {
		return err
	}

	err = sealFallbackObjectKeys(key, saveKey, rpbc, primaryKey, params.RoleToBlName, params.FactoryReset,
		fallbackObjectKeyPCRHandle, params.UseTokens)
	if err != nil {
		return err
	}

	for _, container := range []secboot.BootstrappedContainer{
		key,
		saveKey,
	} {
		if container != nil {
			if err := container.RemoveBootstrapKey(); err != nil {
				// This could be a warning
				return err
			}
		}
	}

	if err := device.StampSealedKeys(params.InstallHostWritableDir, device.SealingMethodTPM); err != nil {
		return err
	}

	installBootChainsPath := BootChainsFileUnder(params.InstallHostWritableDir)
	if err := boot.WriteBootChains(pbc, installBootChainsPath, 0); err != nil {
		return err
	}

	installRecoveryBootChainsPath := RecoveryBootChainsFileUnder(params.InstallHostWritableDir)
	if err := boot.WriteBootChains(rpbc, installRecoveryBootChainsPath, 0); err != nil {
		return err
	}

	return nil
}

func init() {
	boot.SealKeyForBootChains = sealKeyForBootChainsBackend
}

// TODO move those to export_test.go once we have split tests.
func MockSecbootProvisionTPM(f func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error) (restore func()) {
	old := secbootProvisionTPM
	secbootProvisionTPM = f
	return func() {
		secbootProvisionTPM = old
	}
}

func MockSecbootSealKeys(f func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) ([]byte, error)) (restore func()) {
	old := secbootSealKeys
	secbootSealKeys = f
	return func() {
		secbootSealKeys = old
	}
}

func MockSecbootSealKeysWithFDESetupHook(f func(runHook fde.RunSetupHookFunc, keys []secboot.SealKeyRequest, params *secboot.SealKeysWithFDESetupHookParams) error) (restore func()) {
	old := secbootSealKeysWithFDESetupHook
	secbootSealKeysWithFDESetupHook = f
	return func() {
		secbootSealKeysWithFDESetupHook = old
	}
}

func MockRunFDESetupHook(f fde.RunSetupHookFunc) (restore func()) {
	oldRunFDESetupHook := RunFDESetupHook
	RunFDESetupHook = f
	return func() { RunFDESetupHook = oldRunFDESetupHook }
}
