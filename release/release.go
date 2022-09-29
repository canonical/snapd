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
	"io/ioutil"
	"os"
	"regexp"
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

func isWSL() bool {
	return fileExists("/proc/sys/fs/binfmt_misc/WSLInterop")
}

var ioutilReadFile = ioutil.ReadFile

// WSL 1 /proc/version: Linux version [kernel]-Microsoft (Microsoft@Microsoft.com) ([gcc version] ) #[build]-Microsoft [timestamp]
// WS2 2 /proc/version: Linux version [kernel]-microsoft-standard-WSL2 (oe-user@oe-host) ([gcc version], [ld version]) #1 [timestamp]
//
// Note that WS2's kernel can be customized by the user, so there is no guarantee on the
// contents of that string. Hence why we search in WSL1's kernel name instead of WSL2's
var kernelWSL1 = regexp.MustCompile(`^Linux version [0-9.-]+Microsoft \(Microsoft@Microsoft.com\)`)

func getWSLVersion() (int, error) {
	if !isWSL() {
		return 0, fmt.Errorf("cannot get WSL version while not running WSL.")
	}
	kernel, err := ioutilReadFile("/proc/version")
	if err != nil || kernelWSL1.MatchString(string(kernel[:])) {
		// In case of error, we default to WSL1 because it is the most feature-restricted version
		return 1, nil
	}
	return 2, nil
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

	OnWSL = isWSL()
	WSLVersion, _ = getWSLVersion()
}

// MockOnClassic forces the process to appear inside a classic
// Ubuntu system or a native image for testing purposes.
func MockOnClassic(onClassic bool) (restore func()) {
	old := OnClassic
	OnClassic = onClassic
	return func() { OnClassic = old }
}

// MockReleaseInfo fakes a given information to appear in ReleaseInfo,
// as if it was read /etc/os-release on startup.
func MockReleaseInfo(osRelease *OS) (restore func()) {
	old := ReleaseInfo
	ReleaseInfo = *osRelease
	return func() { ReleaseInfo = old }
}
