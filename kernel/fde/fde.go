// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021  Canonical Ltd
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

// package fde implements helper used by low level parts like secboot
// in snap-bootstrap and high level parts like DeviceManager in snapd.
//
// Note that it must never import anything overlord related itself
// to avoid increasing the size of snap-bootstrap.
package fde

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/snapcore/snapd/secboot"
)

func init() {
	secboot.FDEHasRevealKey = HasRevealKey
}

// HasRevealKey return true if the current system has a "fde-reveal-key"
// binary (usually used in the initrd).
//
// This will be setup by devicestate to support device-specific full
// disk encryption implementations.
func HasRevealKey() bool {
	// XXX: should we record during initial sealing that the fde-setup
	//      was used and only use fde-reveal-key in that case?
	_, err := exec.LookPath("fde-reveal-key")
	return err == nil
}

func isV1Hook(hookOutput []byte) bool {
	// This is the prefix of a tpm secboot v1 key as used in the
	// "denver" project. So if we see this prefix we know it's
	// v1 hook output.
	return bytes.HasPrefix(hookOutput, []byte("USK$"))
}

func unmarshalInitialSetupResult(hookOutput []byte) (*InitialSetupResult, error) {
	// We expect json output that fits InitalSetupResult
	// hook at this point. However the "denver" project
	// uses the old and deprecated v1 API that returns raw
	// bytes and we still need to support this.
	var res InitialSetupResult
	if err := json.Unmarshal(hookOutput, &res); err != nil {
		// If the outout is not json and looks like va
		if !isV1Hook(hookOutput) {
			return nil, fmt.Errorf("cannot decode hook output %q: %v", hookOutput, err)
		}
		// v1 hooks do not support a handle
		handle := json.RawMessage("{v1-no-handle: true}")
		res.Handle = &handle
		res.EncryptedKey = hookOutput
	}

	return &res, nil
}

// SetupRequest carries the operation and parameters for the fde-setup hooks
// made available to them via the snapctl fde-setup-request command.
type SetupRequest struct {
	// XXX: make "op" a type: "features", "initial-setup", "update" ?
	Op string `json:"op"`

	// This needs to be a []byte so that Go's standard library will base64
	// encode it automatically for us
	Key []byte `json:"key,omitempty"`

	// TODO: provide LoadChains, KernelCmdline etc to support full
	//       tpm sealing
}

// A RunSetupHookFunc implements running the fde-setup kernel hook.
type RunSetupHookFunc func(req *SetupRequest) ([]byte, error)

// InitialSetupParams contains the inputs for the fde-setup hook
type InitialSetupParams struct {
	Key     secboot.EncryptionKey
	KeyName string

	//TODO:UC20: provide bootchains and a way to track measured
	//boot-assets
}

// InitalSetupResult contains the outputs of the fde-setup hook
type InitialSetupResult struct {
	// result when called with "initial-setup"
	// XXX call this encrypted-key if possible?
	EncryptedKey []byte           `json:"sealed-key"`
	Handle       *json.RawMessage `json:"handle"`
}

// InitialSetup invokes the initial-setup op running the kernel hook via runSetupHook.
func InitialSetup(runSetupHook RunSetupHookFunc, params *InitialSetupParams) (*InitialSetupResult, error) {
	req := &SetupRequest{
		Op:      "initial-setup",
		Key:     params.Key[:],
		KeyName: params.KeyName,
	}
	hookOutput, err := runSetupHook(req)
	if err != nil {
		return nil, err
	}
	res, err := unmarshalInitialSetupResult(hookOutput)
	if err != nil {
		return nil, err
	}
	return res, nil
}
