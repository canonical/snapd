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
	"strings"

	"github.com/snapcore/snapd/osutil"
)

func validateVolumeContentsPresence(gadgetSnapRootDir string, vol *LaidOutVolume) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for _, s := range vol.LaidOutStructure {
		if !s.HasFilesystem() {
			continue
		}
		for _, c := range s.Content {
			realSource := filepath.Join(gadgetSnapRootDir, c.Source)
			if !osutil.FileExists(realSource) {
				return fmt.Errorf("structure %v, content %v: source path does not exist", s, c)
			}
			if strings.HasSuffix(c.Source, "/") {
				// expecting a directory
				if err := checkSourceIsDir(realSource + "/"); err != nil {
					return fmt.Errorf("structure %v, content %v: %v", s, c, err)
				}
			}
		}
	}
	return nil
}

// Validate checks whether the given directory contains valid gadget snap
// metadata and a matching content, under the provided model constraints, which
// are handled identically to ReadInfo().
func Validate(gadgetSnapRootDir string, modelConstraints *ModelConstraints) error {
	info, err := ReadInfo(gadgetSnapRootDir, modelConstraints)
	if err != nil {
		return fmt.Errorf("invalid gadget metadata: %v", err)
	}

	for name, vol := range info.Volumes {
		lv, err := LayoutVolume(gadgetSnapRootDir, &vol, defaultConstraints)
		if err != nil {
			return fmt.Errorf("invalid layout of volume %q: %v", name, err)
		}
		if err := validateVolumeContentsPresence(gadgetSnapRootDir, lv); err != nil {
			return fmt.Errorf("invalid volume %q: %v", name, err)
		}
	}

	return nil
}
