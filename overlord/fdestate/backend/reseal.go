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
	"errors"
	"fmt"
	"os"
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
	secbootResealKey                 = secboot.ResealKey
	secbootBuildPCRProtectionProfile = secboot.BuildPCRProtectionProfile
	secbootGetPrimaryKey             = secboot.GetPrimaryKey
	secbootRevokeOldKeys             = (*secboot.UpdatedKeys).RevokeOldKeys
	secbootLoadCheckResult           = secboot.LoadCheckResult
	bootIsResealNeeded               = boot.IsResealNeeded
)

// MockSecbootResealKey is only useful in testing. Note that this is a very low
// level call and may need significant environment setup.
func MockSecbootResealKey(f func(key secboot.KeyDataLocation, params *secboot.ResealKeyParams) (secboot.UpdatedKeys, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootResealKey only can be mocked in tests")
	old := secbootResealKey
	secbootResealKey = f
	return func() {
		secbootResealKey = old
	}
}

func MockSecbootBuildPCRProtectionProfile(f func(
	modelParams []*secboot.SealKeyModelParams,
	checkResult *secboot.PreinstallCheckResult,
	allowInsufficientDmaProtection bool,
) (secboot.SerializedPCRProfile, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootBuildPCRProtectionProfile only can be mocked in tests")
	old := secbootBuildPCRProtectionProfile
	secbootBuildPCRProtectionProfile = f
	return func() {
		secbootBuildPCRProtectionProfile = old
	}
}

func MockSecbootGetPrimaryKey(f func(devices []string, fallbackKeyFiles []string) ([]byte, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootGetPrimaryKey only can be mocked in tests")
	old := secbootGetPrimaryKey
	secbootGetPrimaryKey = f
	return func() {
		secbootGetPrimaryKey = old
	}
}

func MockSecbootLoadCheckResult(f func(filename string) (*secboot.PreinstallCheckResult, error)) (restore func()) {
	osutil.MustBeTestBinary("secbootLoadCheckResult only can be mocked in tests")
	old := secbootLoadCheckResult
	secbootLoadCheckResult = f
	return func() {
		secbootLoadCheckResult = old
	}
}

func secbootPCRPolicyCounterHandlesImpl(uk secboot.UpdatedKeys) []uint32 {
	return uk.PCRPolicyCounterHandles()
}

var secbootPCRPolicyCounterHandles = secbootPCRPolicyCounterHandlesImpl

// RoleInfo is an extract of fdestate.KeyslotRoleInfo
type RoleInfo struct {
	PrimaryKeyID                   int
	TPM2PCRPolicyRevocationCounter uint32
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

type parametersKey struct {
	role          string
	containerRole string
}

// updatedParameters represents sealing parameters from FDE state
// (eventually updated from resealing), but not yet applied to current
// FDE state.
type updatedParameters struct {
	catalog map[parametersKey]*SealingParameters
}

func newUpdatedParameters() *updatedParameters {
	return &updatedParameters{catalog: make(map[parametersKey]*SealingParameters)}
}

func (u *updatedParameters) set(role, containerRole string, params *SealingParameters) {
	u.catalog[parametersKey{role: role, containerRole: containerRole}] = params
}

func (u *updatedParameters) unset(role, containerRole string) {
	delete(u.catalog, parametersKey{role: role, containerRole: containerRole})
}

func (u *updatedParameters) setTpmPCRProfile(role, containerRole string, tpmPCRProfile []byte) bool {
	params, ok := u.catalog[parametersKey{role: role, containerRole: containerRole}]
	if !ok {
		return false
	}
	params.TpmPCRProfile = tpmPCRProfile
	return true
}

// find returns the parameters for the first container role it finds
func (u *updatedParameters) find(role string, containerRoles ...string) *SealingParameters {
	for _, cr := range containerRoles {
		params, ok := u.catalog[parametersKey{role: role, containerRole: cr}]
		if ok {
			return params
		}
	}
	return nil
}

func (u *updatedParameters) apply(manager FDEStateManager) error {
	for key, parameters := range u.catalog {
		if err := manager.Update(key.role, key.containerRole, parameters); err != nil {
			return err
		}
	}
	return nil
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
	// Get returns the current parameters for a given role as well as policy counter and primary key id. If role exists but there is no parameters, it will return nil parameters without error.
	Get(role string, containerRole string) (parameters *SealingParameters, err error)
	// RoleInfo
	RoleInfo(role string) (info *RoleInfo, err error)
	// Unlock notifies the manager that the state can be unlocked and returns a function to relock it.
	Unlock() (relock func())
	// GetEncryptedContainers returns the list of encrypted disks for the device
	GetEncryptedContainers() ([]EncryptedContainer, error)
	// VerifyPrimaryKeyAgainstState verifies that a raw primary key matches
	// the digest of primary key indexed by its state ID.
	VerifyPrimaryKeyAgainstState(primaryKeyID int, primaryKey []byte) bool
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

// errNoPCRProfileCalculated is used when recalculateParamatersTPM
// does not set the parameters in state due to unchanged boot
// chain. This is not an real error most of the time but it still needs
// to exit resealing attempt.
var errNoPCRProfileCalculated = errors.New("no PCR profile calculated, skipping resealing")

func doReseal(manager FDEStateManager, rootdir string, hintExpectFDEHook bool, inputs resealInputs, opts resealOptions) error {
	revokeOldKeys := opts.Revoke

	containers, err := manager.GetEncryptedContainers()
	if err != nil {
		return err
	}

	var devices []string

	for _, container := range containers {
		devices = append(devices, container.DevPath())
	}

	saveFDEDir := dirs.SnapFDEDirUnderSave(dirs.SnapSaveDirUnder(rootdir))
	fallbackPrimaryKeyFiles := []string{
		filepath.Join(saveFDEDir, "aux-key"),
		filepath.Join(saveFDEDir, "tpm-policy-auth-key"),
	}

	params := inputs.bootChains
	recoverModels := getUniqueModels(params.RecoveryBootChains)
	newParameters := newUpdatedParameters()
	newParameters.set("run", "all", &SealingParameters{
		BootModes: []string{"run"},
		Models:    getUniqueModels(params.RunModeBootChains),
	})
	newParameters.set("run+recover", "all", &SealingParameters{
		BootModes: []string{"run", "recover"},
		Models:    getUniqueModels(append(params.RunModeBootChains, params.RecoveryBootChainsForRunKey...)),
	})
	newParameters.set("recover", "system-data", &SealingParameters{
		BootModes: []string{"recover"},
		Models:    recoverModels,
	})
	newParameters.set("recover", "system-save", &SealingParameters{
		BootModes: []string{"recover", "factory-reset"},
		Models:    recoverModels,
	})

	tpmProfilesCalculated := false
	ensureTPMProfiles := func() error {
		if !tpmProfilesCalculated {
			relock := manager.Unlock()
			defer relock()

			if err := recalculateParamatersTPM(newParameters, rootdir, inputs, opts); err != nil {
				return err
			}

			tpmProfilesCalculated = true
		}
		return nil
	}

	foundPrimaryKeys := make(map[int][]byte)

	allResealedKeys := make(map[int]secboot.UpdatedKeys)
	resealKey := func(key secboot.KeyDataLocation, role string, containerRole string) error {
		parameters := newParameters.find(role, containerRole, "all")
		if parameters == nil {
			return fmt.Errorf("internal error: not container role for %s/%s", role, containerRole)
		}
		roleInfo, err := manager.RoleInfo(role)
		if err != nil {
			return err
		}

		getTpmPCRProfile := func() ([]byte, error) {
			if err := ensureTPMProfiles(); err != nil {
				return nil, err
			}
			if parameters.TpmPCRProfile == nil {
				return nil, errNoPCRProfileCalculated
			}
			return parameters.TpmPCRProfile, nil
		}

		verifyPrimaryKey := func(primaryKey []byte) {
			if !manager.VerifyPrimaryKeyAgainstState(roleInfo.PrimaryKeyID, primaryKey) {
				logger.Noticef("WARNING: primary key is not matching the FDE state")
			} else {
				foundPrimaryKeys[roleInfo.PrimaryKeyID] = primaryKey
			}
		}

		rkp := &secboot.ResealKeyParams{
			// Because the save disk might be opened with
			// an old plainkey, there might not be any
			// primary key available in keyring for that
			// save device. So we need to look at all
			// devices.
			PrimaryKeyDevices:       devices,
			FallbackPrimaryKeyFiles: fallbackPrimaryKeyFiles,
			VerifyPrimaryKey:        verifyPrimaryKey,
			BootModes:               parameters.BootModes,
			Models:                  parameters.Models,
			GetTpmPCRProfile:        getTpmPCRProfile,
			NewPCRPolicyVersion:     revokeOldKeys,
			HintExpectFDEHook:       hintExpectFDEHook,
		}
		resealedKeys, err := secbootResealKey(key, rkp)
		if err != nil {
			revokeOldKeys = false
			return err
		}

		for _, policyCounter := range secbootPCRPolicyCounterHandles(resealedKeys) {
			if policyCounter != roleInfo.TPM2PCRPolicyRevocationCounter {
				logger.Noticef("WARNING: policy counter handle %v is not matching the FDE state", policyCounter)
			}
		}

		allResealedKeys[roleInfo.PrimaryKeyID] = append(allResealedKeys[roleInfo.PrimaryKeyID], resealedKeys...)

		return nil
	}

	for _, container := range containers {
		legacyKeys := container.LegacyKeys()

		switch container.ContainerRole() {
		case "system-data":
			defaultLegacyKey, hasDefaultLegacyKey := legacyKeys["default"]
			runKey := secboot.KeyDataLocation{
				DevicePath: container.DevPath(),
				SlotName:   "default",
			}
			if hasDefaultLegacyKey {
				runKey.KeyFile = defaultLegacyKey
			}

			if err := resealKey(runKey, "run+recover", container.ContainerRole()); err != nil {
				if !errors.Is(err, errNoPCRProfileCalculated) {
					return err
				}
				// remove incomplete state to avoid applying it to the global FDE state
				newParameters.unset("run", "all")
				newParameters.unset("run+recover", "all")
			}
		}

		switch container.ContainerRole() {
		case "system-save", "system-data":
			fallbackLegacyKey, hasFallbackLegacyKey := legacyKeys["default-fallback"]

			fallbackKey := secboot.KeyDataLocation{
				DevicePath: container.DevPath(),
				SlotName:   "default-fallback",
			}

			if hasFallbackLegacyKey {
				fallbackKey.KeyFile = fallbackLegacyKey
			}

			if err := resealKey(fallbackKey, "recover", container.ContainerRole()); err != nil {
				// If the error is errNoPCRProfileCalculated, then we just skipped
				// resealing because no change was detected in boot chains. This is not an error.
				if !errors.Is(err, errNoPCRProfileCalculated) {
					return err
				}
				// remove incomplete state to avoid applying it to the global FDE state
				newParameters.unset("recover", container.ContainerRole())
			}
		}
	}

	if err := newParameters.apply(manager); err != nil {
		return err
	}

	if revokeOldKeys {
		for primaryKeyID, oldKeys := range allResealedKeys {
			primaryKey, hasPrimaryKey := foundPrimaryKeys[primaryKeyID]
			if !hasPrimaryKey {
				return fmt.Errorf("Missing primary key")
			}
			if err := secbootRevokeOldKeys(&oldKeys, primaryKey); err != nil {
				return fmt.Errorf("cannot revoke older keys: %v", err)
			}
		}
	}

	return nil
}

// recalculateParamatersTPM recalculate TPM PCR profiles and stores them in `parameters`
func recalculateParamatersTPM(parameters *updatedParameters, rootdir string, inputs resealInputs, opts resealOptions) error {
	params := inputs.bootChains
	// reseal the run object
	pbc := boot.ToPredictableBootChains(append(params.RunModeBootChains, params.RecoveryBootChainsForRunKey...))

	// TODO:FDEM: is it sufficient to assume that missing preinstall check file is intentional?
	// Alternatively we need some other indication that we use/rely on it, perhaps it can be
	// loaded into FDE state, which can be refreshed after any run time changes.
	loadCheckResultPath := device.PreinstallCheckResultUnder(boot.InstallHostFDESaveDir)
	checkResult, err := secbootLoadCheckResult(loadCheckResultPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cannot load preinstall check result: %v", err)
		}
		logger.Noticef("preinstall check result not available: file %s not found", loadCheckResultPath)
	}

	needed, nextCount, err := bootIsResealNeeded(pbc, BootChainsFileUnder(rootdir), opts.ExpectReseal)
	if err != nil {
		return err
	}

	if needed || opts.Force {
		runOnlyPbc := boot.ToPredictableBootChains(params.RunModeBootChains)

		pbcJSON, _ := json.Marshal(pbc)
		logger.Debugf("resealing (%d) to boot chains: %s", nextCount, pbcJSON)

		err := updateRunProtectionProfile(parameters, runOnlyPbc, pbc, inputs.signatureDBUpdate, params.RoleToBlName, checkResult)
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

		err := updateFallbackProtectionProfile(parameters, rpbc, inputs.signatureDBUpdate, params.RoleToBlName, checkResult)
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

func anyClassicModel(params ...*secboot.SealKeyModelParams) bool {
	for _, m := range params {
		if m.Model.Classic() {
			return true
		}
	}
	return false
}

// updateRunProtectionProfile recalculate run TPM PCR profiles and stores them in `parameters`
func updateRunProtectionProfile(
	parameters *updatedParameters,
	pbcRunOnly, pbcWithRecovery boot.PredictableBootChains,
	sigDbxUpdate []byte,
	roleToBlName map[bootloader.Role]string,
	checkResult *secboot.PreinstallCheckResult,
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
		var err error

		pcrProfile, err = secbootBuildPCRProtectionProfile(modelParams, checkResult, !hasClassicModel)
		if err != nil {
			return err
		}

		pcrProfileRunOnly, err = secbootBuildPCRProtectionProfile(modelParamsRunOnly, checkResult, !hasClassicModel)
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

	if ok := parameters.setTpmPCRProfile("run+recover", "all", pcrProfile); !ok {
		return fmt.Errorf("no run+recover state")
	}
	if ok := parameters.setTpmPCRProfile("run", "all", pcrProfileRunOnly); !ok {
		return fmt.Errorf("no run state")
	}

	return nil
}

// updateRunProtectionProfile recalculate fallback TPM PCR profiles and stores them in `parameters`
func updateFallbackProtectionProfile(
	parameters *updatedParameters,
	pbc boot.PredictableBootChains,
	sigDbxUpdate []byte,
	roleToBlName map[bootloader.Role]string,
	checkResult *secboot.PreinstallCheckResult,
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
		var err error

		pcrProfile, err = secbootBuildPCRProtectionProfile(modelParams, checkResult, !hasClassicModel)
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

	// We should have different pcr profile here...
	if ok := parameters.setTpmPCRProfile("recover", "system-save", pcrProfile); !ok {
		return fmt.Errorf("no recover state for system-save")
	}
	if ok := parameters.setTpmPCRProfile("recover", "system-data", pcrProfile); !ok {
		return fmt.Errorf("no recover state for system-data")
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

	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		if opts.EnsureProvisioned {
			lockoutAuthFile := device.TpmLockoutAuthUnder(boot.InstallHostFDESaveDir)
			if err := secbootProvisionTPM(secboot.TPMPartialReprovision, lockoutAuthFile); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}

	return doReseal(manager, rootdir, method == device.SealingMethodFDESetupHook, inputs, opts)
}

func attachSignatureDbxUpdate(params []*secboot.SealKeyModelParams, update []byte) {
	if len(update) == 0 {
		return
	}

	for _, p := range params {
		p.EFISignatureDbxUpdate = update
	}
}
