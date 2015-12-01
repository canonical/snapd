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

import (
	"os"
	"path/filepath"
)

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
	SnapdSocket               string

	SnapAssertsDBDir      string
	SnapTrustedAccountKey string

	SnapBinariesDir  string
	SnapServicesDir  string
	SnapBusPolicyDir string

	ClickSystemHooksDir string
	CloudMetaDataFile   string

	ClassicDir string
)

var (
	// not exported because it does not honor the global rootdir
	snappyDir = filepath.Join("var", "lib", "snappy")
)

func init() {
	// init the global directories at startup
	root := os.Getenv("SNAPPY_GLOBAL_ROOT")
	if root == "" {
		root = "/"
	}

	SetRootDir(root)
}

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	GlobalRootDir = rootdir

	SnapAppsDir = filepath.Join(rootdir, "/apps")
	SnapOemDir = filepath.Join(rootdir, "/oem")
	SnapDataDir = filepath.Join(rootdir, "/var/lib/apps")
	SnapDataHomeGlob = filepath.Join(rootdir, "/home/*/apps/")
	SnapAppArmorDir = filepath.Join(rootdir, snappyDir, "apparmor", "profiles")
	SnapAppArmorAdditionalDir = filepath.Join(rootdir, snappyDir, "apparmor", "additional")
	SnapSeccompDir = filepath.Join(rootdir, snappyDir, "seccomp", "profiles")
	SnapIconsDir = filepath.Join(rootdir, snappyDir, "icons")
	SnapMetaDir = filepath.Join(rootdir, snappyDir, "meta")
	SnapLockFile = filepath.Join(rootdir, "/run/snappy.lock")
	SnapBlobDir = filepath.Join(rootdir, snappyDir, "snaps")
	// keep in sync with the debian/ubuntu-snappy.snapd.socket file:
	SnapdSocket = filepath.Join(rootdir, "/run/snapd.socket")

	SnapAssertsDBDir = filepath.Join(rootdir, snappyDir, "assertions")
	SnapTrustedAccountKey = filepath.Join(rootdir, "/usr/share/snappy/trusted.acckey")

	SnapBinariesDir = filepath.Join(SnapAppsDir, "bin")
	SnapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")
	SnapBusPolicyDir = filepath.Join(rootdir, "/etc/dbus-1/system.d")

	ClickSystemHooksDir = filepath.Join(rootdir, "/usr/share/click/hooks")

	CloudMetaDataFile = filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net/meta-data")

	SnapUdevRulesDir = filepath.Join(rootdir, "/etc/udev/rules.d")

	LocaleDir = filepath.Join(rootdir, "/usr/share/locale")
	ClassicDir = filepath.Join(rootdir, "/writable/classic")
}
