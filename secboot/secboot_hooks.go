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
	"encoding/json"
	"fmt"
	"io"
	"os"

	sb "github.com/snapcore/secboot"
	sb_scope "github.com/snapcore/secboot/bootscope"
	sb_hooks "github.com/snapcore/secboot/hooks"

	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var fdeHasRevealKey = fde.HasRevealKey
var sbSetModel = sb_scope.SetModel
var sbSetBootMode = sb_scope.SetBootMode
var sbSetKeyRevealer = sb_hooks.SetKeyRevealer

const legacyFdeHooksPlatformName = "fde-hook-v2"

func init() {
	handler := &fdeHookV2DataHandler{}
	sb.RegisterPlatformKeyDataHandler(legacyFdeHooksPlatformName, handler)
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
		// TODO: add support for AEAD
		flags := sb_hooks.KeyProtectorNoAEAD
		sb_hooks.SetKeyProtector(protector, flags)
		defer sb_hooks.SetKeyProtector(nil, 0)

		params := &sb_hooks.KeyParams{
			PrimaryKey: primaryKey,
			Role:       skr.KeyName,
			AuthorizedSnapModels: []sb.SnapModel{
				params.Model,
			},
			// TODO: add boot modes
		}

		protectedKey, primaryKeyOut, unlockKey, err := sb_hooks.NewProtectedKey(rand.Reader, params)
		if err != nil {
			return err
		}

		if primaryKey == nil {
			primaryKey = primaryKeyOut
		}
		const token = false
		if _, err := skr.BootstrappedContainer.AddKey(skr.SlotName, unlockKey, token); err != nil {
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
