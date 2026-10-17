// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package boot

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// sysrootStateFileName is the name of the file in the snap-bootstrap /run
// dir describing how /sysroot was mounted.
const sysrootStateFileName = "sysroot.json"

// SysrootIntegrity describes the integrity protection used to mount /sysroot.
type SysrootIntegrity struct {
	// Type is the integrity data type, e.g. "dm-verity".
	// TODO: switch to typed value e.g. integrity.IntegrityDataType
	Type string `json:"type"`
}

// SysrootState describes how /sysroot was mounted by snap-bootstrap.
type SysrootState struct {
	// Integrity is only set if the base was mounted using integrity data
	// from a verified snap-revision assertion.
	Integrity *SysrootIntegrity `json:"integrity,omitempty"`
}

// LoadSysrootState reads SysrootState from the snap-bootstrap /run dir.
//
// Returns os.ErrNotExist if the sysroot state file does not exist.
func LoadSysrootState() (*SysrootState, error) {
	b, err := os.ReadFile(filepath.Join(dirs.SnapBootstrapRunDir, sysrootStateFileName))
	if err != nil {
		return nil, err
	}

	var st SysrootState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, err
	}
	return &st, nil
}
