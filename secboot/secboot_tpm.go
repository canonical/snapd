// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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

package secboot

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/canonical/go-tpm2"
	"github.com/canonical/go-tpm2/mu"
	sb "github.com/snapcore/secboot"
	sb_efi "github.com/snapcore/secboot/efi"
	sb_tpm2 "github.com/snapcore/secboot/tpm2"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/efi"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap/snapfile"
)

const (
	keyringPrefix = "ubuntu-fde"
)

var (
	sbConnectToDefaultTPM                           = sb_tpm2.ConnectToDefaultTPM
	sbMeasureSnapSystemEpochToTPM                   = sb_tpm2.MeasureSnapSystemEpochToTPM
	sbMeasureSnapModelToTPM                         = sb_tpm2.MeasureSnapModelToTPM
	sbBlockPCRProtectionPolicies                    = sb_tpm2.BlockPCRProtectionPolicies
	sbefiAddPCRProfile                              = sb_efi.AddPCRProfile
	sbefiAddSystemdStubProfile                      = sb_efi.AddSystemdStubProfile
	sbAddSnapModelProfile                           = sb_tpm2.AddSnapModelProfile
	sbUpdateKeyPCRProtectionPolicyMultiple          = sb_tpm2.UpdateKeyPCRProtectionPolicyMultiple
	sbSealedKeyObjectRevokeOldPCRProtectionPolicies = (*sb_tpm2.SealedKeyObject).RevokeOldPCRProtectionPolicies
	sbNewFileKeyDataReader                          = sb.NewFileKeyDataReader
	sbReadKeyData                                   = sb.ReadKeyData
	sbReadSealedKeyObjectFromFile                   = sb_tpm2.ReadSealedKeyObjectFromFile
	sbNewTPMProtectedKey                            = sb_tpm2.NewTPMProtectedKey
	sbNewTPMPassphraseProtectedKey                  = sb_tpm2.NewTPMPassphraseProtectedKey
	sbNewKeyDataFromSealedKeyObjectFile             = sb_tpm2.NewKeyDataFromSealedKeyObjectFile

	randutilRandomKernelUUID = randutil.RandomKernelUUID

	isTPMEnabled                        = (*sb_tpm2.Connection).IsEnabled
	lockoutAuthSet                      = (*sb_tpm2.Connection).LockoutAuthSet
	sbTPMEnsureProvisioned              = (*sb_tpm2.Connection).EnsureProvisioned
	sbTPMEnsureProvisionedWithCustomSRK = (*sb_tpm2.Connection).EnsureProvisionedWithCustomSRK
	tpmReleaseResources                 = tpmReleaseResourcesImpl
	tpmGetCapabilityHandles             = (*sb_tpm2.Connection).GetCapabilityHandles

	sbTPMDictionaryAttackLockReset = (*sb_tpm2.Connection).DictionaryAttackLockReset

	sbUpdateKeyDataPCRProtectionPolicy = sb_tpm2.UpdateKeyDataPCRProtectionPolicy

	// check whether the interfaces match
	_ (sb.SnapModel) = ModelForSealing(nil)
)

func CheckTPMKeySealingSupported(mode TPMProvisionMode) error {
	logger.Noticef("checking if secure boot is enabled...")
	if err := checkSecureBootEnabled(); err != nil {
		logger.Noticef("secure boot not enabled: %v", err)
		return err
	}
	logger.Noticef("secure boot is enabled")

	logger.Noticef("checking if TPM device is available...")
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		err = fmt.Errorf("cannot connect to TPM device: %v", err)
		logger.Noticef("%v", err)
		return err
	}
	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		logger.Noticef("TPM device detected but not enabled")
		return fmt.Errorf("TPM device is not enabled")
	}
	if mode == TPMProvisionFull {
		if lockoutAuthSet(tpm) {
			logger.Noticef("TPM in lockout mode")
			return sb_tpm2.ErrTPMLockout
		}
	}

	logger.Noticef("TPM device detected and enabled")

	return nil
}

func checkSecureBootEnabled() error {
	// 8be4df61-93ca-11d2-aa0d-00e098032b8c is the EFI Global Variable vendor GUID
	b, _, err := efi.ReadVarBytes("SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c")
	if err != nil {
		if err == efi.ErrNoEFISystem {
			return err
		}
		return fmt.Errorf("cannot read secure boot variable: %v", err)
	}
	if len(b) < 1 {
		return errors.New("secure boot variable does not exist")
	}
	if b[0] != 1 {
		return errors.New("secure boot is disabled")
	}

	return nil
}

// initramfsPCR is the TPM PCR that we reserve for the EFI image and use
// for measurement from the initramfs.
const initramfsPCR = 12

func insecureConnectToTPM() (*sb_tpm2.Connection, error) {
	return sbConnectToDefaultTPM()
}

func measureWhenPossible(whatHow func(tpm *sb_tpm2.Connection) error) error {
	// the model is ready, we're good to try measuring it now
	tpm, err := insecureConnectToTPM()
	if err != nil {
		if xerrors.Is(err, sb_tpm2.ErrNoTPM2Device) {
			return nil
		}
		return fmt.Errorf("cannot open TPM connection: %v", err)
	}
	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		return nil
	}

	return whatHow(tpm)
}

// MeasureSnapSystemEpochWhenPossible measures the snap system epoch only if the
// TPM device is available. If there's no TPM device success is returned.
func MeasureSnapSystemEpochWhenPossible() error {
	measure := func(tpm *sb_tpm2.Connection) error {
		return sbMeasureSnapSystemEpochToTPM(tpm, initramfsPCR)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap system epoch: %v", err)
	}

	return nil
}

// MeasureSnapModelWhenPossible measures the snap model only if the TPM device is
// available. If there's no TPM device success is returned.
func MeasureSnapModelWhenPossible(findModel func() (*asserts.Model, error)) error {
	measure := func(tpm *sb_tpm2.Connection) error {
		model, err := findModel()
		if err != nil {
			return err
		}
		return sbMeasureSnapModelToTPM(tpm, initramfsPCR, model)
	}

	if err := measureWhenPossible(measure); err != nil {
		return fmt.Errorf("cannot measure snap model: %v", err)
	}

	return nil
}

func lockTPMSealedKeys() error {
	tpm, tpmErr := sbConnectToDefaultTPM()
	if tpmErr != nil {
		if xerrors.Is(tpmErr, sb_tpm2.ErrNoTPM2Device) {
			logger.Noticef("cannot open TPM connection: %v", tpmErr)
			return nil
		}
		return fmt.Errorf("cannot lock TPM: %v", tpmErr)
	}
	defer tpm.Close()

	// Lock access to the sealed keys. This should be called whenever there
	// is a TPM device detected, regardless of whether secure boot is enabled
	// or there is an encrypted volume to unlock. Note that snap-bootstrap can
	// be called several times during initialization, and if there are multiple
	// volumes to unlock we should lock access to the sealed keys only after
	// the last encrypted volume is unlocked, in which case lockKeysOnFinish
	// should be set to true.
	//
	// We should only touch the PCR that we've currently reserved for the kernel
	// EFI image. Touching others will break the ability to perform any kind of
	// attestation using the TPM because it will make the log inconsistent.
	return sbBlockPCRProtectionPolicies(tpm, []int{initramfsPCR})
}

func activateVolOpts(allowRecoveryKey bool, allowPassphrase bool, legacyPaths ...string) *sb.ActivateVolumeOptions {
	passphraseTry := 0
	if allowPassphrase {
		passphraseTry = 1
	}
	options := sb.ActivateVolumeOptions{
		PassphraseTries: passphraseTry,
		// disable recovery key by default
		RecoveryKeyTries:  0,
		KeyringPrefix:     keyringPrefix,
		LegacyDevicePaths: legacyPaths,
	}
	if allowRecoveryKey {
		// enable recovery key only when explicitly allowed
		options.RecoveryKeyTries = 3
	}
	return &options
}

func newAuthRequestor() sb.AuthRequestor {
	return NewSystemdAuthRequestor()
}

func readKeyTokenImpl(devicePath, slotName string) (*sb.KeyData, error) {
	kdReader, err := sb.NewLUKS2KeyDataReader(devicePath, slotName)
	if err != nil {
		return nil, err
	}
	return sb.ReadKeyData(kdReader)
}

var readKeyToken = readKeyTokenImpl

// TODO:FDEM: we do not really need an interface here, a struct would be
// enough.
type keyLoader interface {
	// LoadedKeyData keeps track of keys in KeyData format.
	LoadedKeyData(kd *sb.KeyData)
	// LoadedSealedKeyObject keeps track of sealed key object for
	// legacy TPM This is useful for resealing. For unlocking, a
	// matching KeyData will also be emitted.
	LoadedSealedKeyObject(sko *sb_tpm2.SealedKeyObject)
	// LoadedFDEHookKeyV1 keeps track of sealed key object for
	// legacy FDE hooks. In this case no KeyData is emitted.
	LoadedFDEHookKeyV1(sk []byte)
}

type defaultKeyLoader struct {
	KeyData         *sb.KeyData
	SealedKeyObject *sb_tpm2.SealedKeyObject
	FDEHookKeyV1    []byte
}

func (dkl *defaultKeyLoader) LoadedKeyData(kd *sb.KeyData) {
	dkl.KeyData = kd
}

func (dkl *defaultKeyLoader) LoadedSealedKeyObject(sko *sb_tpm2.SealedKeyObject) {
	dkl.SealedKeyObject = sko
}

func (dkl *defaultKeyLoader) LoadedFDEHookKeyV1(sk []byte) {
	dkl.FDEHookKeyV1 = sk
}

func hasOldSealedKeyPrefix(keyfile string) (bool, error) {
	f, err := os.Open(keyfile)
	if err != nil {
		return false, err
	}
	defer f.Close()

	var rawPrefix = []byte("USK$")

	buf := make([]byte, len(rawPrefix))
	l, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return false, err
	}

	return l == len(rawPrefix) && bytes.HasPrefix(buf, rawPrefix), nil
}

// readKeyFileImpl attempts to read a key file. It will call the
// different key loader methods depending on the key format found. In
// case of a legacy sealed key object format, it will decide whether
// it is for TPM or FDE hooks base on hintExpectFDEHook. For all cases
// but the v1 FDE hook sealed objects, a KeyData will be provided.  In
// the case of TPM sealed object, the key object itself will be
// provided. This is uselful for resealing, as the associated KeyData
// provided in that case will be enough for unlocking.
// TODO:FDEM: consider moving this to secboot
func readKeyFileImpl(keyfile string, kl keyLoader, hintExpectFDEHook bool) error {
	oldSealedKey, err := hasOldSealedKeyPrefix(keyfile)
	if err != nil {
		return err
	}

	switch {
	case oldSealedKey && hintExpectFDEHook:
		// FDE hook key v1
		//
		// It has the same magic header as sealed key object,
		// but there is no version information. Thus we need
		// to use a hint that we are using FDE hooks.
		sealedKey, err := os.ReadFile(keyfile)
		if err != nil {
			return fmt.Errorf("cannot read FDE hook v1 key: %v", err)
		}
		kl.LoadedFDEHookKeyV1(sealedKey)
		return nil

	case oldSealedKey && !hintExpectFDEHook:
		// TPM sealed object
		sealedObject, err := sbReadSealedKeyObjectFromFile(keyfile)
		if err != nil {
			return fmt.Errorf("cannot read key object: %v", err)
		}
		keyData, err := sbNewKeyDataFromSealedKeyObjectFile(keyfile)
		if err != nil {
			return fmt.Errorf("cannot read key object as key data: %v", err)
		}
		kl.LoadedKeyData(keyData)
		kl.LoadedSealedKeyObject(sealedObject)
		return nil

	default:
		reader, err := sbNewFileKeyDataReader(keyfile)
		if err != nil {
			return fmt.Errorf("cannot open key data: %v", err)
		}
		keyData, err := sbReadKeyData(reader)
		if err != nil {
			return err
		}
		kl.LoadedKeyData(keyData)
		return nil
	}
}

var readKeyFile = readKeyFileImpl

func (key KeyDataLocation) readTokenAndGetWriter() (*sb.KeyData, sb.KeyDataWriter, error) {
	kd, err := readKeyToken(key.DevicePath, key.SlotName)
	if err != nil {
		return nil, nil, err
	}
	writer, err := newLUKS2KeyDataWriter(key.DevicePath, key.SlotName)
	if err != nil {
		return nil, nil, err
	}
	return kd, writer, nil
}

// readKeyDataAndGetWriter reads key data or sealed key object from a token or a
// file. The key data could be placed either way since the
// installation decides where to store it. When resealing, we do not
// know about this decision that might have been done with another
// version of snapd. So it has to try from both the token and file. It
// will read in priority from the token. It may return either a
// KeyData or a SealedKeyObject depending on the format if read from a
// file. It will return only a KeyData if found in a token. If a
// KeyData is returned, then a KeyDataWriter is also returned.
// TODO:FDEM: consider moving this to secboot_sb.go
func readKeyDataAndGetWriter(key KeyDataLocation) (*sb.KeyData, *sb_tpm2.SealedKeyObject, sb.KeyDataWriter, error) {
	// We try with the token first. If we find it, we will ignore
	// the file.
	kd, writer, tokenErr := key.readTokenAndGetWriter()
	if tokenErr == nil {
		return kd, nil, writer, nil
	}

	// We did not find key data in the token. Let's try with the
	// file.
	loadedKey := &defaultKeyLoader{}
	const hintExpectFDEHook = false
	fileErr := readKeyFile(key.KeyFile, loadedKey, hintExpectFDEHook)

	if fileErr == nil {
		if loadedKey.SealedKeyObject == nil {
			return loadedKey.KeyData, nil, sb.NewFileKeyDataWriter(key.KeyFile), nil
		} else {
			// loadedKey.SealedKeyObject is not nil, then
			// the KeyData is just a work-around for
			// unlocking. Let's ignore it here.
			// There is no KeyDataWriter.
			return nil, loadedKey.SealedKeyObject, nil, nil
		}
	}

	return nil, nil, nil, fmt.Errorf(`trying to load key data from %s:%s returned "%v", and from %s returned "%v"`, key.DevicePath, key.SlotName, tokenErr, key.KeyFile, fileErr)
}

// ProvisionTPM provisions the default TPM and saves the lockout authorization
// key to the specified file.
func ProvisionTPM(mode TPMProvisionMode, lockoutAuthFile string) error {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	if err := tpmProvision(tpm, mode, lockoutAuthFile); err != nil {
		return err
	}
	return nil
}

// ProvisionForCVM provisions the default TPM using a custom SRK
// template that is created by the encrypt tool prior to first boot of
// Azure CVM instances. It takes UbuntuSeedDir (ESP) and expects
// "tpm2-srk.tmpl" there which is deleted after successful provision.
//
// Key differences with ProvisionTPM()
// - lack of TPM or if TPM is disabled is ignored.
// - it is fatal if TPM Provisioning requires a Lockout file
// - Custom SRK file is required in InitramfsUbuntuSeedDir
func ProvisionForCVM(initramfsUbuntuSeedDir string) error {
	tpm, err := insecureConnectToTPM()
	if err != nil {
		if xerrors.Is(err, sb_tpm2.ErrNoTPM2Device) {
			return nil
		}
		return fmt.Errorf("cannot open TPM connection: %v", err)
	}
	defer tpm.Close()

	if !isTPMEnabled(tpm) {
		return nil
	}

	srkTmplPath := filepath.Join(initramfsUbuntuSeedDir, "tpm2-srk.tmpl")
	f, err := os.Open(srkTmplPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("cannot open SRK template file: %v", err)
	}
	defer f.Close()

	var srkTmpl *tpm2.Public
	if _, err := mu.UnmarshalFromReader(f, mu.Sized(&srkTmpl)); err != nil {
		return fmt.Errorf("cannot read SRK template: %v", err)
	}

	err = sbTPMEnsureProvisionedWithCustomSRK(tpm, sb_tpm2.ProvisionModeWithoutLockout, nil, srkTmpl)
	if err != nil && err != sb_tpm2.ErrTPMProvisioningRequiresLockout {
		return fmt.Errorf("cannot prepare TPM: %v", err)
	}

	if err := os.Remove(srkTmplPath); err != nil {
		return fmt.Errorf("cannot remove SRK template file: %v", err)
	}

	return nil
}

func kdfOptions(volumesAuth *device.VolumesAuthOptions) (sb.KDFOptions, error) {
	switch volumesAuth.KDFType {
	case "":
		return nil, nil
	case "argon2id":
		return &sb.Argon2Options{
			Mode:           sb.Argon2id,
			TargetDuration: volumesAuth.KDFTime,
		}, nil
	case "argon2i":
		return &sb.Argon2Options{
			Mode:           sb.Argon2i,
			TargetDuration: volumesAuth.KDFTime,
		}, nil
	case "pbkdf2":
		return &sb.PBKDF2Options{
			TargetDuration: volumesAuth.KDFTime,
		}, nil
	default:
		return nil, fmt.Errorf("internal error: unknown kdfType passed %q", volumesAuth.KDFType)
	}
}

func newTPMProtectedKey(tpm *sb_tpm2.Connection, creationParams *sb_tpm2.ProtectKeyParams, volumesAuth *device.VolumesAuthOptions) (protectedKey *sb.KeyData, primaryKey sb.PrimaryKey, unlockKey sb.DiskUnlockKey, err error) {
	if volumesAuth != nil {
		switch volumesAuth.Mode {
		case device.AuthModePassphrase:
			kdfOptions, kdferr := kdfOptions(volumesAuth)
			if kdferr != nil {
				return nil, nil, nil, kdferr
			}
			passphraseParams := &sb_tpm2.PassphraseProtectKeyParams{
				ProtectKeyParams: *creationParams,
				KDFOptions:       kdfOptions,
			}
			protectedKey, primaryKey, unlockKey, err = sbNewTPMPassphraseProtectedKey(tpm, passphraseParams, volumesAuth.Passphrase)
		case device.AuthModePIN:
			// TODO: Implement PIN authentication mode.
			return nil, nil, nil, fmt.Errorf("%q authentication mode is not implemented", device.AuthModePIN)
		default:
			return nil, nil, nil, fmt.Errorf("internal error: invalid authentication mode %q", volumesAuth.Mode)
		}
	} else {
		protectedKey, primaryKey, unlockKey, err = sbNewTPMProtectedKey(tpm, creationParams)
	}

	return protectedKey, primaryKey, unlockKey, err
}

// SealKeys seals the encryption keys according to the specified parameters. The
// TPM must have already been provisioned. If sealed key already exists at the
// PCR handle, SealKeys will fail and return an error.
func SealKeys(keys []SealKeyRequest, params *SealKeysParams) ([]byte, error) {
	numModels := len(params.ModelParams)
	if numModels < 1 {
		return nil, fmt.Errorf("at least one set of model-specific parameters is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return nil, fmt.Errorf("TPM device is not enabled")
	}

	pcrProfile, err := buildPCRProtectionProfile(params.ModelParams)
	if err != nil {
		return nil, err
	}

	pcrHandle := params.PCRPolicyCounterHandle
	logger.Noticef("sealing with PCR handle %#x", pcrHandle)

	var primaryKey sb.PrimaryKey
	if params.PrimaryKey != nil {
		primaryKey = params.PrimaryKey
	}
	for _, key := range keys {
		creationParams := &sb_tpm2.ProtectKeyParams{
			PCRProfile:             pcrProfile,
			Role:                   params.KeyRole,
			PCRPolicyCounterHandle: tpm2.Handle(pcrHandle),
			PrimaryKey:             primaryKey,
		}
		protectedKey, primaryKeyOut, unlockKey, err := newTPMProtectedKey(tpm, creationParams, params.VolumesAuth)
		if primaryKey == nil {
			primaryKey = primaryKeyOut
		}
		if err != nil {
			return nil, err
		}
		if err := key.BootstrappedContainer.AddKey(key.SlotName, unlockKey); err != nil {
			return nil, err
		}

		keyWriter, err := key.getWriter()
		if err != nil {
			return nil, err
		}

		if err := protectedKey.WriteAtomic(keyWriter); err != nil {
			return nil, err
		}

		if key.SlotName == "default" {
			// "default" key will only be TPM on data disk. "save" disk will be handled
			// with the protector key.
			key.BootstrappedContainer.RegisterKeyAsUsed(primaryKeyOut, unlockKey)
		}
	}

	if primaryKey != nil && params.TPMPolicyAuthKeyFile != "" {
		if err := osutil.AtomicWriteFile(params.TPMPolicyAuthKeyFile, primaryKey, 0600, 0); err != nil {
			return nil, fmt.Errorf("cannot write the policy auth key file: %v", err)
		}
	}

	return primaryKey, nil
}

// MaybeSealedKeyData interface wraps a sb_tpm2.SealedKeyData
// This is mainly used to be able to mock sb_tpm2.SealedKeyData
type MaybeSealedKeyData interface {
	// Unwrap returns the sealed key data contained in this wrapper
	Unwrap() *sb_tpm2.SealedKeyData
}

func sbTPMRevokeOldPCRProtectionPoliciesImpl(key MaybeSealedKeyData, tpm *sb_tpm2.Connection, primaryKey []byte) error {
	return key.Unwrap().RevokeOldPCRProtectionPolicies(tpm, primaryKey)
}

var sbTPMRevokeOldPCRProtectionPolicies = sbTPMRevokeOldPCRProtectionPoliciesImpl

// UpdatedKeys is a collection of updated sealed key that can be used to
// revoke older keys.
// Sealed keys *may* use different policy counter, though in practice they
// all share the same counter. This is why we need a collection.
type UpdatedKeys []MaybeSealedKeyData

type actualSealedKeyData struct {
	data *sb_tpm2.SealedKeyData
}

func (a *actualSealedKeyData) Unwrap() *sb_tpm2.SealedKeyData {
	return a.data
}

// RevokeOldKeys goes through all of the updated keys and
// make sure to increase all the policy counters used by those
// keys to the value those keys use.
func (uk *UpdatedKeys) RevokeOldKeys(primaryKey []byte) error {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return fmt.Errorf("TPM device is not enabled")
	}

	for _, key := range *uk {
		if err := sbTPMRevokeOldPCRProtectionPolicies(key, tpm, primaryKey); err != nil {
			return err
		}
	}

	return nil
}

func sbNewSealedKeyDataImpl(k *sb.KeyData) (MaybeSealedKeyData, error) {
	kd, err := sb_tpm2.NewSealedKeyData(k)
	return &actualSealedKeyData{kd}, err
}

var sbNewSealedKeyData = sbNewSealedKeyDataImpl

// ResealKeys updates the PCR protection policy for the sealed encryption keys
// according to the specified parameters.
// If newPCRPolicyVersion is true, the keys will use a new policy version
// so that the policy counter can be incremented. The function will
// then also return the updated keys in order to increase the counter.
func ResealKeys(params *ResealKeysParams, newPCRPolicyVersion bool) (UpdatedKeys, error) {
	numSealedKeyObjects := len(params.Keys)
	if numSealedKeyObjects < 1 {
		return nil, fmt.Errorf("at least one key file is required")
	}

	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return nil, fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()
	if !isTPMEnabled(tpm) {
		return nil, fmt.Errorf("TPM device is not enabled")
	}

	var pcrProfile sb_tpm2.PCRProtectionProfile
	if _, err := mu.UnmarshalFromBytes(params.PCRProfile, &pcrProfile); err != nil {
		return nil, err
	}

	keyDatas := make([]*sb.KeyData, 0, numSealedKeyObjects)
	sealedKeyObjects := make([]*sb_tpm2.SealedKeyObject, 0, numSealedKeyObjects)
	writers := make([]sb.KeyDataWriter, 0, numSealedKeyObjects)
	for _, key := range params.Keys {
		keyData, keyObject, writer, err := readKeyDataAndGetWriter(key)
		if err != nil {
			return nil, err
		}
		if keyObject == nil {
			if writer == nil {
				return nil, fmt.Errorf("internal error: new keydata has no writer")
			}
			writers = append(writers, writer)
			keyDatas = append(keyDatas, keyData)
		} else {
			sealedKeyObjects = append(sealedKeyObjects, keyObject)
		}
	}

	hasOldSealedKeyObjects := len(sealedKeyObjects) != 0
	hasKeyDatas := len(keyDatas) != 0

	if hasOldSealedKeyObjects && hasKeyDatas {
		return nil, fmt.Errorf("key files are different formats")
	}

	var updatedKeys UpdatedKeys

	if hasOldSealedKeyObjects {
		if err := sbUpdateKeyPCRProtectionPolicyMultiple(tpm, sealedKeyObjects, params.PrimaryKey, &pcrProfile); err != nil {
			return nil, fmt.Errorf("cannot update legacy PCR protection policy: %w", err)
		}

		// write key files
		for i, sko := range sealedKeyObjects {
			w := sb_tpm2.NewFileSealedKeyObjectWriter(params.Keys[i].KeyFile)
			if err := sko.WriteAtomic(w); err != nil {
				return nil, fmt.Errorf("cannot write key data file %s: %w", params.Keys[i].KeyFile, err)
			}
		}

		// revoke old policies via the primary key object
		if err := sbSealedKeyObjectRevokeOldPCRProtectionPolicies(sealedKeyObjects[0], tpm, params.PrimaryKey); err != nil {
			return nil, fmt.Errorf("cannot revoke old PCR protection policies: %w", err)
		}
	} else {
		policyVersion := sb_tpm2.NoNewPCRPolicyVersion
		if newPCRPolicyVersion {
			policyVersion = sb_tpm2.NewPCRPolicyVersion
		}

		if err := sbUpdateKeyDataPCRProtectionPolicy(tpm, params.PrimaryKey, &pcrProfile, policyVersion, keyDatas...); err != nil {
			return nil, fmt.Errorf("cannot update PCR protection policy: %w", err)
		}

		for i, key := range params.Keys {
			if err := keyDatas[i].WriteAtomic(writers[i]); err != nil {
				return nil, fmt.Errorf("cannot write key data in keyfile %s:%s: %w", key.DevicePath, key.SlotName, err)
			}
		}

		if newPCRPolicyVersion {
			for i, keyData := range keyDatas {
				skd, err := sbNewSealedKeyData(keyData)
				if err != nil {
					key := params.Keys[i]
					return nil, fmt.Errorf("cannot get sealed key data for keyfile %s:%s: %w", key.DevicePath, key.SlotName, err)
				}

				updatedKeys = append(updatedKeys, skd)
			}
		}
	}

	return updatedKeys, nil
}

func buildPCRProtectionProfile(modelParams []*SealKeyModelParams) (*sb_tpm2.PCRProtectionProfile, error) {
	numModels := len(modelParams)
	modelPCRProfiles := make([]*sb_tpm2.PCRProtectionProfile, 0, numModels)

	for _, mp := range modelParams {
		var updateDB []*sb_efi.SignatureDBUpdate

		if len(mp.EFISignatureDbxUpdate) > 0 {
			updateDB = append(updateDB, &sb_efi.SignatureDBUpdate{
				Name: sb_efi.Dbx,
				Data: mp.EFISignatureDbxUpdate,
			})
		}

		modelProfile := sb_tpm2.NewPCRProtectionProfile()

		loadSequences, err := buildLoadSequences(mp.EFILoadChains)
		if err != nil {
			return nil, fmt.Errorf("cannot build EFI image load sequences: %v", err)
		}

		if err := sbefiAddPCRProfile(
			tpm2.HashAlgorithmSHA256,
			modelProfile.RootBranch(),
			loadSequences,
			sb_efi.WithSecureBootPolicyProfile(),
			sb_efi.WithBootManagerCodeProfile(),
			sb_efi.WithSignatureDBUpdates(updateDB...),
		); err != nil {
			return nil, fmt.Errorf("cannot add EFI secure boot and boot manager policy profiles: %v", err)
		}

		// Add systemd EFI stub profile
		if len(mp.KernelCmdlines) != 0 {
			systemdStubParams := sb_efi.SystemdStubProfileParams{
				PCRAlgorithm:   tpm2.HashAlgorithmSHA256,
				PCRIndex:       initramfsPCR,
				KernelCmdlines: mp.KernelCmdlines,
			}
			if err := sbefiAddSystemdStubProfile(modelProfile.RootBranch(), &systemdStubParams); err != nil {
				return nil, fmt.Errorf("cannot add systemd EFI stub profile: %v", err)
			}
		}

		// Add snap model profile
		if mp.Model != nil {
			snapModelParams := sb_tpm2.SnapModelProfileParams{
				PCRAlgorithm: tpm2.HashAlgorithmSHA256,
				PCRIndex:     initramfsPCR,
				Models:       []sb.SnapModel{mp.Model},
			}
			if err := sbAddSnapModelProfile(modelProfile.RootBranch(), &snapModelParams); err != nil {
				return nil, fmt.Errorf("cannot add snap model profile: %v", err)
			}
		}

		modelPCRProfiles = append(modelPCRProfiles, modelProfile)
	}

	pcrProfile := sb_tpm2.NewPCRProtectionProfile().AddProfileOR(modelPCRProfiles...)

	logger.Debugf("PCR protection profile:\n%s", pcrProfile.String())

	return pcrProfile, nil
}

// BuildPCRProtectionProfile builds and serializes a PCR profile from
// a list of SealKeyModelParams.
func BuildPCRProtectionProfile(modelParams []*SealKeyModelParams) (SerializedPCRProfile, error) {
	pcrProfile, err := buildPCRProtectionProfile(modelParams)
	if err != nil {
		return nil, err
	}
	return mu.MarshalToBytes(pcrProfile)
}

func tpmProvision(tpm *sb_tpm2.Connection, mode TPMProvisionMode, lockoutAuthFile string) error {
	var currentLockoutAuth []byte
	if mode == TPMPartialReprovision {
		logger.Debugf("using existing lockout authorization")
		d, err := os.ReadFile(lockoutAuthFile)
		if err != nil {
			return fmt.Errorf("cannot read existing lockout auth: %v", err)
		}
		currentLockoutAuth = d
	}
	// Create and save the lockout authorization file
	lockoutAuth := make([]byte, 16)
	// crypto rand is protected against short reads
	_, err := rand.Read(lockoutAuth)
	if err != nil {
		return fmt.Errorf("cannot create lockout authorization: %v", err)
	}
	if err := osutil.AtomicWriteFile(lockoutAuthFile, lockoutAuth, 0600, 0); err != nil {
		return fmt.Errorf("cannot write the lockout authorization file: %v", err)
	}

	// TODO:UC20: ideally we should ask the firmware to clear the TPM and then reboot
	//            if the device has previously been provisioned, see
	//            https://godoc.org/github.com/snapcore/secboot#RequestTPMClearUsingPPI
	if currentLockoutAuth != nil {
		// use the current lockout authorization data to authorize
		// provisioning
		tpm.LockoutHandleContext().SetAuthValue(currentLockoutAuth)
	}
	if err := sbTPMEnsureProvisioned(tpm, sb_tpm2.ProvisionModeFull, lockoutAuth); err != nil {
		logger.Noticef("TPM provisioning error: %v", err)
		return fmt.Errorf("cannot provision TPM: %v", err)
	}
	return nil
}

// buildLoadSequences builds EFI load image event trees from this package LoadChains
func buildLoadSequences(chains []*LoadChain) (loadseqs *sb_efi.ImageLoadSequences, err error) {
	// this will build load event trees for the current
	// device configuration, e.g. something like:
	//
	// shim -> recovery grub -> recovery kernel 1
	//                      |-> recovery kernel 2
	//                      |-> recovery kernel ...
	//                      |-> normal grub -> run kernel good
	//                                     |-> run kernel try

	loadseqs = sb_efi.NewImageLoadSequences()

	for _, chain := range chains {
		// root of load events has source Firmware
		loadseq, err := chain.loadEvent()
		if err != nil {
			return nil, err
		}
		loadseqs.Append(loadseq)
	}
	return loadseqs, nil
}

// loadEvent builds the corresponding load event and its tree
func (lc *LoadChain) loadEvent() (sb_efi.ImageLoadActivity, error) {
	var next []sb_efi.ImageLoadActivity
	for _, nextChain := range lc.Next {
		// everything that is not the root has source shim
		ev, err := nextChain.loadEvent()
		if err != nil {
			return nil, err
		}
		next = append(next, ev)
	}
	image, err := efiImageFromBootFile(lc.BootFile)
	if err != nil {
		return nil, err
	}
	return sb_efi.NewImageLoadActivity(image).Loads(next...), nil
}

func efiImageFromBootFile(b *bootloader.BootFile) (sb_efi.Image, error) {
	if b.Snap == "" {
		if !osutil.FileExists(b.Path) {
			return nil, fmt.Errorf("file %s does not exist", b.Path)
		}
		return sb_efi.FileImage(b.Path), nil
	}

	snapf, err := snapfile.Open(b.Snap)
	if err != nil {
		return nil, err
	}
	return sb_efi.NewSnapFileImage(
		snapf,
		b.Path,
	), nil
}

func tpmReleaseResourcesImpl(tpm *sb_tpm2.Connection, handle tpm2.Handle) error {
	rc, err := tpm.CreateResourceContextFromTPM(handle)
	if err != nil {
		if tpm2.IsResourceUnavailableError(err, handle) {
			// there's nothing to release, the handle isn't used
			return nil
		}
		return fmt.Errorf("cannot create resource context: %v", err)
	}
	if err := tpm.NVUndefineSpace(tpm.OwnerHandleContext(), rc, tpm.HmacSession()); err != nil {
		return fmt.Errorf("cannot undefine space: %v", err)
	}
	return nil
}

// releasePCRResourceHandles releases any TPM resources associated with given
// PCR handles.
// TODO:FDEM:FIX: were are not releasing PCR handles, but NV index handles. So
// the name is confusing
func releasePCRResourceHandles(handles ...uint32) error {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		err = fmt.Errorf("cannot connect to TPM device: %v", err)
		return err
	}
	defer tpm.Close()

	var errs []string
	for _, handle := range handles {
		logger.Debugf("releasing PCR handle %#x", handle)
		if err := tpmReleaseResources(tpm, tpm2.Handle(handle)); err != nil {
			errs = append(errs, fmt.Sprintf("handle %#x: %v", handle, err))
		}
	}
	if errCnt := len(errs); errCnt != 0 {
		return fmt.Errorf("cannot release TPM resources for %v handles:\n%v", errCnt, strings.Join(errs, "\n"))
	}
	return nil
}

func resetLockoutCounter(lockoutAuthFile string) error {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return fmt.Errorf("cannot connect to TPM: %v", err)
	}
	defer tpm.Close()

	lockoutAuth, err := os.ReadFile(lockoutAuthFile)
	if err != nil {
		return fmt.Errorf("cannot read existing lockout auth: %v", err)
	}
	tpm.LockoutHandleContext().SetAuthValue(lockoutAuth)

	if err := sbTPMDictionaryAttackLockReset(tpm, tpm.LockoutHandleContext(), tpm.HmacSession()); err != nil {
		return err
	}

	return nil
}

type mockableSealedKeyData interface {
	PCRPolicyCounterHandle() tpm2.Handle
}

type mockableSealedKeyObject interface {
	PCRPolicyCounterHandle() tpm2.Handle
}

type mockableKeyData interface {
	PlatformName() string
	GetTPMSealedKeyData() (mockableSealedKeyData, error)
}

type realSealedKeyData struct {
	*sb_tpm2.SealedKeyData
}

type realSealedKeyObject struct {
	*sb_tpm2.SealedKeyObject
}

type realKeyData struct {
	*sb.KeyData
}

func (keyData *realKeyData) GetTPMSealedKeyData() (mockableSealedKeyData, error) {
	skd, err := sb_tpm2.NewSealedKeyData(keyData.KeyData)
	if err != nil {
		return nil, err
	}
	return &realSealedKeyData{skd}, nil
}

func mockableReadKeyDataImpl(r sb.KeyDataReader) (mockableKeyData, error) {
	keyData, err := sb.ReadKeyData(r)
	if err != nil {
		return nil, err
	}
	return &realKeyData{keyData}, nil
}

var mockableReadKeyData = mockableReadKeyDataImpl

type mockableKeyLoader struct {
	KeyData         mockableKeyData
	SealedKeyObject mockableSealedKeyObject
	FDEHookKeyV1    []byte
}

func (dkl *mockableKeyLoader) LoadedKeyData(kd *sb.KeyData) {
	dkl.KeyData = &realKeyData{kd}
}

func (dkl *mockableKeyLoader) LoadedSealedKeyObject(sko *sb_tpm2.SealedKeyObject) {
	dkl.SealedKeyObject = &realSealedKeyObject{sko}
}

func (dkl *mockableKeyLoader) LoadedFDEHookKeyV1(sk []byte) {
	dkl.FDEHookKeyV1 = sk
}

func mockableReadKeyFileImpl(keyFile string, keyLoader *mockableKeyLoader, hintExpectFDEHook bool) error {
	return readKeyFile(keyFile, keyLoader, hintExpectFDEHook)
}

var mockableReadKeyFile = mockableReadKeyFileImpl

// GetPCRHandle returns the handle used by a key. The key will be
// searched on the token of the keySlot from the node. If that keySlot
// has no KeyData, then the key will be loaded at path keyFile.
func GetPCRHandle(node, keySlot, keyFile string, hintExpectFDEHook bool) (uint32, error) {
	slots, err := sbListLUKS2ContainerUnlockKeyNames(node)
	if err != nil {
		return 0, fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	var readKeyDataErr error
	for _, slot := range slots {
		if slot == keySlot {
			reader, err := sbNewLUKS2KeyDataReader(node, slot)
			if err != nil {
				// We save the error and try the file instead.
				readKeyDataErr = err
				break
			}
			keyData, err := mockableReadKeyData(reader)
			if err != nil {
				return 0, fmt.Errorf("cannot read key data for slot '%s': %w", keySlot, err)
			}
			if keyData.PlatformName() != "tpm2" {
				return 0, nil
			}
			sealedKeyData, err := keyData.GetTPMSealedKeyData()
			if err != nil {
				return 0, fmt.Errorf("cannot read sealed key data for slot '%s': %w", keySlot, err)
			}
			return uint32(sealedKeyData.PCRPolicyCounterHandle()), nil
		}
	}

	loadedKey := &mockableKeyLoader{}
	err = mockableReadKeyFile(keyFile, loadedKey, hintExpectFDEHook)
	if err != nil {
		if os.IsNotExist(err) {
			if readKeyDataErr != nil {
				// TODO:FDEM:FIX: secboot should tell us if
				// Data was nil, in that case we
				// should be silent, otherwise we
				// should return the error.
				logger.Noticef("WARNING: cannot read key data for slot %s: %v", keySlot, readKeyDataErr)
				return 0, nil
			}
			return 0, nil
		}
		return 0, fmt.Errorf("cannot read key file %s: %w", keyFile, err)
	}

	if loadedKey.SealedKeyObject != nil {
		return uint32(loadedKey.SealedKeyObject.PCRPolicyCounterHandle()), nil
	}

	if loadedKey.KeyData != nil {
		if loadedKey.KeyData.PlatformName() != "tpm2" {
			return 0, nil
		}
		sealedKeyData, err := loadedKey.KeyData.GetTPMSealedKeyData()
		if err != nil {
			return 0, fmt.Errorf("cannot read sealed key data from %s: %w", keyFile, err)
		}
		return uint32(sealedKeyData.PCRPolicyCounterHandle()), nil
	}

	return 0, nil
}

// RemoveOldCounterHandles releases TPM2 handles used by some keys.
// The keys for which handles are released are:
//   - in the keyslots of the given device, with names matching possibleOldKeys.
//   - in key files at paths given by possibleKeyFiles.
//
// All TPM2 handles found in any key found will be removed. If keyslots
// or key files are not found, they are just ignored.
// hintExpectFDEHook helps reading old key object files.  If not TPM2
// key is found, nothing happens.
func RemoveOldCounterHandles(device string, possibleOldKeys map[string]bool, possibleKeyFiles []string, hintExpectFDEHook bool) error {
	slots, err := sbListLUKS2ContainerUnlockKeyNames(device)
	if err != nil {
		return fmt.Errorf("cannot list slots in partition save partition: %v", err)
	}

	oldHandles := make(map[uint32]bool)

	for _, slot := range slots {
		if possibleOldKeys[slot] {
			reader, err := sbNewLUKS2KeyDataReader(device, slot)
			if err != nil {
				// TODO:FDEM:FIX: secboot should tell us if
				// Data was nil, in that case we
				// should be silent, otherwise we
				// should return the error.
				continue
			}
			keyData, err := mockableReadKeyData(reader)
			if err != nil {
				return fmt.Errorf("cannot read key data for slot '%s': %w", slot, err)
			}
			if keyData.PlatformName() != "tpm2" {
				continue
			}
			sealedKeyData, err := keyData.GetTPMSealedKeyData()
			if err != nil {
				return fmt.Errorf("cannot read sealed key data for slot '%s': %w", slot, err)
			}
			oldHandles[uint32(sealedKeyData.PCRPolicyCounterHandle())] = true
		}
	}

	for _, keyFile := range possibleKeyFiles {
		loadedKey := &mockableKeyLoader{}
		err := mockableReadKeyFile(keyFile, loadedKey, hintExpectFDEHook)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("cannot read key file %s: %w", keyFile, err)
		}
		if loadedKey.SealedKeyObject != nil {
			handle := uint32(loadedKey.SealedKeyObject.PCRPolicyCounterHandle())
			oldHandles[handle] = true
			// We used multiple handles before. But we
			// lost track of the handle for the run key as
			// we reformatted it already. Let's add the
			// matching run handle.
			if handle == AltFallbackObjectPCRPolicyCounterHandle {
				oldHandles[AltRunObjectPCRPolicyCounterHandle] = true
			} else if handle == FallbackObjectPCRPolicyCounterHandle {
				oldHandles[RunObjectPCRPolicyCounterHandle] = true
			} else {
				logger.Noticef("WARNING: we are deleting an unexpected handle. That should never have happened.")
			}
		} else if loadedKey.KeyData != nil {
			if loadedKey.KeyData.PlatformName() != "tpm2" {
				continue
			}
			sealedKeyData, err := loadedKey.KeyData.GetTPMSealedKeyData()
			if err != nil {
				return fmt.Errorf("cannot read sealed key data from %s: %w", keyFile, err)
			}
			oldHandles[uint32(sealedKeyData.PCRPolicyCounterHandle())] = true
		}
	}

	var oldHandlesList []uint32
	for handle := range oldHandles {
		switch {
		case handle == RunObjectPCRPolicyCounterHandle:
			fallthrough
		case handle == FallbackObjectPCRPolicyCounterHandle:
			fallthrough
		case handle == AltRunObjectPCRPolicyCounterHandle:
			fallthrough
		case handle == AltFallbackObjectPCRPolicyCounterHandle:
			fallthrough
		case handle >= PCRPolicyCounterHandleStart && handle < PCRPolicyCounterHandleStart+PCRPolicyCounterHandleRange:
			oldHandlesList = append(oldHandlesList, handle)
		}
	}

	if len(oldHandlesList) == 0 {
		return nil
	}

	return releasePCRResourceHandles(oldHandlesList...)
}

// FindFreeHandle finds and unused handle on the TPM.
// The handle will be arbitrary in range 0x01880005-0x0188000F.
func FindFreeHandle() (uint32, error) {
	tpm, err := sbConnectToDefaultTPM()
	if err != nil {
		return 0, fmt.Errorf("cannot connect to TPM: %w", err)
	}
	defer tpm.Close()

	handles, err := tpmGetCapabilityHandles(tpm, tpm2.Handle(PCRPolicyCounterHandleStart), PCRPolicyCounterHandleRange)
	if err != nil {
		return 0, fmt.Errorf("cannot get free handles: %w", err)
	}

	takenHandles := make(map[uint32]bool)
	for _, handle := range handles {
		logger.Debugf("TPM handle %v is in use", uint32(handle))
		takenHandles[uint32(handle)] = true
	}

	for _, tentative := range randutil.Perm(int(PCRPolicyCounterHandleRange)) {
		handle := PCRPolicyCounterHandleStart + uint32(tentative)
		if !takenHandles[PCRPolicyCounterHandleStart+uint32(tentative)] {
			logger.Debugf("TPM handle %v is free, taking it", uint32(handle))
			return handle, nil
		}
	}

	return 0, fmt.Errorf("no free handle on TPM")
}
