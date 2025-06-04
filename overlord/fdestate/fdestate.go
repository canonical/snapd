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
package fdestate

import (
	"crypto"
	"errors"
	"fmt"

	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/secboot"
)

var (
	secbootGetPrimaryKeyDigest    = secboot.GetPrimaryKeyDigest
	secbootVerifyPrimaryKeyDigest = secboot.VerifyPrimaryKeyDigest
	secbootGetPCRHandle           = secboot.GetPCRHandle
	backendRemoveBootChainCache   = backend.RemoveBootChainCache
)

// Model is a json serializable secboot.ModelForSealing
type Model struct {
	SeriesValue    string             `json:"series"`
	BrandIDValue   string             `json:"brand-id"`
	ModelValue     string             `json:"model"`
	ClassicValue   bool               `json:"classic"`
	GradeValue     asserts.ModelGrade `json:"grade"`
	SignKeyIDValue string             `json:"sign-key-id"`
}

// Series implements secboot.ModelForSealing.Series
func (m *Model) Series() string {
	return m.SeriesValue
}

// BrandID implements secboot.ModelForSealing.BrandID
func (m *Model) BrandID() string {
	return m.BrandIDValue
}

// Model implements secboot.ModelForSealing.Model
func (m *Model) Model() string {
	return m.ModelValue
}

// Classic implements secboot.ModelForSealing.Classic
func (m *Model) Classic() bool {
	return m.ClassicValue
}

// Grade implements secboot.ModelForSealing.Grade
func (m *Model) Grade() asserts.ModelGrade {
	return m.GradeValue
}

// SignKeyID implements secboot.ModelForSealing.SignKeyID
func (m *Model) SignKeyID() string {
	return m.SignKeyIDValue
}

func newModel(m secboot.ModelForSealing) *Model {
	return &Model{
		SeriesValue:    m.Series(),
		BrandIDValue:   m.BrandID(),
		ModelValue:     m.Model(),
		ClassicValue:   m.Classic(),
		GradeValue:     m.Grade(),
		SignKeyIDValue: m.SignKeyID(),
	}
}

var _ secboot.ModelForSealing = (*Model)(nil)

// KeyslotRoleParameters stores upgradeable parameters for a keyslot role
type KeyslotRoleParameters struct {
	// Models are the optional list of approved models
	Models []*Model `json:"models,omitempty"`
	// BootModes are the optional list of approved modes (run, recover, ...)
	BootModes []string `json:"boot-modes,omitempty"`
	// TPM2PCRProfile is an optional serialized PCR profile
	TPM2PCRProfile secboot.SerializedPCRProfile `json:"tpm2-pcr-profile,omitempty"`
}

// KeyslotRoleInfo stores information about a key slot role
type KeyslotRoleInfo struct {
	// PrimaryKeyID is the ID for the primary key found in
	// PrimaryKeys field of FdeState
	PrimaryKeyID int `json:"primary-key-id"`
	// Parameters is indexed by container role name
	Parameters map[string]KeyslotRoleParameters `json:"params,omitempty"`
	// TPM2PCRPolicyRevocationCounter is the handle for the TPM
	// policy revocation counter.  A value of 0 means it is not
	// set.
	TPM2PCRPolicyRevocationCounter uint32 `json:"tpm2-pcr-policy-revocation-counter,omitempty"`
}

// KeyDigest stores a Digest(key, salt) of a key
// TODO:FDEM: take what is implemented in secboot
type KeyDigest struct {
	// Algorithm is the algorithm for
	Algorithm secboot.HashAlg `json:"alg"`
	// Salt is the salt for the Digest digest
	Salt []byte `json:"salt"`
	// Digest is the result of `Digest(key, salt)`
	Digest []byte `json:"digest"`
}

const defaultHashAlg = crypto.SHA256

func getPrimaryKeyDigest(devicePath string) (KeyDigest, error) {
	salt, digest, err := secbootGetPrimaryKeyDigest(devicePath, crypto.Hash(defaultHashAlg))
	if err != nil {
		return KeyDigest{}, err
	}

	return KeyDigest{
		Algorithm: secboot.HashAlg(defaultHashAlg),
		Salt:      salt,
		Digest:    digest,
	}, nil
}

func (kd *KeyDigest) verifyPrimaryKeyDigest(devicePath string) (bool, error) {
	return secbootVerifyPrimaryKeyDigest(devicePath, crypto.Hash(kd.Algorithm), kd.Salt, kd.Digest)
}

// PrimaryKeyInfo provides information about a primary key that is used to manage key slots
type PrimaryKeyInfo struct {
	Digest KeyDigest `json:"digest"`
}

// FdeState is the root persistent FDE state
type FdeState struct {
	// PrimaryKeys are the keys on the system. Key with ID 0 is
	// reserved for snapd and is populated on first boot. Other
	// IDs are for externally managed keys.
	// If key 0 is not present, we are on a legacy system that
	// does not have a primary key. We are then in one of these cases:
	//  * v1 TPM keys are in used because an old snapd was used
	//    during installation.
	//  * snap-bootstrap in the kernel is old and does not provide
	//    a primary key in the keyring.
	PrimaryKeys map[int]PrimaryKeyInfo `json:"primary-keys"`

	// KeyslotRoles are all keyslot roles indexed by the role name
	KeyslotRoles map[string]KeyslotRoleInfo `json:"keyslot-roles"`

	// PendingExternalOperations keeps a list of changes that capture FDE
	// related operations running outside of snapd.
	PendingExternalOperations []externalOperation `json:"pending-external-operations"`
}

const fdeStateKey = "fde"

func initializeState(st *state.State) error {
	var s FdeState
	err := st.Get(fdeStateKey, &s)
	if err == nil {
		// TODO:FDEM: Do we need to do something in recover?
		return nil
	}

	if !errors.Is(err, state.ErrNoState) {
		return err
	}

	disks, err := getEncryptedContainers(st)
	if err != nil {
		return fmt.Errorf("cannot get encrypted disks: %w", err)
	}

	s.PrimaryKeys = map[int]PrimaryKeyInfo{}
	// Note that Parameters will be updated on first update
	s.KeyslotRoles = map[string]KeyslotRoleInfo{
		// TODO:FDEM: use a constant
		"run": {
			PrimaryKeyID: 0,
		},
		// TODO:FDEM: use a constant
		"run+recover": {
			PrimaryKeyID: 0,
		},
		// TODO:FDEM: use a constant
		"recover": {
			PrimaryKeyID: 0,
		},
	}

	for _, disk := range disks {
		statePrimaryKey, hasStatePrimaryKey := s.PrimaryKeys[0]
		if !hasStatePrimaryKey {
			digest, err := getPrimaryKeyDigest(disk.DevPath())
			if err != nil {
				if !errors.Is(err, secboot.ErrKernelKeyNotFound) {
					return fmt.Errorf("cannot obtain primary key digest for data device %s: %w", disk.DevPath(), err)
				}
				logger.Noticef("cannot obtain primary key digest for data device %s: %v", disk.DevPath(), err)
			} else {
				s.PrimaryKeys[0] = PrimaryKeyInfo{Digest: digest}
			}
		} else {
			sameDigest, err := statePrimaryKey.Digest.verifyPrimaryKeyDigest(disk.DevPath())
			if err != nil {
				if !errors.Is(err, secboot.ErrKernelKeyNotFound) {
					return fmt.Errorf("cannot obtain primary key digest for data device %s: %w", disk.DevPath(), err)
				}
				logger.Noticef("cannot obtain primary key digest for data device %s: %v", disk.DevPath(), err)
			} else if !sameDigest {
				return fmt.Errorf("primary key for data and save partition are not the same")
			}
		}

		legacyKeys := disk.LegacyKeys()
		method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
		if err != nil {
			// No need to warn if that did not work. Just debug.
			logger.Debugf("cannot find sealed keys method: %v", err)
		}
		hintExpectFDEHook := err == nil && method == device.SealingMethodFDESetupHook
		for _, keyName := range []string{"default", "default-fallback"} {
			if keyName == "default" && disk.ContainerRole() == "system-save" {
				continue
			}
			legacyKey, _ := legacyKeys[keyName]
			// GetPCRHandle parses the key data to know whether it is TPM and what handle was used.
			// However, some old formats have conflicts between hooks and TPM, so for old keys,
			// we hint what is the best way to parse it.
			handle, err := secbootGetPCRHandle(disk.DevPath(), keyName, legacyKey, hintExpectFDEHook)
			if err != nil {
				return fmt.Errorf("cannot obtain counter handle for %s (default): %w", disk.DevPath(), err)
			}
			var profiles []string
			switch keyName {
			case "default":
				profiles = []string{"run", "run+recover"}
			case "default-fallback":
				profiles = []string{"recover"}
			}
			for _, profile := range profiles {
				role := s.KeyslotRoles[profile]
				if s.KeyslotRoles[profile].TPM2PCRPolicyRevocationCounter == 0 {
					role.TPM2PCRPolicyRevocationCounter = handle
					s.KeyslotRoles[profile] = role
				} else if role.TPM2PCRPolicyRevocationCounter != handle {
					return fmt.Errorf("found multiple revocation count for run keys")
				}
			}
		}
	}

	_, hasStatePrimaryKey := s.PrimaryKeys[0]
	if !hasStatePrimaryKey {
		logger.Noticef("WARNING: no primary key was found")
	}

	st.Set(fdeStateKey, s)

	return nil
}

func (s *FdeState) updateParameters(role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error {
	roleInfo, hasRole := s.KeyslotRoles[role]
	if !hasRole {
		return fmt.Errorf("cannot find keyslot role %s", role)
	}

	var convertedModels []*Model
	for _, model := range models {
		convertedModels = append(convertedModels, newModel(model))
	}

	if roleInfo.Parameters == nil {
		roleInfo.Parameters = make(map[string]KeyslotRoleParameters)
	}
	roleInfo.Parameters[containerRole] = KeyslotRoleParameters{
		Models:         convertedModels,
		BootModes:      bootModes,
		TPM2PCRProfile: tpmPCRProfile,
	}

	s.KeyslotRoles[role] = roleInfo

	return nil
}

func updateParameters(st *state.State, role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error {
	var s FdeState
	err := st.Get(fdeStateKey, &s)
	if err != nil {
		return err
	}

	if err := s.updateParameters(role, containerRole, bootModes, models, tpmPCRProfile); err != nil {
		return err
	}

	st.Set(fdeStateKey, s)

	return nil
}

func (s *FdeState) getParameters(role string, containerRole string) (hasParamters bool, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte, err error) {
	roleInfo, hasRole := s.KeyslotRoles[role]
	if !hasRole {
		return false, nil, nil, nil, fmt.Errorf("cannot find keyslot role %s", role)
	}

	if roleInfo.Parameters == nil {
		return false, nil, nil, nil, nil
	}
	parameters, hasContainerRole := roleInfo.Parameters[containerRole]
	if !hasContainerRole {
		parameters, hasContainerRole = roleInfo.Parameters["all"]
	}
	if !hasContainerRole {
		return false, nil, nil, nil, nil
	}

	for _, model := range parameters.Models {
		models = append(models, model)
	}

	return true, parameters.BootModes, models, parameters.TPM2PCRProfile, nil
}

func withFdeState(st *state.State, op func(fdeSt *FdeState) (modified bool, err error)) error {
	var fde FdeState
	if err := st.Get(fdeStateKey, &fde); err != nil {
		return err
	}

	mod, err := op(&fde)
	if err != nil {
		return err
	}

	if mod {
		st.Set(fdeStateKey, &fde)
	}
	return nil
}

// fdeRelevantSnaps returns a list of snaps that are relevant in the context of
// FDE and associated boot policies. Specifically this includes the kernel,
// gadget and base snaps.
func fdeRelevantSnaps(st *state.State) ([]string, error) {
	devCtx, err := snapstate.DeviceCtx(st, nil, nil)
	if err != nil {
		return nil, err
	}

	// these snaps, or either their content is measured during boot
	return []string{devCtx.Gadget(), devCtx.Kernel(), devCtx.Base()}, nil
}

func MockGetPrimaryKeyDigest(f func(devicePath string, alg crypto.Hash) ([]byte, []byte, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking GetPrimaryKeyDigest can be done only from tests")

	old := secbootGetPrimaryKeyDigest
	secbootGetPrimaryKeyDigest = f
	return func() {
		secbootGetPrimaryKeyDigest = old
	}
}

func MockVerifyPrimaryKeyDigest(f func(devicePath string, alg crypto.Hash, salt []byte, digest []byte) (bool, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking VerifyPrimaryKeyDigest can be done only from tests")

	old := secbootVerifyPrimaryKeyDigest
	secbootVerifyPrimaryKeyDigest = f
	return func() {
		secbootVerifyPrimaryKeyDigest = old
	}
}

func MockSecbootGetPCRHandle(f func(devicePath, keySlot, keyFile string, hintExpectFDEHook bool) (uint32, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking secboot.GetPCRHandle can be done only from tests")

	old := secbootGetPCRHandle
	secbootGetPCRHandle = f
	return func() {
		secbootGetPCRHandle = old
	}
}

func Repair(st *state.State) (*state.Change, error) {
	repair := st.NewTask("fde-repair-reseal", "Repair FDE encryption")
	chg := st.NewChange("fde-repair", "Reseal FDE keys")
	chg.AddTask(repair)

	st.EnsureBefore(0)

	return chg, nil
}

func doReseal(t *state.Task, tomb *tomb.Tomb) error {
	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err == device.ErrNoSealedKeys {
		return nil
	} else if err != nil {
		return err
	}

	st := t.State()
	st.Lock()
	defer st.Unlock()

	mgr := fdeMgr(st)

	switch method {
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		if err := backendRemoveBootChainCache(dirs.GlobalRootDir); err != nil {
			return err
		}
	}

	unlocker := st.Unlocker()

	return boot.WithBootChains(func(bc *boot.ResealKeyForBootChainsParams) error {
		const expectReseal = true
		return mgr.resealKeyForBootChains(unlocker, method, dirs.GlobalRootDir, bc, expectReseal)
	}, method)
}
