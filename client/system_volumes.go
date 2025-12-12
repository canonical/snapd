// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package client

import (
	"time"

	"github.com/snapcore/snapd/gadget/device"
)

type KeyslotType string

const (
	KeyslotTypeRecovery KeyslotType = "recovery"
	KeyslotTypePlatform KeyslotType = "platform"
)

type KeyslotInfo struct {
	Type KeyslotType `json:"type"`
	// only for platform key slots
	Roles        []string        `json:"roles,omitempty"`
	PlatformName string          `json:"platform-name,omitempty"`
	AuthMode     device.AuthMode `json:"auth-mode,omitempty"`
}

type SystemVolumesStructureInfo struct {
	VolumeName string                 `json:"volume-name"`
	Name       string                 `json:"name"`
	Encrypted  bool                   `json:"encrypted"`
	Keyslots   map[string]KeyslotInfo `json:"keyslots,omitempty"`
}

type SystemVolumesResult struct {
	ByContainerRole map[string]SystemVolumesStructureInfo `json:"by-container-role,omitempty"`
}

type SystemVolumesOptions struct {
	ContainerRoles  []string
	ByContainerRole bool
}

type ChangePassphraseOptions struct {
	OldPassphrase string `json:"old-passphrase"`
	NewPassphrase string `json:"new-passphrase"`
}

type ChangePINOptions struct {
	OldPIN string `json:"old-pin"`
	NewPIN string `json:"new-pin"`
}

type PlatformKeyOptions struct {
	Passphrase string `json:"passphrase,omitempty"`
	PIN        string `json:"pin,omitempty"`

	AuthMode device.AuthMode `json:"auth-mode,omitempty"`
	KDFType  string          `json:"kdf-type,omitempty"`
	KDFTime  time.Duration   `json:"kdf-time,omitempty"`
}
