// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package disks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
)

// CandidateByLabelPath searches for a filesystem with label matching
// "label". It tries first an exact match, otherwise it tries again by
// ignoring capitalization, but in that case it will return a match
// only if the filesystem is vfat. If found, it returns the path of
// the symlink in the by-label folder.
func CandidateByLabelPath(label string) (string, error) {
	byLabelDir := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/")
	byLabelFs := mylog.Check2(os.ReadDir(byLabelDir))

	candidate := ""
	// encode it so it can be compared with the files
	label = BlkIDEncodeLabel(label)
	// Search first for an exact match
	for _, file := range byLabelFs {
		if file.Name() == label {
			candidate = file.Name()
			logger.Debugf("found candidate %q for gadget label %q",
				candidate, label)
			break
		}
	}
	if candidate == "" {
		// Now try to find a candidate ignoring case, which
		// will be fine only for vfat partitions.
		labelLow := strings.ToLower(label)
		for _, file := range byLabelFs {
			if strings.ToLower(file.Name()) == labelLow {
				if candidate != "" {
					return "", fmt.Errorf("more than one candidate for label %q", label)
				}
				candidate = file.Name()
			}
		}
		if candidate == "" {
			return "", fmt.Errorf("no candidate found for label %q", label)
		}
		// Make sure it is vfat
		fsType := mylog.Check2(filesystemTypeForPartition(filepath.Join(byLabelDir, candidate)))

		if fsType != "vfat" {
			return "", fmt.Errorf("no candidate found for label %q (%q is not vfat)", label, candidate)
		}
		logger.Debugf("found candidate %q (vfat) for gadget label %q",
			candidate, label)
	}

	return filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/", candidate), nil
}
