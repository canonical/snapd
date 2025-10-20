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
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/sandbox/apparmor"
)

// ProtocolVersion denotes a notification protocol version.
type ProtocolVersion uint16

var (
	// versions holds the notification protocols snapd supports, in order of
	// preference. If the first version is supported, try to use it, else try
	// the next version, etc.
	versions = []ProtocolVersion{5, 3}

	// apparmorMetadataTagsSupported allows tests to mock tags support.
	apparmorMetadataTagsSupported = apparmor.MetadataTagsSupported

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
			// Older kernels which only support v3 don't have notify_versions
			// dir at all. If the dir does exist, protocol support for version
			// 3 requires a v3 file to be present.
			if versionsDirExists() {
				if !versionFileExists("v3") {
					return false
				}
			}
			return true
		},
		5: func() bool {
			if !apparmorMetadataTagsSupported() {
				// Don't use v5 if tags are not supported
				return false
			}
			// Support for protocol version 5 requires that the notify_versions
			// directory must exist and contain a file named v5.
			if !versionFileExists("v5") {
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

func notifyVersionsDir() string {
	return filepath.Join(apparmor.FeaturesSysDir(), "policy", "notify_versions")
}

func versionsDirExists() bool {
	return osutil.IsDirectory(notifyVersionsDir())
}

func versionFileExists(versionFilename string) bool {
	return osutil.FileExists(filepath.Join(notifyVersionsDir(), versionFilename))
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

func MockVersionKnown(f func(v ProtocolVersion) bool) (restore func()) {
	orig := versionKnown
	versionKnown = f
	restore = func() {
		versionKnown = orig
	}
	return restore
}
