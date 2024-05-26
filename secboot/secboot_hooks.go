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
	"os"

	"github.com/ddkwork/golibrary/mylog"
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
		res := mylog.Check2(fde.InitialSetup(runHook, keyParams))
		mylog.Check(writeKeyData(skr.KeyFile, res, auxKey, params.Model))

	}
	if params.AuxKeyFile != "" {
		mylog.Check(osutil.AtomicWriteFile(params.AuxKeyFile, auxKey, 0600, 0))
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
	kd := mylog.Check2(sb.NewKeyData(&sb.KeyCreationData{
		PlatformKeyData: sb.PlatformKeyData{
			EncryptedPayload: keySetup.EncryptedKey,
			Handle:           handle,
		},
		PlatformName:      fdeHooksPlatformName,
		AuxiliaryKey:      auxKey,
		SnapModelAuthHash: crypto.SHA256,
	}))
	mylog.Check(kd.SetAuthorizedSnapModels(auxKey, model))

	f := sb.NewFileKeyDataWriter(path)
	mylog.Check(kd.WriteAtomic(f))

	return nil
}

func isV1EncryptedKeyFile(p string) bool {
	// XXX move some of this to kernel/fde
	v1KeyPrefix := []byte("USK$")

	f := mylog.Check2(os.Open(p))

	defer f.Close()

	buf := make([]byte, len(v1KeyPrefix))
	mylog.Check2(io.ReadFull(f, buf))

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

	sealedKey := mylog.Check2(os.ReadFile(sealedEncryptionKeyFile))

	p := fde.RevealParams{
		SealedKey: sealedKey,
	}
	output := mylog.Check2(fde.Reveal(&p))

	// the output of fde-reveal-key is the unsealed key
	unsealedKey := output
	mylog.Check(unlockEncryptedPartitionWithKey(mapperName, sourceDevice, unsealedKey))

	res.FsDevice = targetDevice
	res.UnlockMethod = UnlockedWithSealedKey
	return res, nil
}

func unlockVolumeUsingSealedKeyFDERevealKeyV2(sealedEncryptionKeyFile, sourceDevice, targetDevice, mapperName string, opts *UnlockVolumeUsingSealedKeyOptions) (res UnlockResult, err error) {
	res = UnlockResult{IsEncrypted: true, PartDevice: sourceDevice}

	f := mylog.Check2(sb.NewFileKeyDataReader(sealedEncryptionKeyFile))

	keyData := mylog.Check2(sb.ReadKeyData(f))

	// the output of fde-reveal-key is the unsealed key
	options := activateVolOpts(opts.AllowRecoveryKey)
	modChecker := mylog.Check2(sbActivateVolumeWithKeyData(mapperName, sourceDevice, keyData, options))
	if err == sb.ErrRecoveryKeyUsed {
		logger.Noticef("successfully activated encrypted device %q using a fallback activation method", sourceDevice)
		res.FsDevice = targetDevice
		res.UnlockMethod = UnlockedWithRecoveryKey
		return res, nil
	}

	// ensure we close the open volume under any error condition
	defer func() {
	}()
	// ensure that the model is authorized to open the volume
	model := mylog.Check2(opts.WhichModel())

	ok := mylog.Check2(modChecker.IsModelAuthorized(model))

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
