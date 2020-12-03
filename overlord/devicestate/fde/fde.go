// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020  Canonical Ltd
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

// SetupRequest carries the operation and parameters for the fde-setup hooks
// made available to them via the snapctl fde-setup-request command.
type SetupRequest struct {
	// XXX: make "op" a type: "features", "initial-setup", "update" ?
	Op string `json:"op"`

	Key     *secboot.EncryptionKey `json:"key,omitempty"`
	KeyName string                 `json:"key-name,omitempty"`

	// Model related fields, this will be set to follow the
	// secboot:SnapModel interface.
	//
	// XXX: do we need this to be a list? i.e. multiple models?
	Model map[string]string `json:"model,omitempty"`

	// TODO: provide LoadChains, KernelCmdline etc to support full
	//       tpm sealing
}
