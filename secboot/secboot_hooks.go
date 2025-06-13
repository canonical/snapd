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
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"

	sb "github.com/snapcore/secboot"
	sb_scope "github.com/snapcore/secboot/bootscope"
	sb_hooks "github.com/snapcore/secboot/hooks"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
)

var fdeHasRevealKey = fde.HasRevealKey
var sbSetModel = sb_scope.SetModel
var sbSetBootMode = sb_scope.SetBootMode
var sbSetKeyRevealer = sb_hooks.SetKeyRevealer

const legacyFdeHooksPlatformName = "fde-hook-v2"

func init() {
	v2Handler := &fdeHookV2DataHandler{}
	flags := sb.PlatformKeyDataHandlerFlags(0)
	sb.RegisterPlatformKeyDataHandler(legacyFdeHooksPlatformName, v2Handler, flags)
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
	if params.PrimaryKey != nil {
		// TODO:FDEM:FIX: add unit test taking that primary key
		primaryKey = params.PrimaryKey
	}

	for _, skr := range keys {
		protector := &hookKeyProtector{
			runHook: runHook,
			keyName: skr.KeyName,
		}
		// TODO:FDEM: add support for AEAD (consider OP-TEE work)
		flags := sb_hooks.KeyProtectorNoAEAD
		sb_hooks.SetKeyProtector(protector, flags)
		defer sb_hooks.SetKeyProtector(nil, 0)

		params := &sb_hooks.KeyParams{
			PrimaryKey: primaryKey,
			Role:       skr.KeyName,
			AuthorizedSnapModels: []sb.SnapModel{
				params.Model,
			},
			AuthorizedBootModes: skr.BootModes,
		}

		protectedKey, primaryKeyOut, unlockKey, err := sb_hooks.NewProtectedKey(rand.Reader, params)
		if err != nil {
			return err
		}

		if primaryKey == nil {
			primaryKey = primaryKeyOut
		}
		if err := skr.BootstrappedContainer.AddKey(skr.SlotName, unlockKey); err != nil {
			return err
		}

		keyWriter, err := skr.getWriter()
		if err != nil {
			return err
		}

		if err := protectedKey.WriteAtomic(keyWriter); err != nil {
			return err
		}

		if skr.SlotName == "default" {
			// "default" key will only be using hook on data disk. "save" disk will be handled
			// with the protector key.
			skr.BootstrappedContainer.RegisterKeyAsUsed(primaryKeyOut, unlockKey)
		}
	}

	return nil
}

func setAuthorizedSnapModelsOnHooksKeydataImpl(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, models ...sb.SnapModel) error {
	return kd.SetAuthorizedSnapModels(rand, key, models...)
}

var setAuthorizedSnapModelsOnHooksKeydata = setAuthorizedSnapModelsOnHooksKeydataImpl

func setAuthorizedBootModesOnHooksKeydataImpl(kd *sb_hooks.KeyData, rand io.Reader, key sb.PrimaryKey, bootmodes ...string) error {
	return kd.SetAuthorizedBootModes(rand, key, bootmodes...)
}

var setAuthorizedBootModesOnHooksKeydata = setAuthorizedBootModesOnHooksKeydataImpl

// ResealKeysWithFDESetupHook updates hook based keydatas for given
// files with a specific list of models
func ResealKeysWithFDESetupHook(keys []KeyDataLocation, primaryKeyGetter func() ([]byte, error), models []ModelForSealing, bootModes []string) error {
	var sbModels []sb.SnapModel
	for _, model := range models {
		sbModels = append(sbModels, model)
	}
	var primaryKey []byte

	for _, key := range keys {
		var keyDataWriter sb.KeyDataWriter

		keyData, tokenWriter, tokenErr := key.readTokenAndGetWriter()
		if tokenErr == nil {
			keyDataWriter = tokenWriter
		} else {
			loadedKey := &defaultKeyLoader{}
			const hintExpectFDEHook = true
			if err := readKeyFile(key.KeyFile, loadedKey, hintExpectFDEHook); err != nil {
				return tokenErr
			}

			// Non-nil FDEHookKeyV1 indicates that V1 hook key is used
			if loadedKey.FDEHookKeyV1 != nil {
				// V1 keys do not need resealing
				continue
			}

			if loadedKey.KeyData == nil {
				return fmt.Errorf("internal error: keydata was expected, but none found")
			}

			keyData = loadedKey.KeyData

			keyDataWriter = sb.NewFileKeyDataWriter(key.KeyFile)
		}

		if primaryKey == nil {
			pk, err := primaryKeyGetter()
			if err != nil {
				return err
			}
			primaryKey = pk
		}
		if keyData.Generation() == 1 {
			if err := keyData.SetAuthorizedSnapModels(primaryKey, sbModels...); err != nil {
				return err
			}
		} else {
			hooksKeyData, err := sb_hooks.NewKeyData(keyData)
			if err != nil {
				return err
			}
			if err := setAuthorizedSnapModelsOnHooksKeydata(hooksKeyData, rand.Reader, primaryKey, sbModels...); err != nil {
				return err
			}
			if err := setAuthorizedBootModesOnHooksKeydata(hooksKeyData, rand.Reader, primaryKey, bootModes...); err != nil {
				return err
			}
		}

		if err := keyData.WriteAtomic(keyDataWriter); err != nil {
			return err
		}
	}

	return nil
}

func unlockDiskWithHookV1Key(mapperName, sourceDevice string, sealed []byte) error {
	p := fde.RevealParams{
		SealedKey: sealed,
	}
	options := sb.ActivateVolumeOptions{}
	unlockKey, err := fde.Reveal(&p)
	if err != nil {
		return err
	}
	return sbActivateVolumeWithKey(mapperName, sourceDevice, unlockKey, &options)
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

func (fh *fdeHookV2DataHandler) ChangeAuthKey(data *sb.PlatformKeyData, old, new []byte, context any) ([]byte, error) {
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
