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
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// ValidateNoAppArmorRegexp will check that the given string does not
// contain AppArmor regular expressions (AARE), double quotes or \0.
func ValidateNoAppArmorRegexp(s string) error {
	const AARE = `?*[]{}^"` + "\x00"

	if strings.ContainsAny(s, AARE) {
		return fmt.Errorf("%q contains a reserved apparmor char from %s", s, AARE)
	}
	return nil
}

type aaParserFlags int

const (
	// skipReadCache causes apparmor_parser to be invoked with --skip-read-cache.
	// This allows us to essentially overwrite a cache that we know is stale regardless
	// of the time and date settings (apparmor_parser caching is based on mtime).
	// Note that writing of the cache relies on --write-cache but we pass that
	// command-line option unconditionally.
	skipReadCache aaParserFlags = 1 << iota

	// conserveCPU tells apparmor_parser to spare up to two CPUs on multi-core systems to
	// reduce load when processing many profiles at once.
	conserveCPU aaParserFlags = 1 << iota

	// skipKernelLoad tells apparmor_parser not to load profiles into the kernel. The use
	// case of this is when in pre-seeding mode.
	skipKernelLoad aaParserFlags = 1 << iota
)

var runtimeNumCPU = runtime.NumCPU

func maybeSetNumberOfJobs() string {
	cpus := runtimeNumCPU()
	// Do not use all CPUs as this may have negative impact when booting. Note, -j0 has special meaning
	// so we don't want to pass it to apparmor parser.
	if cpus > 1 {
		if cpus == 2 {
			// systems with only two CPUs, spare 1
			return fmt.Sprintf("-j%d", cpus-1)
		} else {
			// otherwise spare 2
			return fmt.Sprintf("-j%d", cpus-2)
		}
	}
	return ""
}

// loadProfiles loads apparmor profiles from the given files.
//
// If no such profiles were previously loaded then they are simply added to the kernel.
// If there were some profiles with the same name before, those profiles are replaced.
func loadProfiles(fnames []string, cacheDir string, flags aaParserFlags) error {
	if len(fnames) == 0 {
		return nil
	}

	// Use no-expr-simplify since expr-simplify is actually slower on armhf (LP: #1383858)
	args := []string{"--replace", "--write-cache", "-O", "no-expr-simplify", fmt.Sprintf("--cache-loc=%s", cacheDir)}
	if flags&conserveCPU != 0 {
		if jobArg := maybeSetNumberOfJobs(); jobArg != "" {
			args = append(args, jobArg)
		}
	}

	if flags&skipKernelLoad != 0 {
		args = append(args, "--skip-kernel-load")
	}

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

// unloadProfiles is meant to remove the named profiles from the running
// kernel and then remove any cache files. Importantly, we can only unload
// profiles when we are sure there are no lingering processes from the snap
// (ie, forcibly stop all running processes from the snap). Otherwise, any
// running processes will become unconfined. Since we don't have this guarantee
// yet, leave the profiles loaded in the kernel but remove the cache files from
// the system so the policy is gone on the next reboot. LP: #1818241
func unloadProfiles(names []string, cacheDir string) error {
	if len(names) == 0 {
		return nil
	}

	/* TODO: uncomment when no lingering snap processes is guaranteed
	// By the time this function is called, all the profiles (names) have
	// been removed from dirs.SnapAppArmorDir, so to unload the profiles
	// from the running kernel we must instead use sysfs and write the
	// profile names one at a time to
	// /sys/kernel/security/apparmor/.remove (with no trailing \n).
	apparmorSysFsRemove := "/sys/kernel/security/apparmor/.remove"
	if !osutil.IsWritable(appArmorSysFsRemove) {
	        return fmt.Errorf("cannot unload apparmor profile: %s does not exist\n", appArmorSysFsRemove)
	}
	for _, n := range names {
	        // ignore errors since it is ok if the profile isn't removed
	        // from the kernel
	        ioutil.WriteFile(appArmorSysFsRemove, []byte(n), 0666)
	}
	*/

	// AppArmor 2.13 and higher has a cache forest while 2.12 and lower has
	// a flat directory (on 2.12 and earlier, .features and the snap
	// profiles are in the top-level directory instead of a subdirectory).
	// With 2.13+, snap profiles are not expected to be in every
	// subdirectory, so don't error on ENOENT but otherwise if we get an
	// error, something weird happened so stop processing.
	if li, err := filepath.Glob(filepath.Join(cacheDir, "*/.features")); err == nil && len(li) > 0 { // 2.13+
		for _, p := range li {
			dir := path.Dir(p)
			if err := osutil.UnlinkMany(dir, names); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("cannot remove apparmor profile cache in %s: %s", dir, err)
			}
		}
	} else if err := osutil.UnlinkMany(cacheDir, names); err != nil && !os.IsNotExist(err) { // 2.12-
		return fmt.Errorf("cannot remove apparmor profile cache: %s", err)
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
