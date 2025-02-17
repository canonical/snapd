// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package notify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

// ProtocolVersion denotes a notification protocol version.
type ProtocolVersion uint16

var (
	// protocolVersionsDir, if it exists, has a boolean file entry for each
	// protocol version supported by the kernel.
	protocolVersionsDir string

	// protocolFeaturesPath contains fields for supported protocol features.
	protocolFeaturesPath string

	// versions holds the notification protocols snapd supports, in order of
	// preference. If the first version is supported, try to use it, else try
	// the next version, etc.
	versions = []ProtocolVersion{5, 3}

	// versionLikelySupportedChecks provides a function for each known protocol
	// version which returns true if that version is supported by snapd and
	// likely supported by the kernel. Kernel support may be guaged by checking
	// kernel features or probing the filesystem for hints from the kernel
	// about which versions it supports. Even if the check function returns
	// true, the kernel may return EPROTONOSUPPORT when attempting to register
	// on the notify socket with that version, in which case we'll need to try
	// the next version in the list.
	versionLikelySupportedChecks = map[ProtocolVersion]func() bool{
		3: func() bool {
			if !SupportAvailable() {
				return false
			}
			if osutil.IsDirectory(protocolVersionsDir) && !notifyVersionFileExists(3) {
				return false
			}
			return true
		},
		5: func() bool {
			if !SupportAvailable() {
				return false
			}
			if !notifyVersionFileExists(5) {
				return false
			}
			// XXX: apparmor.KernelFeatures() already has this information, but
			// we can't import apparmor here since that would be circular.
			data, err := os.ReadFile(protocolFeaturesPath)
			if err != nil {
				return false
			}
			features := strings.Fields(string(data))
			if !strutil.ListContains(features, "tags") {
				return false
			}
			return true
		},
	}

	// versionKnown returns true if the given protocol version is known by
	// snapd. Even if true, the version may still be unsupported by snapd or
	// the kernel.
	versionKnown = func(v ProtocolVersion) bool {
		_, exists := versionLikelySupportedChecks[v]
		return exists
	}
)

func notifyVersionFileExists(version ProtocolVersion) bool {
	return osutil.FileExists(filepath.Join(protocolVersionsDir, fmt.Sprintf("v%d", version)))
}

// likelySupported returns true if the receiving version is supported by snapd
// and likely supported by the kernel, as reported by the likely supported
// check for that version.
func (v ProtocolVersion) likelySupported() (bool, error) {
	checkFn, ok := versionLikelySupportedChecks[v]
	if !ok {
		// Should not occur, since the caller should only call this method on
		// known versions, and tests should validate that each known version
		// has a support check function.
		return false, fmt.Errorf("internal error: no support check function defined for version %d", v)
	}
	return checkFn(), nil
}

// likelySupportedProtocolVersion returns the preferred protocol version which
// is expected to be supported by both snapd and the kernel. Any versions
// included in the given unsupported map are not tried.
//
// Any versions which are found to be unsupported are added to the given
// unsupported map so that, in case the returned version reports as being
// unsupported by the kernel, subsequent calls to this function will not
// require duplicate checks of support check functions.
func likelySupportedProtocolVersion(unsupported map[ProtocolVersion]bool) (ProtocolVersion, bool) {
	for _, v := range versions {
		if _, exists := unsupported[v]; exists {
			continue
		}
		if supported, _ := v.likelySupported(); !supported {
			unsupported[v] = true
			continue
		}
		return v, true
	}
	return ProtocolVersion(0), false
}

func setupProtocolVersionsPaths(newrootdir string) {
	protocolVersionsDir = filepath.Join(newrootdir, "/sys/kernel/security/apparmor/features/policy/notify_versions")
	protocolFeaturesPath = filepath.Join(newrootdir, "/sys/kernel/security/apparmor/features/policy/notify/user")
}

func init() {
	dirs.AddRootDirCallback(setupProtocolVersionsPaths)
	setupProtocolVersionsPaths(dirs.GlobalRootDir)
}

func MockVersionKnown(f func(v ProtocolVersion) bool) (restore func()) {
	orig := versionKnown
	versionKnown = f
	restore = func() {
		versionKnown = orig
	}
	return restore
}
