// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package gadget

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

var ErrDeviceNotFound = errors.New("device not found")

// FindDeviceForStructure attempts to find an existing device matching given
// volume structure, by inspecting its name and, optionally, the filesystem
// label. Assumes that the host's udev has set up device symlinks correctly.
func FindDeviceForStructure(ps *PositionedStructure) (string, error) {
	var candidates []string

	if ps.Name != "" {
		byPartlabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", strutil.HexEscapePath(ps.Name))
		candidates = append(candidates, byPartlabel)
	}

	if ps.Label != "" {
		byFsLabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", strutil.HexEscapePath(ps.Label))
		candidates = append(candidates, byFsLabel)
	}

	var found string
	for _, candidate := range candidates {
		if !osutil.FileExists(candidate) {
			continue
		}
		target, err := os.Readlink(candidate)
		if err != nil {
			return "", fmt.Errorf("cannot read device link: %v", err)
		}
		if found != "" && target != found {
			// partition label and filesystem label links point to
			// different devices
			return "", fmt.Errorf("conflicting device match, %q points to %q, while other match points to %q",
				candidate, target, found)
		}
		found = target
	}

	if found == "" {
		return "", ErrDeviceNotFound
	}

	return found, nil
}
