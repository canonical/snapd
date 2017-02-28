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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Series holds the Ubuntu Core series for snapd to use.
var Series = "16"

// OS contains information about the system extracted from /etc/os-release.
type OS struct {
	ID        string `json:"id"`
	VersionID string `json:"version-id,omitempty"`
}

var (
	versionSignaturePath = "/proc/version_signature"
	apparmorSysPath      = "/sys/kernel/security/apparmor/"
)

// ForceDevMode returns true if the distribution doesn't implement required
// security features for confinement and devmode is forced.
func (o *OS) ForceDevMode() bool {
	// Check if kernel signature contains "Ubuntu", currently only
	// the Ubuntu kernels have all the required apparmor patches
	// (but those are getting upstreamed so at some point we need
	// to make this check smater)
	versionSig, err := ioutil.ReadFile(versionSignaturePath)
	if err != nil {
		return true
	}
	if !strings.HasPrefix(string(versionSig), "Ubuntu ") {
		return true
	}

	// Also ensure appamor is enabled (cannot use osutil.FileExists() here
	// because of cyclic imports)
	if _, err := os.Stat(apparmorSysPath); err != nil {
		return true
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
		VersionID: "unknown",
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
		case "VERSION_ID":
			osRelease.VersionID = v
		}
	}

	return osRelease
}

// OnClassic states whether the process is running inside a
// classic Ubuntu system or a native Ubuntu Core image.
var OnClassic bool

// ReleaseInfo contains data loaded from /etc/os-release on startup.
var ReleaseInfo OS

func init() {
	ReleaseInfo = readOSRelease()

	OnClassic = (ReleaseInfo.ID != "ubuntu-core")
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

// MockForcedDevmode fake the system to believe its in a distro
// that is in ForcedDevmode
func MockForcedDevmode(isDevmode bool) (restore func()) {
	oldVersionSignaturePath := versionSignaturePath
	oldApparmorSysPath := apparmorSysPath

	temp, err := ioutil.TempDir("", "mock-forced-devmode")
	if err != nil {
		panic(err)
	}
	fakeVersionSignaturePath := filepath.Join(temp, "version_signature")
	fakeApparmorSysPath := filepath.Join(temp, "apparmor")
	if !isDevmode {
		if err := ioutil.WriteFile(fakeVersionSignaturePath, []byte("Ubuntu 4.8.0-39.42-generic 4.8.17"), 0644); err != nil {
			panic(err)
		}
		if err := os.MkdirAll(fakeApparmorSysPath, 0755); err != nil {
			panic(err)
		}
	}
	versionSignaturePath = fakeVersionSignaturePath
	apparmorSysPath = fakeApparmorSysPath

	return func() {
		if err := os.RemoveAll(temp); err != nil {
			panic(err)
		}
		versionSignaturePath = oldVersionSignaturePath
		apparmorSysPath = oldApparmorSysPath
	}
}
