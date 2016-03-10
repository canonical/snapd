// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

// Package apparmor contains primitives for working with apparmor.
//
// References:
//  - http://wiki.apparmor.net/index.php/Kernel_interfaces
//  - http://apparmor.wiki.kernel.org/
//  - http://manpages.ubuntu.com/manpages/xenial/en/man7/apparmor.7.html
package apparmor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// LoadProfile loads an apparmor profile from the given file.
//
// If no such profile was previously loaded then it is simply added to the kernel.
// If there was a profile with the same name before, that profile is replaced.
func LoadProfile(fname string) error {
	output, err := exec.Command("apparmor_parser", "--replace", fname).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot load apparmor profile: %s\napparmor_parser output:\n%s", err, string(output))
	}
	return nil
}

// Profile contains the name and mode of an apparmor profile loaded into the kernel.
type Profile struct {
	// Name of the profile. This is is either full path of the executable or an
	// arbitrary string without spaces.
	Name string
	// Mode is either "enforce" or "complain".
	Mode string
}

// Unload removes a profile from the running kernel.
//
// The operation is done with: apparmor_parser --remove $name
func (profile *Profile) Unload() error {
	output, err := exec.Command("apparmor_parser", "--remove", profile.Name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot unload apparmor profile: %s\napparmor_parser output:\n%s", err, string(output))
	}
	return nil
}

// profilesPath contains information about the currently loaded apparmor profiles.
const realProfilesPath = "/sys/kernel/security/apparmor/profiles"

var profilesPath = realProfilesPath

// LoadedProfiles interrogates the kernel and returns a list of loaded apparmor profiles.
//
// Snappy manages apparmor profiles named *.snap. Other profiles might exist on
// the system (via snappy dimension) and those are filtered-out.
func LoadedProfiles() ([]Profile, error) {
	file, err := os.Open(profilesPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var profiles []Profile
	for {
		var name, mode string
		n, err := fmt.Fscanf(file, "%s %s\n", &name, &mode)
		if n > 0 && n != 2 {
			return nil, fmt.Errorf("syntax error, expected: name (mode)")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		mode = strings.Trim(mode, "()")
		if strings.HasSuffix(name, ".snap") {
			profiles = append(profiles, Profile{Name: name, Mode: mode})
		}
	}
	return profiles, nil
}
