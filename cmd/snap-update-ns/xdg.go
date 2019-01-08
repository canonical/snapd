// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

// xdgRuntimeDir returns the path to XDG_RUNTIME_DIR for a given user ID.
func xdgRuntimeDir(uid int) string {
	return fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, uid)
}

// expandXdgRuntimeDir expands the $XDG_RUNTIME_DIR variable in the given mount profile.
func expandXdgRuntimeDir(profile *osutil.MountProfile, uid int) {
	dir := xdgRuntimeDir(uid)
	for i := range profile.Entries {
		if strings.HasPrefix(profile.Entries[i].Name, "$XDG_RUNTIME_DIR/") {
			profile.Entries[i].Name = strings.Replace(profile.Entries[i].Name, "$XDG_RUNTIME_DIR", dir, 1)
		}
		if strings.HasPrefix(profile.Entries[i].Dir, "$XDG_RUNTIME_DIR/") {
			profile.Entries[i].Dir = strings.Replace(profile.Entries[i].Dir, "$XDG_RUNTIME_DIR", dir, 1)
		}
	}
}
