// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023  Canonical Ltd
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
)

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
		handle := json.RawMessage(v1NoHandle)
		res.Handle = &handle
		res.EncryptedKey = hookOutput
	}

	return &res, nil
}

// TODO: unexport this because how the hook is driven is an implemenation
//
//	detail. It creates quite a bit of churn unfortunately, see
//	https://github.com/snapcore/snapd/compare/master...mvo5:ice/refactor-fde?expand=1
//
// SetupRequest carries the operation and parameters for the fde-setup hooks
// made available to them via the snapctl fde-setup-request command.
type SetupRequest struct {
	Op string `json:"op"`

	// This needs to be a []byte so that Go's standard library will base64
	// encode it automatically for us
	Key []byte `json:"key,omitempty"`

	AAD []byte `json:"aad,omitempty"`

	// Only used when called with "initial-setup"
	KeyName string `json:"key-name,omitempty"`

	// Name of the partition
	PartitionName string `json:"partition-name,omitempty"`
}

// A RunSetupHookFunc implements running the fde-setup kernel hook.
type RunSetupHookFunc func(req *SetupRequest) ([]byte, error)

// InitialSetupParams contains the inputs for the fde-setup hook
type InitialSetupParams struct {
	Key     []byte
	KeyName string
	AAD     []byte
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
		Key:     params.Key,
		KeyName: params.KeyName,
		AAD:     params.AAD,
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

// CheckFeatures returns the features of fde-setup hook.
func CheckFeatures(runSetupHook RunSetupHookFunc) ([]string, error) {
	req := &SetupRequest{
		Op: "features",
	}
	output, err := runSetupHook(req)
	if err != nil {
		return nil, err
	}
	var res struct {
		Features []string `json:"features"`
		Error    string   `json:"error"`
	}
	if err := json.Unmarshal(output, &res); err != nil {
		return nil, fmt.Errorf("cannot parse hook output %q: %v", output, err)
	}
	if res.Features == nil && res.Error == "" {
		return nil, fmt.Errorf(`cannot use hook: neither "features" nor "error" returned`)
	}
	if res.Error != "" {
		return nil, fmt.Errorf("cannot use hook: it returned error: %v", res.Error)
	}
	return res.Features, nil
}
