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
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootProvisionTPM          = secboot.ProvisionTPM
	secbootSealKeys              = secboot.SealKeys
	secbootSealKeysWithProtector = secboot.SealKeysWithProtector
	secbootFindFreeHandle        = secboot.FindFreeHandle
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
			BootModes:             []string{"run", "recover"},
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
			BootModes:             []string{"recover"},
		},
		{
			BootstrappedContainer: saveKey,
			KeyName:               "ubuntu-save",
			SlotName:              "default-fallback",
			KeyFile:               saveFallbackKey,
			BootModes:             []string{"recover", "factory-reset"},
		},
	}
}

func sealRunObjectKeys(key secboot.BootstrappedContainer, pbc boot.PredictableBootChains, maybePrimaryKey []byte, volumesAuth *device.VolumesAuthOptions, roleToBlName map[bootloader.Role]string, pcrHandle uint32, useTokens bool, keyRole string) ([]byte, error) {
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return nil, fmt.Errorf("cannot prepare for key sealing: %v", err)
	}

	hasClassicModel := false
	for _, m := range modelParams {
		if m.Model.Classic() {
			hasClassicModel = true
			break
		}
	}

	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:                    modelParams,
		PrimaryKey:                     maybePrimaryKey,
		VolumesAuth:                    volumesAuth,
		PCRPolicyCounterHandle:         pcrHandle,
		KeyRole:                        keyRole,
		AllowInsufficientDmaProtection: !hasClassicModel,
	}

	if !useTokens {
		sealKeyParams.TPMPolicyAuthKeyFile = filepath.Join(boot.InstallHostFDESaveDir, "tpm-policy-auth-key")
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

func sealFallbackObjectKeys(key, saveKey secboot.BootstrappedContainer, pbc boot.PredictableBootChains, primaryKey []byte, volumesAuth *device.VolumesAuthOptions, roleToBlName map[bootloader.Role]string, factoryReset bool, pcrHandle uint32, useTokens bool, keyRole string) error {
	// also seal the keys to the recovery bootchains as a fallback
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key sealing: %v", err)
	}

	hasClassicModel := false
	for _, m := range modelParams {
		if m.Model.Classic() {
			hasClassicModel = true
			break
		}
	}

	sealKeyParams := &secboot.SealKeysParams{
		ModelParams:                    modelParams,
		PrimaryKey:                     primaryKey,
		VolumesAuth:                    volumesAuth,
		PCRPolicyCounterHandle:         pcrHandle,
		KeyRole:                        keyRole,
		AllowInsufficientDmaProtection: !hasClassicModel,
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

func sealKeyForBootChainsHook(method device.SealingMethod, key, saveKey secboot.BootstrappedContainer, params *boot.SealKeyForBootChainsParams) error {
	if method != device.SealingMethodFDESetupHook {
		return fmt.Errorf("internal error: sealKeyForBootChainsHook called with unsupported method %q", method)
	}

	sealingParams := secboot.SealKeysWithFDESetupHookParams{
		PrimaryKey: params.PrimaryKey,
	}

	if !params.UseTokens {
		sealingParams.AuxKeyFile = filepath.Join(boot.InstallHostFDESaveDir, "aux-key")
	}

	for _, runChain := range params.RunModeBootChains {
		sealingParams.Model = runChain.ModelForSealing()
		break
	}

	skrs := append(runKeySealRequests(key, params.UseTokens), fallbackKeySealRequests(key, saveKey, params.FactoryReset, params.UseTokens)...)
	if err := secbootSealKeysWithProtector(params.KeyProtectorFactory, skrs, &sealingParams); err != nil {
		return err
	}

	if err := device.StampSealedKeys(params.InstallHostWritableDir, method); err != nil {
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

func sealKeyForBootChainsBackend(method device.SealingMethod, key, saveKey secboot.BootstrappedContainer, primaryKey []byte, volumesAuth *device.VolumesAuthOptions, params *boot.SealKeyForBootChainsParams) error {
	if method == device.SealingMethodFDESetupHook {
		// volumes authentication is not supported when using secboot hooks
		return sealKeyForBootChainsHook(method, key, saveKey, params)
	}

	pbc := boot.ToPredictableBootChains(append(params.RunModeBootChains, params.RecoveryBootChains...))
	// the boot chains we seal the fallback object to
	rpbc := boot.ToPredictableBootChains(params.RecoveryBootChains)

	handle, err := secbootFindFreeHandle()
	if err != nil {
		return err
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

	// TODO:FDEM: refactor sealing functions to take a struct instead of so many
	// parameters
	primaryKey, err = sealRunObjectKeys(key, pbc, primaryKey, volumesAuth, params.RoleToBlName, handle, params.UseTokens, "run+recover")
	if err != nil {
		return err
	}

	err = sealFallbackObjectKeys(key, saveKey, rpbc, primaryKey, volumesAuth, params.RoleToBlName, params.FactoryReset,
		handle, params.UseTokens, "recover")
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

// TODO:FDEM: move those to export_test.go once we have split tests.
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

func MockSecbootSealKeysWithProtector(f func(kf secboot.KeyProtectorFactory, keys []secboot.SealKeyRequest, params *secboot.SealKeysWithFDESetupHookParams) error) (restore func()) {
	old := secbootSealKeysWithProtector
	secbootSealKeysWithProtector = f
	return func() {
		secbootSealKeysWithProtector = old
	}
}
