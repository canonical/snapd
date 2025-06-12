// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot

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
	"crypto"
	"errors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/secboot/keys"
)

var errBuildWithoutSecboot = errors.New("build without secboot support")

type DiskUnlockKey []byte

func CheckTPMKeySealingSupported(mode TPMProvisionMode) error {
	return errBuildWithoutSecboot
}

func SealKeys(keys []SealKeyRequest, params *SealKeysParams) ([]byte, error) {
	return nil, errBuildWithoutSecboot
}

func SealKeysWithFDESetupHook(runHook fde.RunSetupHookFunc, keys []SealKeyRequest, params *SealKeysWithFDESetupHookParams) error {
	return errBuildWithoutSecboot
}

func ResealKeys(params *ResealKeysParams) error {
	return errBuildWithoutSecboot
}

func ProvisionTPM(mode TPMProvisionMode, lockoutAuthFile string) error {
	return errBuildWithoutSecboot
}

func resetLockoutCounter(lockoutAuthFile string) error {
	return errBuildWithoutSecboot
}

type ActivateVolumeOptions struct {
}

func ActivateVolumeWithKey(volumeName, sourceDevicePath string, key []byte, options *ActivateVolumeOptions) error {
	return errBuildWithoutSecboot
}

func DeactivateVolume(volumeName string) error {
	return errBuildWithoutSecboot
}

func AddBootstrapKeyOnExistingDisk(node string, newKey keys.EncryptionKey) error {
	return errBuildWithoutSecboot
}

func RenameKeys(node string, renames map[string]string) error {
	return errBuildWithoutSecboot
}

func DeleteKeys(node string, matches map[string]bool) error {
	return errBuildWithoutSecboot
}

func BuildPCRProtectionProfile(modelParams []*SealKeyModelParams) (SerializedPCRProfile, error) {
	return nil, errBuildWithoutSecboot
}

func GetPrimaryKeyDigest(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
	return nil, nil, errBuildWithoutSecboot
}

func VerifyPrimaryKeyDigest(devicePath string, alg crypto.Hash, salt []byte, digest []byte) (bool, error) {
	return false, errBuildWithoutSecboot
}

func ResealKeysWithFDESetupHook(keys []KeyDataLocation, primaryKeyGetter func() ([]byte, error), models []ModelForSealing, bootModes []string) error {
	return errBuildWithoutSecboot
}

type HashAlg crypto.Hash

func (ha HashAlg) MarshalJSON() ([]byte, error) {
	return nil, errBuildWithoutSecboot
}

func (ha *HashAlg) UnmarshalJSON([]byte) error {
	return errBuildWithoutSecboot
}

func FindFreeHandle() (uint32, error) {
	return 0, errBuildWithoutSecboot
}

func GetPCRHandle(node, keySlot, keyFile string, hintExpectFDEHook bool) (uint32, error) {
	return 0, errBuildWithoutSecboot
}

func RemoveOldCounterHandles(node string, possibleOldKeys map[string]bool, possibleKeyFiles []string, hintExpectFDEHook bool) error {
	return errBuildWithoutSecboot
}

func TemporaryNameOldKeys(devicePath string) error {
	return errBuildWithoutSecboot
}

func DeleteOldKeys(devicePath string) error {
	return errBuildWithoutSecboot
}

func GetPrimaryKey(devices []string, fallbackKeyFile string) ([]byte, error) {
	return nil, errBuildWithoutSecboot
}

func CheckRecoveryKey(devicePath string, rkey keys.RecoveryKey) error {
	return errBuildWithoutSecboot
}

func ListContainerRecoveryKeyNames(devicePath string) ([]string, error) {
	return nil, errBuildWithoutSecboot
}

func ListContainerUnlockKeyNames(devicePath string) ([]string, error) {
	return nil, errBuildWithoutSecboot
}

func ReadContainerKeyData(devicePath, slotName string) (KeyData, error) {
	return nil, errBuildWithoutSecboot
}
