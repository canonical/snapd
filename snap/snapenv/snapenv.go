// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snapenv

import (
	"fmt"
	"path/filepath"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap"
)

// GetBasicSnapEnvVars returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func Basic(app *snap.AppInfo) []string {
	return []string{
		fmt.Sprintf("SNAP=%s", app.Snap.MountDir()),
		fmt.Sprintf("SNAP_DATA=%s", app.Snap.DataDir()),
		fmt.Sprintf("SNAP_NAME=%s", app.Snap.Name()),
		fmt.Sprintf("SNAP_VERSION=%s", app.Snap.Version),
		fmt.Sprintf("SNAP_REVISION=%s", app.Snap.Revision),
		fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
	}
}

// User returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func User(app *snap.AppInfo, home string) []string {
	// FIXME: should go into PlacementInfo
	userData := filepath.Join(home, app.Snap.MountDir())
	return []string{
		fmt.Sprintf("SNAP_USER_DATA=%s", userData),
	}
}
