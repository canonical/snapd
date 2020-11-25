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

package hookstate

import (
	"github.com/snapcore/snapd/secboot"
)

// FDESetupRequest is the struct that is passed the the fde-setup hooks
// via the `snapctl fde-setup-request` command.
type FDESetupRequest struct {
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
