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
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// the various file paths
var (
	GlobalRootDir string

	SnapMountDir              string
	SnapBlobDir               string
	SnapDataDir               string
	SnapDataHomeGlob          string
	SnapAppArmorDir           string
	AppArmorCacheDir          string
	SnapAppArmorAdditionalDir string
	SnapSeccompDir            string
	SnapMountPolicyDir        string
	SnapUdevRulesDir          string
	SnapKModModulesDir        string
	LocaleDir                 string
	SnapMetaDir               string
	SnapdSocket               string
	SnapSocket                string
	SnapRunNsDir              string

	SnapSeedDir   string
	SnapDeviceDir string

	SnapAssertsDBDir      string
	SnapTrustedAccountKey string
	SnapAssertsSpoolDir   string

	SnapStateFile string

	SnapBinariesDir     string
	SnapServicesDir     string
	SnapDesktopFilesDir string
	SnapBusPolicyDir    string

	CloudMetaDataFile string

	ClassicDir string

	LibExecDir string

	XdgRuntimeDirGlob string
)

var (
	// not exported because it does not honor the global rootdir
	snappyDir = filepath.Join("var", "lib", "snapd")
)

func init() {
	// init the global directories at startup
	root := os.Getenv("SNAPPY_GLOBAL_ROOT")

	SetRootDir(root)
}

// StripRootDir strips the custom global root directory from the specified argument.
func StripRootDir(dir string) string {
	if !filepath.IsAbs(dir) {
		panic(fmt.Sprintf("supplied path is not absolute %q", dir))
	}
	if !strings.HasPrefix(dir, GlobalRootDir) {
		panic(fmt.Sprintf("supplied path is not related to global root %q", dir))
	}
	result, err := filepath.Rel(GlobalRootDir, dir)
	if err != nil {
		panic(err)
	}
	return "/" + result
}

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	if rootdir == "" {
		rootdir = "/"
	}
	GlobalRootDir = rootdir

	SnapMountDir = filepath.Join(rootdir, "/snap")
	SnapDataDir = filepath.Join(rootdir, "/var/snap")
	SnapDataHomeGlob = filepath.Join(rootdir, "/home/*/snap/")
	SnapAppArmorDir = filepath.Join(rootdir, snappyDir, "apparmor", "profiles")
	AppArmorCacheDir = filepath.Join(rootdir, "/var/cache/apparmor")
	SnapAppArmorAdditionalDir = filepath.Join(rootdir, snappyDir, "apparmor", "additional")
	SnapSeccompDir = filepath.Join(rootdir, snappyDir, "seccomp", "profiles")
	SnapMountPolicyDir = filepath.Join(rootdir, snappyDir, "mount")
	SnapMetaDir = filepath.Join(rootdir, snappyDir, "meta")
	SnapBlobDir = filepath.Join(rootdir, snappyDir, "snaps")
	SnapDesktopFilesDir = filepath.Join(rootdir, snappyDir, "desktop", "applications")
	SnapRunNsDir = filepath.Join(rootdir, "/run/snapd/ns")

	// keep in sync with the debian/snapd.socket file:
	SnapdSocket = filepath.Join(rootdir, "/run/snapd.socket")
	SnapSocket = filepath.Join(rootdir, "/run/snapd-snap.socket")

	SnapAssertsDBDir = filepath.Join(rootdir, snappyDir, "assertions")
	SnapAssertsSpoolDir = filepath.Join(rootdir, "run/snapd/auto-import")

	SnapStateFile = filepath.Join(rootdir, snappyDir, "state.json")

	SnapSeedDir = filepath.Join(rootdir, snappyDir, "seed")
	SnapDeviceDir = filepath.Join(rootdir, snappyDir, "device")

	SnapBinariesDir = filepath.Join(SnapMountDir, "bin")
	SnapServicesDir = filepath.Join(rootdir, "/etc/systemd/system")
	SnapBusPolicyDir = filepath.Join(rootdir, "/etc/dbus-1/system.d")

	CloudMetaDataFile = filepath.Join(rootdir, "/var/lib/cloud/seed/nocloud-net/meta-data")

	SnapUdevRulesDir = filepath.Join(rootdir, "/etc/udev/rules.d")

	SnapKModModulesDir = filepath.Join(rootdir, "/etc/modules-load.d/")

	LocaleDir = filepath.Join(rootdir, "/usr/share/locale")
	ClassicDir = filepath.Join(rootdir, "/writable/classic")

	LibExecDir = filepath.Join(rootdir, "/usr/lib/snapd")

	XdgRuntimeDirGlob = filepath.Join(rootdir, "/run/user/*/")
}
