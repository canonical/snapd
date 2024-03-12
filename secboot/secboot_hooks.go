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
	"crypto"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var fdeHasRevealKey = fde.HasRevealKey

const fdeHooksPlatformName = "fde-hook-v2"

func init() {
	handler := &fdeHookV2DataHandler{}
	sb.RegisterPlatformKeyDataHandler(fdeHooksPlatformName, handler)
}

// SealKeysWithFDESetupHook protects the given keys through using the
// fde-setup hook and saves each protected key to the KeyFile
// indicated in the key SealKeyRequest.
func SealKeysWithFDESetupHook(runHook fde.RunSetupHookFunc, keys []SealKeyRequest, params *SealKeysWithFDESetupHookParams) error {
	auxKey := params.AuxKey[:]
	for _, skr := range keys {
		payload := sb.MarshalKeys([]byte(skr.Key), auxKey)
		keyParams := &fde.InitialSetupParams{
			Key:     payload,
			KeyName: skr.KeyName,
		}
		res, err := fde.InitialSetup(runHook, keyParams)
		if err != nil {
			return err
		}
		if err := writeKeyData(skr.KeyFile, res, auxKey, params.Model); err != nil {
			return fmt.Errorf("cannot store key: %v", err)
		}
	}
	if params.AuxKeyFile != "" {
		if err := osutil.AtomicWriteFile(params.AuxKeyFile, auxKey, 0600, 0); err != nil {
			return fmt.Errorf("cannot write the aux key file: %v", err)
		}
	}

	return nil
}

func writeKeyData(path string, keySetup *fde.InitialSetupResult, auxKey []byte, model sb.SnapModel) error {
	var handle []byte
	if keySetup.Handle == nil {
		// this will reach fde-reveal-key as null but should be ok
		handle = []byte("null")
	} else {
		handle = *keySetup.Handle
	}
	kd, err := sb.NewKeyData(&sb.KeyCreationData{
		PlatformKeyData: sb.PlatformKeyData{
			EncryptedPayload: keySetup.EncryptedKey,
			Handle:           handle,
		},
		PlatformName:      fdeHooksPlatformName,
		AuxiliaryKey:      auxKey,
		SnapModelAuthHash: crypto.SHA256,
	})
	if err != nil {
		return fmt.Errorf("cannot create key data: %v", err)
	}
	if err := kd.SetAuthorizedSnapModels(auxKey, model); err != nil {
		return fmt.Errorf("cannot set model %s/%s as authorized: %v", model.BrandID(), model.Model(), err)
	}
	f := sb.NewFileKeyDataWriter(path)
	if err := kd.WriteAtomic(f); err != nil {
		return fmt.Errorf("cannot write key data: %v", err)
	}
	return nil
}

func isV1EncryptedKeyFile(p string) bool {
	// XXX move some of this to kernel/fde
	var v1KeyPrefix = []byte("USK$")

	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, len(v1KeyPrefix))
	if _, err := io.ReadFull(f, buf); err != nil {
		return false
	}
	return bytes.HasPrefix(buf, v1KeyPrefix)
}

// We have to deal with the following cases:
// 1. Key created with v1 data-format on disk (raw encrypted key), v1 hook reads the data
// 2. Key created with v2 data-format on disk (json), v1 hook created the data (no handle) and reads the data (hook output not json but raw binary data)
// 3. Key created with v1 data-format on disk (raw), v2 hook
// 4. Key created with v2 data-format on disk (json), v2 hook [easy]
func unlockVolumeUsingSealedKeyFDERevealKey(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	// deal with v1 keys
	if isV1EncryptedKeyFile(sealedEncryptionKeyFile) {
		return unlockVolumeUsingSealedKeyFDERevealKeyV1(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName)
	}
	return unlockVolumeUsingSealedKeyFDERevealKeyV2(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
}

func unlockVolumeUsingSealedKeyFDERevealKeyV1(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string) (UnlockResult, error) {
	res := UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}

	sealedKey, err := ioutil.ReadFile(sealedEncryptionKeyFile)
	if err != nil {
		return res, fmt.Errorf("cannot read sealed key file: %v", err)
	}

	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	output, err := fde.Reveal(&p)
	if err != nil {
		return res, err
	}

	// the output of fde-reveal-key is the unsealed key
	unsealedKey := output
	if err := unlockEncryptedPartitionWithKey(mapperName, sourceDevice, unsealedKey); err != nil {
		return res, fmt.Errorf("cannot unlock encrypted partition: %v", err)
	}
	res.FsDevice = targetDevice
	res.UnlockMethod = UnlockedWithSealedKey
	return res, nil
}

func unlockVolumeUsingSealedKeyFDERevealKeyV2(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (res UnlockResult, err error) {
	res = UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}

	f, err := sb.NewFileKeyDataReader(sealedEncryptionKeyFile)
	if err != nil {
		return res, err
	}
	keyData, err := sb.ReadKeyData(f)
	if err != nil {
		fmt := "cannot read key data: %w"
		return res, xerrors.Errorf(fmt, err)
	}

	// the output of fde-reveal-key is the unsealed key
	options := activateVolOpts(opts.AllowRecoveryKey)
	modChecker, err := sbActivateVolumeWithKeyData(mapperName, sourceDevice, keyData, options)
	if err == sb.ErrRecoveryKeyUsed {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
		res.FsDevice = targetDevice
		res.UnlockMethod = UnlockedWithRecoveryKey
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("cannot unlock encrypted partition: %v", err)
	}
	// ensure we close the open volume under any error condition
	defer func() {
		if err != nil {
			if err := sbDeactivateVolume(mapperName); err != nil {
				logger.Noticef("cannot deactivate volume %q: %v", mapperName, err)
			}
		}
	}()
	// ensure that the model is authorized to open the volume
	model, err := opts.WhichModel()
	if err != nil {
		return res, fmt.Errorf("cannot retrieve which model to unlock for: %v", err)
	}
	ok, err := modChecker.IsModelAuthorized(model)
	if err != nil {
		return res, fmt.Errorf("cannot check if model is authorized to unlock disk: %v", err)
	}
	if !ok {
		return res, fmt.Errorf("cannot unlock volume: model %s/%s not authorized", model.BrandID(), model.Model())
	}

	logger.Noticef("successfully activated encrypted device %q using FDE kernel hooks", sourceDevice)
	res.FsDevice = targetDevice
	res.UnlockMethod = UnlockedWithSealedKey
	return res, nil
}

type fdeHookV2DataHandler struct{}

func (fh *fdeHookV2DataHandler) RecoverKeys(data *sb.PlatformKeyData) (sb.KeyPayload, error) {
	var handle *json.RawMessage
	if len(data.Handle) != 0 {
		rawHandle := json.RawMessage(data.Handle)
		handle = &rawHandle
	}
	p := fde.RevealParams{
		SealedKey: data.EncryptedPayload,
		Handle:    handle,
		V2Payload: true,
	}
	return fde.Reveal(&p)
}
