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

	"github.com/snapcore/snapd/osutil"
	"unicode"
)

// Series holds the Ubuntu Core series for snapd to use.
var Series = "16"

// OS contains information about the system extracted from /etc/os-release.
type OS struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	VersionID  string `json:"version-id,omitempty"`
	Codename   string `json:"codename,omitempty"`
	PrettyName string `json:"pretty-name,omitempty"`
}

// ForceDevMode returns true if the distribution doesn't implement required
// security features for confinement and devmode is forced.
func (os *OS) ForceDevMode() bool {
	switch os.ID {
	case "neon":
		fallthrough
	case "ubuntu":
		return false

	case "elementary":
		switch os.VersionID {
		case "0.4":
			return false
		default:
			return true
		}
	default:
		// NOTE: Other distributions can move out of devmode by
		// integrating with the interface security backends. This will
		// be documented separately in the porting guide.
		return true
	}

}

var osReleasePath = "/etc/os-release"

// readOSRelease returns the os-release information of the current system.
func readOSRelease() (*OS, error) {
	osRelease := &OS{}

	f, err := os.Open(osReleasePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open os-release: %s", err)
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ws := strings.SplitN(scanner.Text(), "=", 2)
		if len(ws) < 2 {
			continue
		}

		k := strings.TrimSpace(ws[0])
		v := strings.TrimFunc(ws[1], func(r rune) bool { return r == '"' || unicode.IsSpace(r) })
		switch k {
		case "ID":
			osRelease.ID = v
		case "NAME":
			osRelease.Name = v
		case "VERSION_ID":
			osRelease.VersionID = v
		case "UBUNTU_CODENAME":
			osRelease.Codename = v
		case "PRETTY_NAME":
			osRelease.PrettyName = v
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("cannot read os-release: %s", err)
	}

	return osRelease, nil
}

// OnClassic states whether the process is running inside a
// classic Ubuntu system or a native Ubuntu Core image.
var OnClassic bool

// ReleaseInfo contains data loaded from /etc/os-release on startup.
var ReleaseInfo OS

func init() {
	osRelease, err := readOSRelease()
	if err != nil {
		// Values recommended by os-release(5) as defaults
		osRelease = &OS{
			Name: "Linux",
			ID:   "linux",
		}
	}
	ReleaseInfo = *osRelease
	// Assume that we are running on Classic
	OnClassic = true
	// On Ubuntu, dpkg is not present in an all-snap image so the presence of
	// dpkg status file can be used as an indicator for a classic vs all-snap
	// system.
	if osRelease.ID == "ubuntu" {
		OnClassic = osutil.FileExists("/var/lib/dpkg/status")
	}
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
