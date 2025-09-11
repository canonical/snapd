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

package device

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// encryptionMarkerUnder returns the path of the encrypted system marker under a
// given directory.
func encryptionMarkerUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "marker")
}

// HasEncryptedMarkerUnder returns true when there is an encryption marker in a
// given directory.
func HasEncryptedMarkerUnder(deviceFDEDir string) bool {
	return osutil.FileExists(encryptionMarkerUnder(deviceFDEDir))
}

// ReadEncryptionMarkers reads the encryption marker files at the appropriate
// locations.
func ReadEncryptionMarkers(dataFDEDir, saveFDEDir string) ([]byte, []byte, error) {
	marker1, err := os.ReadFile(encryptionMarkerUnder(dataFDEDir))
	if err != nil {
		return nil, nil, err
	}
	marker2, err := os.ReadFile(encryptionMarkerUnder(saveFDEDir))
	if err != nil {
		return nil, nil, err
	}
	return marker1, marker2, nil
}

// WriteEncryptionMarkers writes the encryption marker files at the appropriate
// locations.
func WriteEncryptionMarkers(dataFDEDir, saveFDEDir string, markerSecret []byte) error {
	err := osutil.AtomicWriteFile(encryptionMarkerUnder(dataFDEDir), markerSecret, 0600, 0)
	if err != nil {
		return err
	}
	return osutil.AtomicWriteFile(encryptionMarkerUnder(saveFDEDir), markerSecret, 0600, 0)
}

// DataSealedKeyUnder returns the path of the sealed key for ubuntu-data.
func DataSealedKeyUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "ubuntu-data.sealed-key")
}

// SaveKeyUnder returns the path of a plain encryption key for ubuntu-save.
func SaveKeyUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "ubuntu-save.key")
}

// RecoveryKeyUnder returns the path of the recovery key.
func RecoveryKeyUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "recovery.key")
}

// FallbackDataSealedKeyUnder returns the path of a fallback ubuntu data key.
func FallbackDataSealedKeyUnder(seedDeviceFDEDir string) string {
	return filepath.Join(seedDeviceFDEDir, "ubuntu-data.recovery.sealed-key")
}

// FallbackSaveSealedKeyUnder returns the path of a fallback ubuntu save key.
func FallbackSaveSealedKeyUnder(seedDeviceFDEDir string) string {
	return filepath.Join(seedDeviceFDEDir, "ubuntu-save.recovery.sealed-key")
}

// FactoryResetFallbackSaveSealedKeyUnder returns the path of a fallback ubuntu
// save key object generated during factory reset.
func FactoryResetFallbackSaveSealedKeyUnder(seedDeviceFDEDir string) string {
	return filepath.Join(seedDeviceFDEDir, "ubuntu-save.recovery.sealed-key.factory-reset")
}

// TpmLockoutAuthUnder returns the path of the tpm lockout authority key.
func TpmLockoutAuthUnder(saveDeviceFDEDir string) string {
	return filepath.Join(saveDeviceFDEDir, "tpm-lockout-auth")
}

// PreinstallCheckResultUnder returns the path of the preinstall check result.
func PreinstallCheckResultUnder(deviceFDEDir string) string {
	return filepath.Join(deviceFDEDir, "preinstall")
}

// ErrNoSealedKeys error if there are no sealed keys
var ErrNoSealedKeys = errors.New("no sealed keys")

// SealingMethod represents the sealing method
type SealingMethod string

const (
	SealingMethodLegacyTPM    = SealingMethod("")
	SealingMethodTPM          = SealingMethod("tpm")
	SealingMethodFDESetupHook = SealingMethod("fde-setup-hook")
)

// StampSealedKeys writes what sealing method was used for key sealing
func StampSealedKeys(rootdir string, content SealingMethod) error {
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	if err := os.MkdirAll(filepath.Dir(stamp), 0755); err != nil {
		return fmt.Errorf("cannot create device fde state directory: %v", err)
	}

	if err := osutil.AtomicWriteFile(stamp, []byte(content), 0644, 0); err != nil {
		return fmt.Errorf("cannot create fde sealed keys stamp file: %v", err)
	}
	return nil
}

// SealedKeysMethod return whether any keys were sealed at all
func SealedKeysMethod(rootdir string) (sm SealingMethod, err error) {
	// TODO:UC20: consider more than the marker for cases where we reseal
	// outside of run mode
	stamp := filepath.Join(dirs.SnapFDEDirUnder(rootdir), "sealed-keys")
	content, err := os.ReadFile(stamp)
	if os.IsNotExist(err) {
		return sm, ErrNoSealedKeys
	}
	return SealingMethod(content), err
}

// EncryptionType specifies what encryption backend should be used (if any)
type EncryptionType string

const (
	EncryptionTypeNone        EncryptionType = ""
	EncryptionTypeLUKS        EncryptionType = "cryptsetup"
	EncryptionTypeLUKSWithICE EncryptionType = "cryptsetup-with-inline-crypto-engine"
)

// TODO:ICE: all EncryptionTypes are LUKS based now so this could be removed?
func (et EncryptionType) IsLUKS() bool {
	return et == EncryptionTypeLUKS || et == EncryptionTypeLUKSWithICE
}

// AuthMode corresponds to an authentication mechanism.
type AuthMode string

const (
	AuthModeNone       AuthMode = "none"
	AuthModePassphrase AuthMode = "passphrase"
	AuthModePIN        AuthMode = "pin"
)

// VolumesAuthOptions contains options for the volumes authentication
// mechanism (e.g. passphrase authentication).
//
// TODO: Add PIN option when secboot support lands.
type VolumesAuthOptions struct {
	Mode       AuthMode      `json:"mode,omitempty"`
	Passphrase string        `json:"passphrase,omitempty"`
	KDFType    string        `json:"kdf-type,omitempty"`
	KDFTime    time.Duration `json:"kdf-time,omitempty"`
}

// Validates authentication options.
func (o *VolumesAuthOptions) Validate() error {
	if o == nil {
		return nil
	}

	switch o.Mode {
	case AuthModePassphrase:
		if len(o.Passphrase) == 0 {
			return fmt.Errorf("passphrase cannot be empty")
		}
	case AuthModePIN:
		if o.KDFType != "" {
			return fmt.Errorf("%q authentication mode does not support custom kdf types", AuthModePIN)
		}
		return fmt.Errorf("%q authentication mode is not implemented", AuthModePIN)
	default:
		return fmt.Errorf("invalid authentication mode %q, only %q and %q modes are supported", o.Mode, AuthModePassphrase, AuthModePIN)
	}

	switch o.KDFType {
	case "argon2i", "argon2id", "pbkdf2", "":
	default:
		return fmt.Errorf("invalid kdf type %q, only \"argon2i\", \"argon2id\" and \"pbkdf2\" are supported", o.KDFType)
	}

	if o.KDFTime < 0 {
		return fmt.Errorf("kdf time cannot be negative")
	}

	return nil
}

type AuthQualityErrorReason string

const (
	AuthQualityErrorReasonLowEntropy AuthQualityErrorReason = "low-entropy"
)

// AuthQualityError contains rich inforamtion on why some auth value
// did not pass quality checks.
type AuthQualityError struct {
	// Reasons is a list of reason enums to explain exactly what quality
	// criteria failed e.g. AuthQualityErrorReasonLowEntropy.
	Reasons []AuthQualityErrorReason
	// Quality contains information about the calculated quality of the
	// specified auth value.
	Quality AuthQuality

	err error
}

func (e *AuthQualityError) Error() string {
	return e.err.Error()
}

type AuthQuality struct {
	// Entropy is the calculated entropy in bits for the passed passphrase
	// or PIN.
	Entropy uint32
	// MinEntropy is the minimum entropy in bits for the corresponding
	// authentication mode i.e. passhrase or PIN.
	MinEntropy uint32
	// OptimalEntropy is the recommended minimum entropy in bits for the
	// corresponding authentication mode i.e. passhrase or PIN.
	OptimalEntropy uint32
}

// Hook setup by secboot to calculate entropy for PINs and passphrases.
// PINs will be supplied as a numeric passphrase.
//
// Note: in most cases ValidatePassphrase should be used instead.
var EntropyBits func(passphrase string) (uint32, error) = entropyBitsImpl

func entropyBitsImpl(passphrase string) (uint32, error) {
	panic("entropy bits calculation callback not set up")
}

const (
	minPassphraseEntropyBits uint32 = 42
	// XXX: placeholder, needs a proper value
	optimalPassphraseEntropyBits uint32 = 100

	minPINEntropyBits uint32 = 13
	// XXX: placeholder, needs a proper value
	optimalPINEntropyBits uint32 = 64
)

// ValidatePassphrase checks quality of given passphrase or PIN based
// on their entropy. An AuthQualityError error is returned which contains
// more information about the given passphrase or PIN quality.
//
// PINs will be supplied as a numeric passphrase.
func ValidatePassphrase(mode AuthMode, passphrase string) (AuthQuality, error) {
	minEntropy, optimalEntropy := minPassphraseEntropyBits, optimalPassphraseEntropyBits
	if mode == AuthModePIN {
		minEntropy, optimalEntropy = minPINEntropyBits, optimalPINEntropyBits
	}

	entropy, err := EntropyBits(passphrase)
	if err != nil {
		return AuthQuality{}, err
	}

	result := AuthQuality{
		Entropy:        entropy,
		MinEntropy:     minEntropy,
		OptimalEntropy: optimalEntropy,
	}

	if entropy >= minEntropy {
		return result, nil
	}

	return AuthQuality{}, &AuthQualityError{
		Reasons: []AuthQualityErrorReason{AuthQualityErrorReasonLowEntropy},
		Quality: result,
		err:     fmt.Errorf("calculated entropy (%d bits) is less than the required minimum entropy (%d bits) for the %q authentication mode", entropy, minEntropy, mode),
	}
}
