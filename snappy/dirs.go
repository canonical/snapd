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

package snappy

import (
	"os"
	"path/filepath"
)

// the various file paths
var (
	globalRootDir string

	snapAppsDir      string
	snapOemDir       string
	snapDataDir      string
	snapDataHomeGlob string
	snapAppArmorDir  string

	snapBinariesDir string
	snapServicesDir string

	clickSystemHooksDir string
	cloudMetaDataFile   string
)

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	globalRootDir = rootdir

	snapAppsDir = filepath.Join(rootdir, "/apps")
	snapOemDir = filepath.Join(rootdir, "/oem")
	snapDataDir = filepath.Join(rootdir, "/var/lib/apps")
	snapDataHomeGlob = filepath.Join(rootdir, "/home/*/apps/")
	snapAppArmorDir = filepath.Join(rootdir, "/var/lib/apparmor/clicks")

	snapBinariesDir = filepath.Join(snapAppsDir, "bin")
	snapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")

	clickSystemHooksDir = filepath.Join(rootdir, "/usr/share/click/hooks")

	cloudMetaDataFile = filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net/meta-data")
}

func init() {
	// init the global directories at startup
	root := os.Getenv("SNAPPY_GLOBAL_ROOT")
	if root == "" {
		root = "/"
	}

	SetRootDir(root)
}
