// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

/*
 * Copyright (C) 2021, 2024 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"io"
	"os"

	sb "github.com/snapcore/secboot"
	sb_hooks "github.com/snapcore/secboot/hooks"
	sb_scope "github.com/snapcore/secboot/bootscope"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var fdeHasRevealKey = fde.HasRevealKey
var sbSetModel = sb_scope.SetModel
var sbSetBootMode = sb_scope.SetBootMode
var sbSetKeyRevealer = sb_hooks.SetKeyRevealer

const fdeHooksPlatformName = "fde-hook-v2"

func init() {
	handler := &fdeHookV2DataHandler{}
	sb.RegisterPlatformKeyDataHandler(fdeHooksPlatformName, handler)
}

type hookKeyProtector struct {
	runHook fde.RunSetupHookFunc
	keyName string
}

func (h *hookKeyProtector) ProtectKey(rand io.Reader, cleartext, aad []byte) (ciphertext []byte, handle []byte, err error) {
	keyParams := &fde.InitialSetupParams{
		Key:     cleartext,
		KeyName: h.keyName,
	}
	res, err := fde.InitialSetup(h.runHook, keyParams)
	if err != nil {
		return nil, nil, err
	}
	if res.Handle == nil {
		return res.EncryptedKey, nil, nil
	} else {
		return res.EncryptedKey, *res.Handle, nil
	}
}

func SealKeysWithFDESetupHook(runHook fde.RunSetupHookFunc, keys []SealKeyRequest, params *SealKeysWithFDESetupHookParams) error {
	var primaryKey sb.PrimaryKey

	for _, skr := range keys {
		protector := &hookKeyProtector{
			runHook: runHook,
			keyName: skr.KeyName,
		}
		flags := sb_hooks.KeyProtectorNoAEAD
		sb_hooks.SetKeyProtector(protector, flags)
		defer sb_hooks.SetKeyProtector(nil, 0)

		params := &sb_hooks.KeyParams{
			PrimaryKey: primaryKey,
			Role:       skr.KeyName,
			AuthorizedSnapModels: []sb.SnapModel{
				params.Model,
			},
			AuthorizedBootModes: []string{
				"run",
			},
		}
		protectedKey, primaryKeyOut, unlockKey, err := sb_hooks.NewProtectedKey(rand.Reader, params)
		if err != nil {
			return err
		}
		if primaryKey == nil {
			primaryKey = primaryKeyOut
		}
		const token = false
		if _, err := skr.Resetter.AddKey(skr.SlotName, unlockKey, token); err != nil {
			return err
		}
		writer := sb.NewFileKeyDataWriter(skr.KeyFile)
		if err := protectedKey.WriteAtomic(writer); err != nil {
			return err
		}
	}
	if primaryKey != nil && params.AuxKeyFile != "" {
		if err := osutil.AtomicWriteFile(params.AuxKeyFile, primaryKey, 0600, 0); err != nil {
			return fmt.Errorf("cannot write the policy auth key file: %v", err)
		}
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

	sealedKey, err := os.ReadFile(sealedEncryptionKeyFile)
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

	model, err := opts.WhichModel()
	if err != nil {
		return res, fmt.Errorf("cannot retrieve which model to unlock for: %v", err)
	}

	// the output of fde-reveal-key is the unsealed key
	options := activateVolOpts(opts.AllowRecoveryKey)
	options.Model = model

	sbSetModel(model)
	//defer sbSetModel(nil)
	sbSetBootMode("run")
	//defer sbSetBootMode("")
	sbSetKeyRevealer(&keyRevealerV3{})
	defer sbSetKeyRevealer(nil)

	authRequestor, err := newAuthRequestor()
	if err != nil {
		return res, fmt.Errorf("cannot build an auth requestor: %v", err)
	}

	err = sbActivateVolumeWithKeyData(mapperName, sourceDevice, authRequestor, options, keyData)
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

	logger.Noticef("successfully activated encrypted device %q using FDE kernel hooks", sourceDevice)
	res.FsDevice = targetDevice
	res.UnlockMethod = UnlockedWithSealedKey
	return res, nil
}

type fdeHookV2DataHandler struct{}

func (fh *fdeHookV2DataHandler) RecoverKeys(data *sb.PlatformKeyData, encryptedPayload []byte) ([]byte, error) {
	var handle *json.RawMessage
	if len(data.EncodedHandle) != 0 {
		rawHandle := json.RawMessage(data.EncodedHandle)
		handle = &rawHandle
	}
	p := fde.RevealParams{
		SealedKey: encryptedPayload,
		Handle:    handle,
		V2Payload: true,
	}
	return fde.Reveal(&p)
}

func (fh *fdeHookV2DataHandler) ChangeAuthKey(data *sb.PlatformKeyData, old, new []byte) ([]byte, error) {
	return nil, fmt.Errorf("cannot change auth key yet")
}

func (fh *fdeHookV2DataHandler) RecoverKeysWithAuthKey(data *sb.PlatformKeyData, encryptedPayload, key []byte) ([]byte, error) {
	return nil, fmt.Errorf("cannot recover keys with auth keys yet")
}

type keyRevealerV3 struct {
}

func (kr *keyRevealerV3) RevealKey(data, ciphertext, aad []byte) (plaintext []byte, err error) {
	logger.Noticef("Called reveal key")
	var handle *json.RawMessage
	if len(data) != 0 {
		rawHandle := json.RawMessage(data)
		handle = &rawHandle
	}
	p := fde.RevealParams{
		SealedKey: ciphertext,
		Handle:    handle,
		V2Payload: true,
	}
	return fde.Reveal(&p)
}
