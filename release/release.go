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

package release

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/snapcore/snapd/strutil"
)

// Series holds the Ubuntu Core series for snapd to use.
var Series = "16"

// OS contains information about the system extracted from /etc/os-release.
type OS struct {
	ID        string   `json:"id"`
	IDLike    []string `json:"-"`
	VariantID string   `json:"variant-id,omitempty"`
	VersionID string   `json:"version-id,omitempty"`
}

// DistroLike checks if the distribution ID or ID_LIKE matches one of the given names.
func DistroLike(distros ...string) bool {
	for _, distro := range distros {
		if ReleaseInfo.ID == distro || strutil.ListContains(ReleaseInfo.IDLike, distro) {
			return true
		}
	}
	return false
}

var (
	osReleasePath         = "/etc/os-release"
	fallbackOsReleasePath = "/usr/lib/os-release"
)

// readOSRelease returns the os-release information of the current system.
func readOSRelease() OS {
	// TODO: separate this out into its own thing maybe (if made more general)
	osRelease := OS{
		// from os-release(5): If not set, defaults to "ID=linux".
		ID: "linux",
	}

	f, err := os.Open(osReleasePath)
	if err != nil {
		// this fallback is as per os-release(5)
		f, err = os.Open(fallbackOsReleasePath)
		if err != nil {
			return osRelease
		}
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ws := strings.SplitN(scanner.Text(), "=", 2)
		if len(ws) < 2 {
			continue
		}

		k := strings.TrimSpace(ws[0])
		v := strings.TrimFunc(ws[1], func(r rune) bool { return r == '"' || r == '\'' || unicode.IsSpace(r) })
		// XXX: should also unquote things as per os-release(5) but not needed yet in practice
		switch k {
		case "ID":
			// ID should be “A lower-case string (no spaces or
			// other characters outside of 0–9, a–z, ".", "_" and
			// "-") identifying the operating system, excluding any
			// version information and suitable for processing by
			// scripts or usage in generated filenames.”
			//
			// So we mangle it a little bit to account for people
			// not being too good at reading comprehension.
			// Works around e.g. lp:1602317
			osRelease.ID = strings.Fields(strings.ToLower(v))[0]
		case "ID_LIKE":
			// This is like ID, except it's a space separated list... hooray?
			osRelease.IDLike = strings.Fields(strings.ToLower(v))
		case "VARIANT_ID":
			osRelease.VariantID = v
		case "VERSION_ID":
			osRelease.VersionID = v
		}
	}

	return osRelease
}

// Note that osutil.FileExists cannot be used here as an osutil import will create a cyclic import
var fileExists = func(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var procMountsPath = "/proc/mounts"

// filesystemRootType returns the filesystem type mounted at "/".
func filesystemRootType() (string, error) {
	// We scan /proc/mounts, which contains space-separated values:
	// [irrelevant] [mount point] [fstype] [irrelevant...]
	// Here are some examples on some platforms:
	// WSL1       :  rootfs / wslfs rw,noatime 0 0
	// WSL2       :  /dev/sdc / ext4 rw,relatime,discard,errors=remount-ro,data=ordered 0 0
	// lxc on WSL2:  /dev/loop0 / btrfs rw,relatime,idmapped,space_cache,user_subvol_rm_allowed,subvolid=259,subvol=/containers/testlxd 0 0
	// We search for mount point = "/", and return the fstype.
	//
	// This should be done by osutil.LoadMountInfo but that would cause a dependency cycle
	file, err := os.Open(procMountsPath)
	if err != nil {
		return "", fmt.Errorf("cannot find root filesystem type: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		data := strings.Fields(scanner.Text())
		if len(data) < 3 || data[1] != "/" {
			continue
		}
		return data[2], nil
	}

	if err = scanner.Err(); err != nil {
		return "", fmt.Errorf("cannot find root filesystem type: %v", err)
	}

	return "", fmt.Errorf("cannot find root filesystem type: not in list")
}

// We detect WSL via the existence of /proc/sys/fs/binfmt_misc/WSLInterop
// Under some undocumented circumstances this file may be missing. We have /run/WSL as a backup.
//
// We detect WSL1 via the root filesystem type:
// - wslfs or lxfs mean WSL1
// - Anything else means WSL2
// After knowing we're in WSL, if any error occurs we assume WSL2 as it is the more flexible version
func getWSLVersion() int {
	if !fileExists("/proc/sys/fs/binfmt_misc/WSLInterop") && !fileExists("/run/WSL") {
		return 0
	}
	fstype, err := filesystemRootType()
	if err != nil {
		// TODO log error here once logger can be imported without circular imports
		return 2
	}

	if fstype == "wslfs" || fstype == "lxfs" {
		return 1
	}
	return 2
}

// SystemctlSupportsUserUnits returns true if the systemctl utility
// supports user units.
func SystemctlSupportsUserUnits() bool {
	// Ubuntu 14.04's systemctl does not support the arguments
	// needed to enable user session units. Further more, it does
	// not ship with a systemd user instance.
	return !(ReleaseInfo.ID == "ubuntu" && ReleaseInfo.VersionID == "14.04")
}

// OnClassic states whether the process is running inside a
// classic Ubuntu system or a native Ubuntu Core image.
var OnClassic bool

// OnCoreDesktop states whether the process is running inside a Core Desktop image.
var OnCoreDesktop bool

// OnWSL states whether the process is running inside the Windows
// Subsystem for Linux
var OnWSL bool

// If the previous is true, WSLVersion states whether the process is running inside WSL1 or WSL2
// Otherwise it is set to 0
var WSLVersion int

// ReleaseInfo contains data loaded from /etc/os-release on startup.
var ReleaseInfo OS

func init() {
	ReleaseInfo = readOSRelease()

	OnClassic = (ReleaseInfo.ID != "ubuntu-core")

	OnCoreDesktop = (ReleaseInfo.ID == "ubuntu-core" && ReleaseInfo.VariantID == "desktop")

	WSLVersion = getWSLVersion()
	OnWSL = WSLVersion != 0
}

// MockOnClassic forces the process to appear inside a classic
// Ubuntu system or a native image for testing purposes.
func MockOnClassic(onClassic bool) (restore func()) {
	old := OnClassic
	OnClassic = onClassic
	return func() { OnClassic = old }
}

// MockOnCoreDesktop forces the process to appear inside a core desktop
// system for testing purposes.
func MockOnCoreDesktop(onCoreDesktop bool) (restore func()) {
	old := OnCoreDesktop
	OnCoreDesktop = onCoreDesktop
	return func() { OnCoreDesktop = old }
}

// MockReleaseInfo fakes a given information to appear in ReleaseInfo,
// as if it was read /etc/os-release on startup.
func MockReleaseInfo(osRelease *OS) (restore func()) {
	old := ReleaseInfo
	ReleaseInfo = *osRelease
	return func() { ReleaseInfo = old }
}
