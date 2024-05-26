// -*- Mode: Go; indent-tabs-mode: t -*-

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

package fde

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/randutil"
)

// RevealKeyRequest carries the operation parameters to the fde-reavel-key
// helper that receives them serialized over stdin.
type RevealKeyRequest struct {
	Op string `json:"op"`

	SealedKey []byte           `json:"sealed-key,omitempty"`
	Handle    *json.RawMessage `json:"handle,omitempty"`
	// deprecated for v1
	KeyName string `json:"key-name,omitempty"`

	// TODO: add VolumeName,SourceDevicePath later
}

// runFDERevealKeyCommand returns the output of fde-reveal-key run
// with systemd.
//
// Note that systemd-run in the initrd can only talk to the private
// systemd bus so this cannot use "--pipe" or "--wait", see
// https://github.com/snapcore/core-initrd/issues/13
func runFDERevealKeyCommand(req *RevealKeyRequest) (output []byte, err error) {
	stdin := mylog.Check2(json.Marshal(req))

	return runFDEinitramfsHelper("fde-reveal-key", stdin)
}

var runFDERevealKey = runFDERevealKeyCommand

func MockRunFDERevealKey(mock func(*RevealKeyRequest) ([]byte, error)) (restore func()) {
	osutil.MustBeTestBinary("fde-reveal-key can only be mocked in tests")
	oldRunFDERevealKey := runFDERevealKey
	runFDERevealKey = mock
	return func() {
		runFDERevealKey = oldRunFDERevealKey
	}
}

func LockSealedKeys() error {
	req := &RevealKeyRequest{
		Op: "lock",
	}
	mylog.Check2(runFDERevealKey(req))

	return nil
}

// RevealParams contains the parameters for fde-reveal-key reveal operation.
type RevealParams struct {
	SealedKey []byte
	Handle    *json.RawMessage
	// V2Payload is set true if SealedKey is expected to contain a v2 payload
	// (disk key + aux key)
	V2Payload bool
}

type revealKeyResult struct {
	Key []byte `json:"key"`
}

const (
	v1keySize  = 64
	v1NoHandle = `{"v1-no-handle":true}`
)

// Reveal invokes the fde-reveal-key reveal operation.
func Reveal(params *RevealParams) (payload []byte, err error) {
	handle := params.Handle
	if params.V2Payload && handle != nil && bytes.Equal([]byte(*handle), []byte(v1NoHandle)) {
		handle = nil
	}
	req := &RevealKeyRequest{
		Op:        "reveal",
		SealedKey: params.SealedKey,
		Handle:    handle,
		// deprecated but needed for v1 hooks
		KeyName: "deprecated-" + randutil.RandomString(12),
	}
	output := mylog.Check2(runFDERevealKey(req))

	// We expect json output that fits the revealKeyResult json at
	// this point. However the "denver" project uses the old and
	// deprecated v1 API that returns raw bytes and we still need
	// to support this.
	var res revealKeyResult
	mylog.Check(json.Unmarshal(output, &res))

	// We expect a v2 payload but not having json
	// output from the hook means that either the
	// hook is buggy or we have a v1 based hook
	// (e.g. "denver" project) with v2 based json
	// data on disk. This is supported but we let
	// the higher levels unmarshaling of the
	// payload deal with the buggy case.

	// If the payload is not expected to be v2 and, the
	// output is not json but matches the size of the
	// "denver" project encrypton key (64 bytes) we assume
	// we deal with a v1 API.

	return res.Key, nil
}
