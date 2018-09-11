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
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// UnloadProfiles removes the named profiles from the running kernel.
//
// The operation is done with: apparmor_parser --remove $names...
// The binary cache file is removed from /var/cache/apparmor
func UnloadProfiles(names []string) error {
	return unloadProfiles(names, dirs.AppArmorCacheDir)
}

type aaParserFlags int

const (
	// skipReadCache causes apparmor_parser to be invoked with --skip-read-cache.
	// This allows us to essentially overwrite a cache that we know is stale regardless
	// of the time and date settings (apparmor_parser caching is based on mtime).
	// Note that writing of the cache relies on --write-cache but we pass that
	// command-line option unconditionally.
	skipReadCache aaParserFlags = 1 << iota
)

// loadProfiles loads apparmor profiles from the given files.
//
// If no such profiles were previously loaded then they are simply added to the kernel.
// If there were some profiles with the same name before, those profiles are replaced.
func loadProfiles(fnames []string, cacheDir string, flags aaParserFlags) error {
	if len(fnames) == 0 {
		return nil
	}

	// Use no-expr-simplify since expr-simplify is actually slower on armhf (LP: #1383858)
	args := []string{"--replace", "--write-cache", "-O", "no-expr-simplify",
		fmt.Sprintf("--cache-loc=%s", cacheDir)}
	if flags&skipReadCache != 0 {
		args = append(args, "--skip-read-cache")
	}
	if !osutil.GetenvBool("SNAPD_DEBUG") {
		args = append(args, "--quiet")
	}
	args = append(args, fnames...)

	output, err := exec.Command("apparmor_parser", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot load apparmor profiles: %s\napparmor_parser output:\n%s", err, string(output))
	}
	return nil
}

func unloadProfiles(names []string, cacheDir string) error {
	if len(names) == 0 {
		return nil
	}

	args := []string{"--remove"}
	args = append(args, names...)
	output, err := exec.Command("apparmor_parser", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot unload apparmor profile: %s\napparmor_parser output:\n%s", err, string(output))
	}
	for _, name := range names {
		err = os.Remove(filepath.Join(cacheDir, name))
		// It is not an error if the cache file wasn't there to remove.
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove apparmor profile cache: %s", err)
		}
	}
	return nil
}

// profilesPath contains information about the currently loaded apparmor profiles.
const realProfilesPath = "/sys/kernel/security/apparmor/profiles"

var profilesPath = realProfilesPath

// LoadedProfiles interrogates the kernel and returns a list of loaded apparmor profiles.
//
// Snappy manages apparmor profiles named "snap.*". Other profiles might exist on
// the system (via snappy dimension) and those are filtered-out.
func LoadedProfiles() ([]string, error) {
	file, err := os.Open(profilesPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var profiles []string
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
		if strings.HasPrefix(name, "snap.") {
			profiles = append(profiles, name)
		}
	}
	return profiles, nil
}
