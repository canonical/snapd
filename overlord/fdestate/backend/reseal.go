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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootResealKeys                 = secboot.ResealKeys
	secbootBuildPCRProtectionProfile  = secboot.BuildPCRProtectionProfile
	secbootResealKeysWithFDESetupHook = secboot.ResealKeysWithFDESetupHook
	secbootGetPrimaryKey              = secboot.GetPrimaryKey
	secbootRevokeOldKeys              = (*secboot.UpdatedKeys).RevokeOldKeys
	bootIsResealNeeded                = boot.IsResealNeeded
)

// MockSecbootResealKeys is only useful in testing. Note that this is a very low
// level call and may need significant environment setup.
func MockSecbootResealKeys(f func(params *secboot.ResealKeysParams, newPCRPolicyVersion bool) (secboot.UpdatedKeys, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootResealKeys only can be mocked in tests")
	old := secbootResealKeys
	secbootResealKeys = f
	return func() {
		secbootResealKeys = old
	}
}

func MockSecbootBuildPCRProtectionProfile(f func(modelParams []*secboot.SealKeyModelParams, allowInsufficientDmaProtection bool) (secboot.SerializedPCRProfile, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootBuildPCRProtectionProfile only can be mocked in tests")
	old := secbootBuildPCRProtectionProfile
	secbootBuildPCRProtectionProfile = f
	return func() {
		secbootBuildPCRProtectionProfile = old
	}
}

func MockSecbootGetPrimaryKey(f func(devices []string, fallbackKeyFile string) ([]byte, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootGetPrimaryKey only can be mocked in tests")
	old := secbootGetPrimaryKey
	secbootGetPrimaryKey = f
	return func() {
		secbootGetPrimaryKey = old
	}
}

// SealingParameters contains the parameters that may be used for
// sealing.  It should be the same as
// fdestate.KeyslotRoleParameters. However we cannot import it. See
// documentation for that type.
type SealingParameters struct {
	BootModes     []string
	Models        []secboot.ModelForSealing
	TpmPCRProfile []byte
}

// EncryptedContainer gives information on the role, path and path to
// extra legacy keys.
type EncryptedContainer interface {
	// ContainerRole gives the container role of the disk. See KeyslotRoleInfo.Parameters.
	ContainerRole() string
	// DevPath gives the path to the device node. This should be the same as the path used for keyring.
	DevPath() string
	// LegacyKeys gives path of the legacy keys indexed by the key names used in the tokens
	LegacyKeys() map[string]string
}

// FDEStateManager represents an interface for a manager that can
// store a state for sealing parameters.
type FDEStateManager interface {
	// Update will update the sealing parameters for a give role.
	Update(role string, containerRole string, parameters *SealingParameters) error
	// Get returns the current parameters for a given role. If parameters exist for that role, it will return nil without error.
	Get(role string, containerRole string) (parameters *SealingParameters, err error)
	// Unlock notifies the manager that the state can be unlocked and returns a function to relock it.
	Unlock() (relock func())
	// GetEncryptedContainers returns the list of encrypted disks for the device
	GetEncryptedContainers() ([]EncryptedContainer, error)
}

// comparableModel is just a representation of secboot.ModelForSealing
// that is comparable so we can use it as an index of a map.
type comparableModel struct {
	BrandID   string
	SignKeyID string
	Model     string
	Classic   bool
	Grade     asserts.ModelGrade
	Series    string
}

func toComparable(m secboot.ModelForSealing) comparableModel {
	return comparableModel{
		BrandID:   m.BrandID(),
		SignKeyID: m.SignKeyID(),
		Model:     m.Model(),
		Classic:   m.Classic(),
		Grade:     m.Grade(),
		Series:    m.Series(),
	}
}

func getUniqueModels(bootChains []boot.BootChain) []secboot.ModelForSealing {
	uniqueModels := make(map[comparableModel]secboot.ModelForSealing)

	for _, bc := range bootChains {
		m := bc.ModelForSealing()
		uniqueModels[toComparable(m)] = m
	}

	var models []secboot.ModelForSealing
	for _, m := range uniqueModels {
		models = append(models, m)
	}

	return models
}

type resealParamsAndLocation struct {
	params   *SealingParameters
	location secboot.KeyDataLocation
}

func doReseal(manager FDEStateManager, method device.SealingMethod, rootdir string, revokeOldKeys bool) error {
	containers, err := manager.GetEncryptedContainers()
	if err != nil {
		return err
	}

	var keys []resealParamsAndLocation

	var devices []string

	for _, container := range containers {
		devices = append(devices, container.DevPath())
		legacyKeys := container.LegacyKeys()

		switch container.ContainerRole() {
		case "system-data":
			parameters, err := manager.Get("run+recover", container.ContainerRole())
			if err != nil {
				return err
			}
			if parameters == nil {
				logger.Debugf("there was no parameters for run+recover/%s", container.ContainerRole())
				if revokeOldKeys {
					// After installation, on first boot, we have an incomplete FDE state.
					// This is fine. In this case we are not supposed to revoke old keys.
					// And we cannot revoke old keys because we probably did not update
					// the expected counter value in the profile. Otherwise incrementing
					// the counter would revoke the current keys. So as a safety
					// precaution, we should just ignore revocation attempt in this case.
					logger.Noticef("warning: we are trying to revoke old keys while we have an incomplete FDE state")
					revokeOldKeys = false
				}
			} else {
				defaultLegacyKey, hasDefaultLegacyKey := legacyKeys["default"]

				runKey := secboot.KeyDataLocation{
					DevicePath: container.DevPath(),
					SlotName:   "default",
				}
				if hasDefaultLegacyKey {
					runKey.KeyFile = defaultLegacyKey
				}
				keys = append(keys, resealParamsAndLocation{
					params:   parameters,
					location: runKey,
				})
			}
		}

		switch container.ContainerRole() {
		case "system-save", "system-data":
			parameters, err := manager.Get("recover", container.ContainerRole())
			if err != nil {
				return err
			}
			if parameters == nil {
				logger.Debugf("there was no parameters for recover/%s", container.ContainerRole())
				if revokeOldKeys {
					// After installation, on first boot, we have an incomplete FDE state.
					// This is fine. In this case we are not supposed to revoke old keys.
					// And we cannot revoke old keys because we probably did not update
					// the expected counter value in the profile. Otherwise incrementing
					// the counter would revoke the current keys. So as a safety
					// precaution, we should just ignore revocation attempt in this case.
					logger.Noticef("warning: we are trying to revoke old keys while we have an incomplete FDE state")
					revokeOldKeys = false
				}
			} else {
				fallbackLegacyKey, hasFallbackLegacyKey := legacyKeys["default-fallback"]

				fallbackKey := secboot.KeyDataLocation{
					DevicePath: container.DevPath(),
					SlotName:   "default-fallback",
				}

				if hasFallbackLegacyKey {
					fallbackKey.KeyFile = fallbackLegacyKey
				}

				keys = append(keys, resealParamsAndLocation{
					params:   parameters,
					location: fallbackKey,
				})
			}
		}
	}

	switch method {
	case device.SealingMethodFDESetupHook:
		// Note this is the fallback key path for old installations. The file might not exist.
		primaryKeyFile := filepath.Join(boot.InstallHostFDESaveDir, "aux-key")
		// All devices should have the same primary key
		primaryKeyGetter := func() ([]byte, error) {
			return secbootGetPrimaryKey(devices, primaryKeyFile)
		}
		for _, key := range keys {
			if err := secbootResealKeysWithFDESetupHook([]secboot.KeyDataLocation{key.location}, primaryKeyGetter, key.params.Models, key.params.BootModes); err != nil {
				return err
			}
		}
		return nil
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDirUnder(rootdir))
		authKeyFile := filepath.Join(saveFDEDir, "tpm-policy-auth-key")
		// All devices should have the same primary key
		primaryKey, err := secbootGetPrimaryKey(devices, authKeyFile)
		if err != nil {
			return err
		}
		var allResealedKeys secboot.UpdatedKeys
		for _, key := range keys {
			keyParams := &secboot.ResealKeysParams{
				PCRProfile: key.params.TpmPCRProfile,
				Keys:       []secboot.KeyDataLocation{key.location},
				PrimaryKey: primaryKey,
			}

			resealedKeys, err := secbootResealKeys(keyParams, revokeOldKeys)
			if err != nil {
				return fmt.Errorf("cannot reseal the encryption key: %v", err)
			}
			if revokeOldKeys {
				allResealedKeys = append(allResealedKeys, resealedKeys...)
			}
		}
		if revokeOldKeys {
			if err := secbootRevokeOldKeys(&allResealedKeys, primaryKey); err != nil {
				return fmt.Errorf("cannot revoke older keys: %v", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}
}

func recalculateParamatersFDEHook(manager FDEStateManager, method device.SealingMethod, rootdir string, inputs resealInputs, opts resealOptions) error {
	params := inputs.bootChains
	runModels := getUniqueModels(append(params.RunModeBootChains, params.RecoveryBootChainsForRunKey...))
	recoveryModels := getUniqueModels(params.RecoveryBootChains)

	runParams := &SealingParameters{
		BootModes: []string{"run", "recover"},
		Models:    runModels,
	}
	if err := manager.Update("run+recover", "all", runParams); err != nil {
		return err
	}

	recoveryParamsData := &SealingParameters{
		BootModes: []string{"recover"},
		Models:    recoveryModels,
	}
	if err := manager.Update("recover", "system-data", recoveryParamsData); err != nil {
		return err
	}

	recoveryParamsSave := &SealingParameters{
		BootModes: []string{"recover", "factory-reset"},
		Models:    recoveryModels,
	}
	if err := manager.Update("recover", "system-save", recoveryParamsSave); err != nil {
		return err
	}

	return nil
}

func recalculateParamatersTPM(manager FDEStateManager, method device.SealingMethod, rootdir string, inputs resealInputs, opts resealOptions) error {
	params := inputs.bootChains
	// reseal the run object
	pbc := boot.ToPredictableBootChains(append(params.RunModeBootChains, params.RecoveryBootChainsForRunKey...))

	needed, nextCount, err := bootIsResealNeeded(pbc, BootChainsFileUnder(rootdir), opts.ExpectReseal)
	if err != nil {
		return err
	}
	if needed || opts.Force {
		runOnlyPbc := boot.ToPredictableBootChains(params.RunModeBootChains)

		pbcJSON, _ := json.Marshal(pbc)
		logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

		err := updateRunProtectionProfile(manager, runOnlyPbc, pbc, inputs.signatureDBUpdate, params.RoleToBlName)
		if err != nil {
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
	needed, nextFallbackCount, err = bootIsResealNeeded(rpbc, RecoveryBootChainsFileUnder(rootdir), opts.ExpectReseal)
	if err != nil {
		return err
	}
	if needed || opts.Force {
		rpbcJSON, _ := json.Marshal(rpbc)
		logger.Debugf("resealing (%d) to recovery boot chains: %s", nextFallbackCount, rpbcJSON)

		err := updateFallbackProtectionProfile(manager, rpbc, inputs.signatureDBUpdate, params.RoleToBlName)
		if err != nil {
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

func anyClassicModel(params... *secboot.SealKeyModelParams) bool {
	for _, m := range params {
		if m.Model.Classic() {
			return true
		}
	}
	return false
}

func updateRunProtectionProfile(
	manager FDEStateManager,
	pbcRunOnly, pbcWithRecovery boot.PredictableBootChains,
	sigDbxUpdate []byte,
	roleToBlName map[bootloader.Role]string,
) error {
	// get model parameters from bootchains
	modelParams, err := boot.SealKeyModelParams(pbcWithRecovery, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for key resealing: %v", err)
	}

	modelParamsRunOnly, err := boot.SealKeyModelParams(pbcRunOnly, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for key resealing: %v", err)
	}

	hasClassicModel := anyClassicModel(append(modelParams, modelParamsRunOnly...)...)

	if len(modelParams) < 1 || len(modelParamsRunOnly) < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	if len(sigDbxUpdate) > 0 {
		logger.Debug("attaching DB update payload")
		attachSignatureDbxUpdate(modelParams, sigDbxUpdate)
		attachSignatureDbxUpdate(modelParamsRunOnly, sigDbxUpdate)
	}

	var pcrProfile []byte
	var pcrProfileRunOnly []byte

	err = func() error {
		relock := manager.Unlock()
		defer relock()

		var err error

		pcrProfile, err = secbootBuildPCRProtectionProfile(modelParams, !hasClassicModel)
		if err != nil {
			return err
		}

		pcrProfileRunOnly, err = secbootBuildPCRProtectionProfile(modelParamsRunOnly, !hasClassicModel)
		if err != nil {
			return err
		}

		return nil
	}()
	if err != nil {
		return err
	}

	if len(pcrProfile) == 0 {
		return fmt.Errorf("unexpected length of serialized PCR profile")
	}

	logger.Debugf("PCR profile length: %v", len(pcrProfile))

	var models []secboot.ModelForSealing
	for _, m := range modelParams {
		models = append(models, m.Model)
	}

	var modelsRunOnly []secboot.ModelForSealing
	for _, m := range modelParamsRunOnly {
		modelsRunOnly = append(modelsRunOnly, m.Model)
	}

	runParams := &SealingParameters{
		BootModes:     []string{"run", "recover"},
		Models:        models,
		TpmPCRProfile: pcrProfile,
	}

	// TODO:FDEM: use constants for "run+recover" and "all"
	if err := manager.Update("run+recover", "all", runParams); err != nil {
		return err
	}

	runOnlyParams := &SealingParameters{
		BootModes:     []string{"run"},
		Models:        modelsRunOnly,
		TpmPCRProfile: pcrProfileRunOnly,
	}
	// TODO:FDEM: use constants for "run+recover" and "all"
	if err := manager.Update("run", "all", runOnlyParams); err != nil {
		return err
	}

	return nil
}

func updateFallbackProtectionProfile(
	manager FDEStateManager, pbc boot.PredictableBootChains,
	sigDbxUpdate []byte,
	roleToBlName map[bootloader.Role]string,
) error {
	// get model parameters from bootchains
	modelParams, err := boot.SealKeyModelParams(pbc, roleToBlName)
	if err != nil {
		return fmt.Errorf("cannot prepare for fallback key resealing: %v", err)
	}

	numModels := len(modelParams)
	if numModels < 1 {
		return fmt.Errorf("at least one set of model-specific parameters is required")
	}

	if len(sigDbxUpdate) > 0 {
		logger.Debug("attaching DB update payload for fallback keys")
		attachSignatureDbxUpdate(modelParams, sigDbxUpdate)
	}

	hasClassicModel := anyClassicModel(modelParams...)

	var pcrProfile []byte
	err = func() error {
		relock := manager.Unlock()
		defer relock()

		var err error

		pcrProfile, err = secbootBuildPCRProtectionProfile(modelParams, !hasClassicModel)
		if err != nil {
			return err
		}
		return nil
	}()
	if err != nil {
		return err
	}

	if len(pcrProfile) == 0 {
		return fmt.Errorf("unexpected length of serialized PCR profile")
	}

	var models []secboot.ModelForSealing
	for _, m := range modelParams {
		models = append(models, m.Model)
	}

	saveParams := &SealingParameters{
		BootModes:     []string{"recover", "factory-reset"},
		Models:        models,
		TpmPCRProfile: pcrProfile,
	}
	// TODO:FDEM: use constants for "recover" (the first parameter) and "system-save"
	if err := manager.Update("recover", "system-save", saveParams); err != nil {
		return err
	}

	dataParams := &SealingParameters{
		BootModes:     []string{"recover"},
		Models:        models,
		TpmPCRProfile: pcrProfile,
	}
	// TODO:FDEM: use constants for "recover" (the first parameter) and "system-data"
	if err := manager.Update("recover", "system-data", dataParams); err != nil {
		return err
	}

	return nil
}

// ResealKeyForBootChains reseals disk encryption keys with the given bootchains.
func ResealKeyForBootChains(manager FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
	return resealKeys(manager, method, rootdir,
		resealInputs{
			bootChains: params.BootChains,
		},
		resealOptions{
			ExpectReseal:      params.Options.ExpectReseal,
			Force:             params.Options.Force,
			EnsureProvisioned: params.Options.EnsureProvisioned,
			IgnoreFDEHooks:    params.Options.IgnoreFDEHooks,
			Revoke:            params.Options.RevokeOldKeys,
		})
}

// ResealKeysForSignaturesDBUpdate reseals disk encryption keys for the provided
// boot chains and an optional signature DB update
func ResealKeysForSignaturesDBUpdate(
	manager FDEStateManager, method device.SealingMethod, rootdir string,
	params *boot.ResealKeyForBootChainsParams, dbUpdate []byte,
) error {
	return resealKeys(manager, method, rootdir,
		resealInputs{
			bootChains:        params.BootChains,
			signatureDBUpdate: dbUpdate,
		},
		resealOptions{
			ExpectReseal: true,
			// the boot chains are unchanged, which normally would result in
			// no-reseal being done, but the content of DBX is being changed,
			// either being part of the request or it has already been written
			// to the relevant EFI variables (in which case this is a
			// post-update reseal)
			Force: true,
			// no model changed => ignore FDE hooks
			IgnoreFDEHooks: true,
		})
}

type resealInputs struct {
	bootChains        boot.BootChains
	signatureDBUpdate []byte
}

type resealOptions struct {
	ExpectReseal      bool
	Force             bool
	EnsureProvisioned bool
	Revoke            bool
	IgnoreFDEHooks    bool
}

func resealKeys(
	manager FDEStateManager, method device.SealingMethod, rootdir string,
	inputs resealInputs,
	opts resealOptions,
) error {
	switch method {
	case device.SealingMethodFDESetupHook:
		if opts.IgnoreFDEHooks {
			return nil
		}

		if err := recalculateParamatersFDEHook(manager, method, rootdir, inputs, opts); err != nil {
			return err
		}
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		if opts.EnsureProvisioned {
			lockoutAuthFile := device.TpmLockoutAuthUnder(boot.InstallHostFDESaveDir)
			if err := secbootProvisionTPM(secboot.TPMPartialReprovision, lockoutAuthFile); err != nil {
				return err
			}
		}

		if err := recalculateParamatersTPM(manager, method, rootdir, inputs, opts); err != nil {
			// TODO:FDEM:FIX: remove the save boot chains.
			return err
		}
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}

	return doReseal(manager, method, rootdir, opts.Revoke)
}

func attachSignatureDbxUpdate(params []*secboot.SealKeyModelParams, update []byte) {
	if len(update) == 0 {
		return
	}

	for _, p := range params {
		p.EFISignatureDbxUpdate = update
	}
}
