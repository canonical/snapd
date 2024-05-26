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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
)

var evalSymlinks = filepath.EvalSymlinks

// FindDeviceForStructure attempts to find an existing block device matching
// given volume structure, by inspecting its name and, optionally, the
// filesystem label. Assumes that the host's udev has set up device symlinks
// correctly.
func FindDeviceForStructure(vs *VolumeStructure) (string, error) {
	var candidates []string

	if vs.Name != "" {
		byPartlabel := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/", disks.BlkIDEncodeLabel(vs.Name))
		candidates = append(candidates, byPartlabel)
	}
	if vs.HasFilesystem() {
		fsLabel := vs.Label
		if fsLabel == "" && vs.Name != "" {
			// when image is built and the structure has no
			// filesystem label, the structure name will be used by
			// default as the label
			fsLabel = vs.Name
		}
		if fsLabel != "" {
			candLabel := mylog.Check2(disks.CandidateByLabelPath(fsLabel))
			if err == nil {
				candidates = append(candidates, candLabel)
			} else {
				logger.Debugf("no by-label candidate for %q: %v", fsLabel, err)
			}
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
		target := mylog.Check2(evalSymlinks(candidate))

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
