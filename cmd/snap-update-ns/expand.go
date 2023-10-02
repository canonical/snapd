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

// expandPrefixVariable expands variable at the beginning of a path-like string.
func expandPrefixVariable(path, variable, value string) string {
	if strings.HasPrefix(path, variable) {
		if len(path) == len(variable) {
			return value
		}
		if len(path) > len(variable) && path[len(variable)] == '/' {
			return value + path[len(variable):]
		}
	}
	return path
}

// xdgRuntimeDir returns the path to XDG_RUNTIME_DIR for a given user ID.
func xdgRuntimeDir(uid int) string {
	return fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, uid)
}

// expandXdgRuntimeDir expands the $XDG_RUNTIME_DIR variable in the given mount profile.
func expandXdgRuntimeDir(profile *osutil.MountProfile, uid int) {
	variable := "$XDG_RUNTIME_DIR"
	value := xdgRuntimeDir(uid)
	for i := range profile.Entries {
		profile.Entries[i].Name = expandPrefixVariable(profile.Entries[i].Name, variable, value)
		profile.Entries[i].Dir = expandPrefixVariable(profile.Entries[i].Dir, variable, value)
	}
}

// expandHomeDir expands the $HOME variable in the given mount profile for entries
// of mount kind "ensure-dir".
func expandHomeDir(profile *osutil.MountProfile, home string) {
	variable := "$HOME"
	for i := range profile.Entries {
		if profile.Entries[i].XSnapdKind() != "ensure-dir" {
			continue
		}
		profile.Entries[i].Name = expandPrefixVariable(profile.Entries[i].Name, variable, home)
		profile.Entries[i].Dir = expandPrefixVariable(profile.Entries[i].Dir, variable, home)
		if profile.Entries[i].XSnapdMustExistDir() != "" {
			mustExistDir := expandPrefixVariable(profile.Entries[i].XSnapdMustExistDir(), variable, home)
			osutil.ReplaceMountEntryOption(&profile.Entries[i], osutil.XSnapdMustExistDir(mustExistDir))
		}
	}
}
