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
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
)

var evalSymlinks = filepath.EvalSymlinks

// FindDeviceForStructure attempts to find an existing block device matching
// given volume structure, by inspecting its name and, optionally, the
// filesystem label. Assumes that the host's udev has set up device symlinks
// correctly.
func FindDeviceForStructure(ps *LaidOutStructure) (string, error) {
	var candidates []string

	if ps.Name != "" {
		byPartlabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", disks.BlkIDEncodeLabel(ps.Name))
		candidates = append(candidates, byPartlabel)
	}
	if ps.HasFilesystem() {
		fsLabel := ps.Label
		if fsLabel == "" && ps.Name != "" {
			// when image is built and the structure has no
			// filesystem label, the structure name will be used by
			// default as the label
			fsLabel = ps.Name
		}
		if fsLabel != "" {
			byFsLabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", disks.BlkIDEncodeLabel(fsLabel))
			candidates = append(candidates, byFsLabel)
		}
	}

	var found string
	var match string
	for _, candidate := range candidates {
		if !osutil.FileExists(candidate) {
			continue
		}
		if !osutil.IsSymlink(candidate) {
			// /dev/disk/by-label/* and /dev/disk/by-partlabel/* are
			// expected to be symlink
			return "", fmt.Errorf("candidate %v is not a symlink", candidate)
		}
		target, err := evalSymlinks(candidate)
		if err != nil {
			return "", fmt.Errorf("cannot read device link: %v", err)
		}
		if found != "" && target != found {
			// partition and filesystem label links point to
			// different devices
			return "", fmt.Errorf("conflicting device match, %q points to %q, previous match %q points to %q",
				candidate, target, match, found)
		}
		found = target
		match = candidate
	}

	if found == "" {
		return "", ErrDeviceNotFound
	}

	return found, nil
}
