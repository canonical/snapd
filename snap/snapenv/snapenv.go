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

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap"
)

// Basic returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func Basic(info *snap.Info) []string {
	return []string{
		fmt.Sprintf("SNAP=%s", info.MountDir()),
		fmt.Sprintf("SNAP_COMMON=%s", info.CommonDataDir()),
		fmt.Sprintf("SNAP_DATA=%s", info.DataDir()),
		fmt.Sprintf("SNAP_NAME=%s", info.Name()),
		fmt.Sprintf("SNAP_VERSION=%s", info.Version),
		fmt.Sprintf("SNAP_REVISION=%s", info.Revision),
		fmt.Sprintf("SNAP_ARCH=%s", arch.UbuntuArchitecture()),
		"SNAP_LIBRARY_PATH=/var/lib/snapd/lib/gl:",
	}
}

// User returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func User(info *snap.Info, home string) []string {
	return []string{
		fmt.Sprintf("HOME=%s", info.UserDataDir(home)),
		fmt.Sprintf("SNAP_USER_COMMON=%s", info.UserCommonDataDir(home)),
		fmt.Sprintf("SNAP_USER_DATA=%s", info.UserDataDir(home)),
	}
}
