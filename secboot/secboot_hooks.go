// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	sb "github.com/snapcore/secboot"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
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
func SealKeysWithFDESetupHook(runHook fde.RunSetupHookFunc, keys []SealKeyRequest) error {
	for _, skr := range keys {
		params := &fde.InitialSetupParams{
			Key:     skr.Key,
			KeyName: skr.KeyName,
		}
		res, err := fde.InitialSetup(runHook, params)
		if err != nil {
			return err
		}
		if err := osutil.AtomicWriteFile(skr.KeyFile, res.EncryptedKey, 0600, 0); err != nil {
			return fmt.Errorf("cannot store key: %v", err)
		}
	}

	return nil
}

/*

func MarshalKeys(key []byte, auxKey []byte) []byte {
	return sb.MarshalKeys(key, auxKey)
}

func WriteKeyData(name, path string, encryptedPayload, auxKey []byte, rawhandle *json.RawMessage) error {
	handle, err := json.Marshal(*rawhandle)
	if err != nil {
		return err
	}
	kd, err := sb.NewKeyData(&sb.KeyCreationData{
		PlatformKeyData: sb.PlatformKeyData{
			EncryptedPayload: encryptedPayload,
			Handle:           handle,
		},
		PlatformName:      fdeHooksPlatformName,
		AuxiliaryKey:      auxKey,
		SnapModelAuthHash: crypto.SHA256,
	})
	if err != nil {
		return fmt.Errorf("cannot create key-data: %v", err)
	}
	f := sb.NewFileKeyDataWriter(name, path)
	if err := kd.WriteAtomic(f); err != nil {
		return fmt.Errorf("cannot write key-data: %v", err)
	}

	return nil
}
*/

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
func unlockVolumeUsingSealedKeyFDERevealKey(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	// deal with v1 keys
	if isV1EncryptedKeyFile(sealedEncryptionKeyFile) {
		return unlockVolumeUsingSealedKeyFDERevealKeyV1(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
	}
	return unlockVolumeUsingSealedKeyFDERevealKeyV2(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName, opts)
}

func unlockVolumeUsingSealedKeyFDERevealKeyV1(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
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

func unlockVolumeUsingSealedKeyFDERevealKeyV2(name, sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (UnlockResult, error) {
	res := UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}

	f, err := sb.NewFileKeyDataReader(sealedEncryptionKeyFile)
	if err != nil {
		return res, err
	}
	keyData, err := sb.ReadKeyData(f)
	if err != nil {
		fmt := "cannot read key data: %w"
		return res, xerrors.Errorf(fmt, err)
	}
	key, _, err := keyData.RecoverKeys()
	if err != nil {
		return res, err
	}

	// the output of fde-reveal-key is the unsealed key
	if err := unlockEncryptedPartitionWithKey(mapperName, sourceDevice, key); err != nil {
		return res, fmt.Errorf("cannot unlock encrypted partition: %v", err)
	}

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
