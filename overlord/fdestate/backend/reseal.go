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
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootResealKeys                = secboot.ResealKeys
	secbootBuildPCRProtectionProfile = secboot.BuildPCRProtectionProfile
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

func MockSecbootBuildPCRProtectionProfile(f func(modelParams []*secboot.SealKeyModelParams) (secboot.SerializedPCRProfile, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootBuildPCRProtectionProfile only can be mocked in tests")
	old := secbootBuildPCRProtectionProfile
	secbootBuildPCRProtectionProfile = f
	return func() {
		secbootBuildPCRProtectionProfile = old
	}
}

type StateUpdater func(role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error

// ResealKeyForBootChains reseals disk encryption keys with the given bootchains.
func ResealKeyForBootChains(updateState StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
	switch method {
	case device.SealingMethodFDESetupHook:
		// FIXME: do something
		return nil
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}

	saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDirUnder(rootdir))
	authKeyFile := filepath.Join(saveFDEDir, "tpm-policy-auth-key")

	// reseal the run object
	pbc := boot.ToPredictableBootChains(append(params.RunModeBootChains, params.RecoveryBootChainsForRunKey...))

	needed, nextCount, err := boot.IsResealNeeded(pbc, BootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if needed {
		pbcJSON, _ := json.Marshal(pbc)
		logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

		if err := resealRunObjectKeys(updateState, pbc, authKeyFile, params.RoleToBlName); err != nil {
			return err
		}

		logger.Debugf("resealing (%d) succeeded", nextCount)

		bootChainsPath := BootChainsFileUnder(rootdir)
		if err := boot.WriteBootChains(pbc, bootChainsPath, nextCount); err != nil {
			return err
		}
	} else {
		logger.Debugf("reseal not necessary")
	}

	// reseal the fallback object
	rpbc := boot.ToPredictableBootChains(params.RecoveryBootChains)

	var nextFallbackCount int
	needed, nextFallbackCount, err = boot.IsResealNeeded(rpbc, RecoveryBootChainsFileUnder(rootdir), expectReseal)
	if err != nil {
		return err
	}
	if needed {
		rpbcJSON, _ := json.Marshal(rpbc)
		logger.Debugf("resealing (%d) to recovery boot chains: %s", nextFallbackCount, rpbcJSON)

		if err := resealFallbackObjectKeys(updateState, rpbc, authKeyFile, params.RoleToBlName); err != nil {
			return err
		}
		logger.Debugf("fallback resealing (%d) succeeded", nextFallbackCount)

		recoveryBootChainsPath := RecoveryBootChainsFileUnder(rootdir)
		if err := boot.WriteBootChains(rpbc, recoveryBootChainsPath, nextFallbackCount); err != nil {
			return err
		}
	} else {
		logger.Debugf("fallback reseal not necessary")
	}

	return nil
}

func resealRunObjectKeys(updateState StateUpdater, pbc boot.PredictableBootChains, authKeyFile string, roleToBlName map[bootloader.Role]string) error {
	// get model parameters from bootchains
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for key resealing: %v", err)
	}

	numModels := len(modelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	pcrProfile, err := secbootBuildPCRProtectionProfile(modelParams)
	if err != nil {
		return err
	}

	var models []secboot.ModelForSealing
	for _, m := range modelParams {
		models = append(models, m.Model)
	}

	// list all the key files to reseal
	keys := []secboot.KeyFileOrSlotName{
		{
			DevicePath: "/dev/disk/by-partlabel/ubuntu-data",
			SlotName:   "default",
			KeyFile:    device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir),
		},
	}

	resealKeyParams := &secboot.ResealKeysParams{
		PCRProfile:           pcrProfile,
		Keys:                 keys,
		TPMPolicyAuthKeyFile: authKeyFile,
	}
	if err := secbootResealKeys(resealKeyParams); err != nil {
		return fmt.Errorf("cannot reseal the encryption key: %v", err)
	}

	// FIXME: We should also update "run" keyslot role

	// TODO: use constants for "run+recover" and "all"
	if err := updateState("run+recover", "all", []string{"run", "recover"}, models, pcrProfile); err != nil {
		return err
	}

	return nil
}

func resealFallbackObjectKeys(updateState StateUpdater, pbc boot.PredictableBootChains, authKeyFile string, roleToBlName map[bootloader.Role]string) error {
	// get model parameters from bootchains
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key resealing: %v", err)
	}

	numModels := len(modelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	pcrProfile, err := secbootBuildPCRProtectionProfile(modelParams)
	if err != nil {
		return err
	}

	var models []secboot.ModelForSealing
	for _, m := range modelParams {
		models = append(models, m.Model)
	}

	// list all the key files to reseal
	keys := []secboot.KeyFileOrSlotName{
		{
			DevicePath: "/dev/disk/by-partlabel/ubuntu-data",
			SlotName:   "default-fallback",
			KeyFile:    device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir),
		},
		{
			DevicePath: "/dev/disk/by-partlabel/ubuntu-save",
			SlotName:   "default-fallback",
			KeyFile:    device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir),
		},
	}

	resealKeyParams := &secboot.ResealKeysParams{
		PCRProfile:           pcrProfile,
		Keys:                 keys,
		TPMPolicyAuthKeyFile: authKeyFile,
	}
	if err := secbootResealKeys(resealKeyParams); err != nil {
		return fmt.Errorf("cannot reseal the fallback encryption keys: %v", err)
	}

	// FIXME: We are missing recover for system-data, for
	// "recover" boot mode. It is different from the run+recover
	// as this should only include working models.

	// TODO: use constants for "recover" (the first parameter) and "system-save"
	if err := updateState("recover", "system-save", []string{"recover", "factory-reset"}, models, pcrProfile); err != nil {
		return err
	}

	return nil
}
