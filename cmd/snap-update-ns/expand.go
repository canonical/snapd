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

package main

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// expandPrefixVariable expands the given variable at the beginning of a path-like string if it exists.
func expandPrefixVariable(path, variable, value string) (string, bool) {
	if strings.HasPrefix(path, variable) {
		if len(path) == len(variable) {
			return value, true
		}
		if len(path) > len(variable) && path[len(variable)] == '/' {
			return value + path[len(variable):], true
		}
	}
	return path, false
}

// xdgRuntimeDir returns the path to XDG_RUNTIME_DIR for a given user ID.
func xdgRuntimeDir(uid int) string {
	return fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, uid)
}

// expandXdgRuntimeDir expands the $XDG_RUNTIME_DIR variable in the given mount profile.
func expandXdgRuntimeDir(profile *osutil.MountProfile, uid int) {
	const envVar = "$XDG_RUNTIME_DIR"
	value := xdgRuntimeDir(uid)
	for i := range profile.Entries {
		profile.Entries[i].Name, _ = expandPrefixVariable(profile.Entries[i].Name, envVar, value)
		profile.Entries[i].Dir, _ = expandPrefixVariable(profile.Entries[i].Dir, envVar, value)
	}
}

// expandHomeDir expands the $HOME variable in the given mount profile for entries
// of mount kind "ensure-dir". It returns an error if expansion is required but home
// err indicates that path should not be used for expansion.
func expandHomeDir(profile *osutil.MountProfile, home func() (path string, err error)) error {
	const envVar = "$HOME"
	homePath, homeErr := home()

	for i := range profile.Entries {
		if profile.Entries[i].XSnapdKind() != "ensure-dir" {
			continue
		}

		dir, dirExpanded := expandPrefixVariable(profile.Entries[i].Dir, envVar, homePath)
		mustExistDir, mustExistDirExpanded := expandPrefixVariable(profile.Entries[i].XSnapdMustExistDir(), envVar, homePath)

		if homeErr == nil {
			if dirExpanded {
				profile.Entries[i].Dir = dir
			}
			if mustExistDirExpanded {
				osutil.ReplaceMountEntryOption(&profile.Entries[i], osutil.XSnapdMustExistDir(mustExistDir))
			}
		} else if dirExpanded || mustExistDirExpanded {
			return fmt.Errorf("cannot expand mount entry (%s): %v", profile.Entries[i], homeErr)
		}
	}
	return nil
}
