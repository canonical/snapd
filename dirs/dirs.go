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

	SnapSnapsDir              string
	SnapBlobDir               string
	SnapDataDir               string
	SnapDataHomeGlob          string
	SnapAppArmorDir           string
	AppArmorCacheDir          string
	SnapAppArmorAdditionalDir string
	SnapSeccompDir            string
	SnapUdevRulesDir          string
	LocaleDir                 string
	SnapMetaDir               string
	SnapLockFile              string
	SnapdSocket               string

	SnapAssertsDBDir      string
	SnapTrustedAccountKey string

	SnapStateFile string

	SnapBinariesDir     string
	SnapServicesDir     string
	SnapDesktopFilesDir string
	SnapBusPolicyDir    string

	CloudMetaDataFile string

	ClassicDir string
)

var (
	// not exported because it does not honor the global rootdir
	snappyDir = filepath.Join("var", "lib", "snapd")
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

	SnapSnapsDir = filepath.Join(rootdir, "/snap")
	SnapDataDir = filepath.Join(rootdir, "/var/snap")
	SnapDataHomeGlob = filepath.Join(rootdir, "/home/*/snap/")
	SnapAppArmorDir = filepath.Join(rootdir, snappyDir, "apparmor", "profiles")
	AppArmorCacheDir = filepath.Join(rootdir, "/var/cache/apparmor")
	SnapAppArmorAdditionalDir = filepath.Join(rootdir, snappyDir, "apparmor", "additional")
	SnapSeccompDir = filepath.Join(rootdir, snappyDir, "seccomp", "profiles")
	SnapMetaDir = filepath.Join(rootdir, snappyDir, "meta")
	SnapLockFile = filepath.Join(rootdir, "/run/snapd-legacy.lock")
	SnapBlobDir = filepath.Join(rootdir, snappyDir, "snaps")
	SnapDesktopFilesDir = filepath.Join(rootdir, snappyDir, "desktop", "applications")
	// keep in sync with the debian/ubuntu-snappy.snapd.socket file:
	SnapdSocket = filepath.Join(rootdir, "/run/snapd.socket")

	SnapAssertsDBDir = filepath.Join(rootdir, snappyDir, "assertions")
	SnapTrustedAccountKey = filepath.Join(rootdir, "/usr/share/snapd/trusted.acckey")

	SnapStateFile = filepath.Join(rootdir, snappyDir, "state.json")

	SnapBinariesDir = filepath.Join(SnapSnapsDir, "bin")
	SnapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")
	SnapBusPolicyDir = filepath.Join(rootdir, "/etc/dbus-1/system.d")

	CloudMetaDataFile = filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net/meta-data")

	SnapUdevRulesDir = filepath.Join(rootdir, "/etc/udev/rules.d")

	LocaleDir = filepath.Join(rootdir, "/usr/share/locale")
	ClassicDir = filepath.Join(rootdir, "/writable/classic")
}
