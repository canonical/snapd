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

package dirs

import "path/filepath"

// the various file paths
var (
	GlobalRootDir string

	SnapAppsDir               string
	SnapBlobDir               string
	SnapOemDir                string
	SnapDataDir               string
	SnapDataHomeGlob          string
	SnapAppArmorDir           string
	SnapAppArmorAdditionalDir string
	SnapSeccompDir            string
	SnapUdevRulesDir          string
	LocaleDir                 string
	SnapIconsDir              string
	SnapMetaDir               string
	SnapLockFile              string

	SnapBinariesDir  string
	SnapServicesDir  string
	SnapBusPolicyDir string

	ClickSystemHooksDir string
	CloudMetaDataFile   string

	SnappyDir = filepath.Join("var", "lib", "snappy")
)

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	GlobalRootDir = rootdir

	SnapAppsDir = filepath.Join(rootdir, "/apps")
	SnapOemDir = filepath.Join(rootdir, "/oem")
	SnapDataDir = filepath.Join(rootdir, "/var/lib/apps")
	SnapDataHomeGlob = filepath.Join(rootdir, "/home/*/apps/")
	SnapAppArmorDir = filepath.Join(rootdir, SnappyDir, "apparmor", "profiles")
	SnapAppArmorAdditionalDir = filepath.Join(rootdir, SnappyDir, "apparmor", "additional")
	SnapSeccompDir = filepath.Join(rootdir, SnappyDir, "seccomp", "profiles")
	SnapIconsDir = filepath.Join(rootdir, SnappyDir, "icons")
	SnapMetaDir = filepath.Join(rootdir, SnappyDir, "meta")
	SnapLockFile = filepath.Join(rootdir, "/run/snappy.lock")
	SnapBlobDir = filepath.Join(rootdir, SnappyDir, "snaps")

	SnapBinariesDir = filepath.Join(SnapAppsDir, "bin")
	SnapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")
	SnapBusPolicyDir = filepath.Join(rootdir, "/etc/dbus-1/system.d")

	ClickSystemHooksDir = filepath.Join(rootdir, "/usr/share/click/hooks")

	CloudMetaDataFile = filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net/meta-data")

	SnapUdevRulesDir = filepath.Join(rootdir, "/etc/udev/rules.d")

	LocaleDir = filepath.Join(rootdir, "/usr/share/locale")
}
