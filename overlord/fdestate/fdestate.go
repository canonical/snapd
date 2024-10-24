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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

var errNotImplemented = errors.New("not implemented")

var (
	disksDMCryptUUIDFromMountPoint = disks.DMCryptUUIDFromMountPoint
	secbootGetPrimaryKeyHMAC       = secboot.GetPrimaryKeyHMAC
	secbootVerifyPrimaryKeyHMAC    = secboot.VerifyPrimaryKeyHMAC
)

// EFISecureBootDBManagerStartup indicates that the local EFI key database
// manager has started.
func EFISecureBootDBManagerStartup(st *state.State) error {
	if _, err := device.SealedKeysMethod(dirs.GlobalRootDir); err == device.ErrNoSealedKeys {
		return nil
	}

	return errNotImplemented
}

type EFISecurebootKeyDatabase int

const (
	EFISecurebootPK EFISecurebootKeyDatabase = iota
	EFISecurebootKEK
	EFISecurebootDB
	EFISecurebootDBX
)

// EFISecureBootDBUpdatePrepare notifies notifies that the local EFI key
// database manager is about to update the database.
func EFISecureBootDBUpdatePrepare(st *state.State, db EFISecurebootKeyDatabase, payload []byte) error {
	if _, err := device.SealedKeysMethod(dirs.GlobalRootDir); err == device.ErrNoSealedKeys {
		return nil
	}

	return errNotImplemented
}

// EFISecureBootDBUpdateCleanup notifies that the local EFI key database manager
// has reached a cleanup stage of the update process.
func EFISecureBootDBUpdateCleanup(st *state.State) error {
	if _, err := device.SealedKeysMethod(dirs.GlobalRootDir); err == device.ErrNoSealedKeys {
		return nil
	}

	return errNotImplemented
}

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

func newModel(m secboot.ModelForSealing) Model {
	return Model{
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
	Models []Model `json:"models,omitempty"`
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

// KeyDigest stores a HMAC(key, salt) of a key
// FIXME: take what is implemented in secboot
type KeyDigest struct {
	// Algorithm is the algorithm for
	Algorithm secboot.HashAlg `json:"alg"`
	// Salt is the salt for the HMAC digest
	Salt []byte `json:"salt"`
	// Digest is the result of `HMAC(key, salt)`
	Digest []byte `json:"digest"`
}

const defaultHashAlg = crypto.SHA256

func getPrimaryKeyHMAC(devicePath string) (KeyDigest, error) {
	salt, digest, err := secbootGetPrimaryKeyHMAC(devicePath, crypto.Hash(defaultHashAlg))
	if err != nil {
		return KeyDigest{}, err
	}

	return KeyDigest{
		Algorithm: secboot.HashAlg(defaultHashAlg),
		Salt:      salt,
		Digest:    digest,
	}, nil
}

func (kd *KeyDigest) verifyPrimaryKeyHMAC(devicePath string) (bool, error) {
	return secbootVerifyPrimaryKeyHMAC(devicePath, crypto.Hash(kd.Algorithm), kd.Salt, kd.Digest)
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
	PrimaryKeys map[int]PrimaryKeyInfo `json:"primary-keys"`

	// KeyslotRoles are all keyslot roles indexed by the role name
	KeyslotRoles map[string]KeyslotRoleInfo `json:"keyslot-roles"`
}

const fdeStateKey = "fde"

func initializeState(st *state.State) error {
	var s FdeState
	err := st.Get(fdeStateKey, &s)
	if err == nil {
		// TODO: Do we need to do something in recover?
		return nil
	}

	if !errors.Is(err, state.ErrNoState) {
		return err
	}

	// FIXME mount points will be different in recovery or factory-reset modes
	// either inspect degraded.json, or use boot.HostUbuntuDataForMode()
	dataUUID, dataErr := disksDMCryptUUIDFromMountPoint(dirs.SnapdStateDir(dirs.GlobalRootDir))
	saveUUID, saveErr := disksDMCryptUUIDFromMountPoint(dirs.SnapSaveDir)
	if errors.Is(saveErr, disks.ErrMountPointNotFound) {
		// TODO: do we need to care about old cases where there is no save partition?
		return nil
	}

	if errors.Is(dataErr, disks.ErrNoDmUUID) && errors.Is(saveErr, disks.ErrNoDmUUID) {
		// There is no encryption, so we ignore it.
		// TODO: we should verify the device "sealed key method"
		return nil
	}

	if dataErr != nil {
		return fmt.Errorf("cannot resolve data partition mount: %w", dataErr)
	}
	if saveErr != nil {
		return fmt.Errorf("cannot resolve save partition mount: %w", saveErr)
	}

	devpData := fmt.Sprintf("/dev/disk/by-uuid/%s", dataUUID)
	devpSave := fmt.Sprintf("/dev/disk/by-uuid/%s", saveUUID)
	digest, err := getPrimaryKeyHMAC(devpData)
	if err != nil {
		return fmt.Errorf("cannot obtain primary key digest for data device %s: %w", devpData, err)
	}
	// TODO: restore key verification once we know that it is always added to
	// the keyring
	sameDigest, err := digest.verifyPrimaryKeyHMAC(devpSave)
	if err != nil {
		if !errors.Is(err, secboot.ErrKernelKeyNotFound) {
			return fmt.Errorf("cannot verify primary key digest for save device %s: %w", devpSave, err)
		} else {
			logger.Noticef("cannot verify primary key digest for save device %s: %v", devpSave, err)
		}
	} else if !sameDigest {
		return fmt.Errorf("primary key for data and save partition are not the same")
	}

	s.PrimaryKeys = map[int]PrimaryKeyInfo{
		0: {
			Digest: digest,
		},
	}

	// Note that Parameters will be updated on first update
	s.KeyslotRoles = map[string]KeyslotRoleInfo{
		// TODO: use a constant
		"run": {
			PrimaryKeyID: 0,
			// FIXME: from Chris: Rather than hardcoding
			// an index value, I'd prefer us to adopt the
			// approach of picking a random index from a
			// small acceptable range. I could add an API
			// in secboot for that, or we could do it
			// here.
			TPM2PCRPolicyRevocationCounter: secboot.RunObjectPCRPolicyCounterHandle,
		},
		// TODO: use a constant
		"run+recover": {
			PrimaryKeyID: 0,
			// FIXME: from Chris: Rather than hardcoding
			// an index value, I'd prefer us to adopt the
			// approach of picking a random index from a
			// small acceptable range. I could add an API
			// in secboot for that, or we could do it
			// here.
			TPM2PCRPolicyRevocationCounter: secboot.RunObjectPCRPolicyCounterHandle,
		},
		// TODO: use a constant
		"recover": {
			PrimaryKeyID: 0,
			// FIXME: from Chris: Rather than hardcoding
			// an index value, I'd prefer us to adopt the
			// approach of picking a random index from a
			// small acceptable range. I could add an API
			// in secboot for that, or we could do it
			// here.
			TPM2PCRPolicyRevocationCounter: secboot.FallbackObjectPCRPolicyCounterHandle,
		},
	}

	st.Set(fdeStateKey, s)

	return nil
}

func updateParameters(st *state.State, role string, containerRole string, bootModes []string, models []secboot.ModelForSealing, tpmPCRProfile []byte) error {
	var s FdeState
	err := st.Get(fdeStateKey, &s)
	if err != nil {
		return err
	}

	roleInfo, hasRole := s.KeyslotRoles[role]
	if !hasRole {
		return fmt.Errorf("cannot find keyslot role %s", role)
	}

	var convertedModels []Model
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

	st.Set(fdeStateKey, s)

	return nil
}

func MockDMCryptUUIDFromMountPoint(f func(mountpoint string) (string, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking DMCryptUUIDFromMountPoint can be done only from tests")

	old := disksDMCryptUUIDFromMountPoint
	disksDMCryptUUIDFromMountPoint = f
	return func() {
		disksDMCryptUUIDFromMountPoint = old
	}
}

func MockGetPrimaryKeyHMAC(f func(devicePath string, alg crypto.Hash) ([]byte, []byte, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking GetPrimaryKeyHMAC can be done only from tests")

	old := secbootGetPrimaryKeyHMAC
	secbootGetPrimaryKeyHMAC = f
	return func() {
		secbootGetPrimaryKeyHMAC = old
	}
}

func MockVerifyPrimaryKeyHMAC(f func(devicePath string, alg crypto.Hash, salt []byte, digest []byte) (bool, error)) (restore func()) {
	osutil.MustBeTestBinary("mocking VerifyPrimaryKeyHMAC can be done only from tests")

	old := secbootVerifyPrimaryKeyHMAC
	secbootVerifyPrimaryKeyHMAC = f
	return func() {
		secbootVerifyPrimaryKeyHMAC = old
	}
}
