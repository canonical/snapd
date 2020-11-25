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

func validateEncryptionSupport(info *Info) error {
	for name, vol := range info.Volumes {
		var haveSave bool
		for _, s := range vol.Structure {
			if s.Role == SystemSave {
				haveSave = true
			}
		}
		if !haveSave {
			return fmt.Errorf("volume %q has no structure with system-save role", name)
		}
		// XXX: shall we make sure that size of ubuntu-save is reasonable?
	}
	return nil
}

type ValidationConstraints struct {
	// EncryptedData when true indicates that the gadget will be used on a
	// device where the data partition will be encrypted.
	EncryptedData bool
}

// Validate checks whether the given directory contains valid gadget snap
// metadata and a matching content, under the provided model constraints, which
// are handled identically to ReadInfo(). Optionally takes additional validation
// constraints, which for instance may only be known at run time,
func Validate(gadgetSnapRootDir string, model Model, extra *ValidationConstraints) error {
	info, err := ReadInfo(gadgetSnapRootDir, model)
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
	if extra != nil {
		if extra.EncryptedData {
			if err := validateEncryptionSupport(info); err != nil {
				return fmt.Errorf("gadget does not support encrypted data: %v", err)
			}
		}
	}
	return nil
}
