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

	"github.com/snapcore/snapd/testutil"
)

// ProtocolVersion denotes a notification protocol version.
type ProtocolVersion uint16

// versions holds the notification protocols snapd supports, in order of
// preference. If the first version is supported, try to use it, else try the
// next version, etc.
var (
	versions = []ProtocolVersion{3}

	versionSupportedCallbacks = map[ProtocolVersion]func() bool{
		3: SupportAvailable,
	}

	versionKnown = func(v ProtocolVersion) bool {
		_, exists := versionSupportedCallbacks[v]
		return exists
	}
)

func (v ProtocolVersion) supported() (bool, error) {
	callback, ok := versionSupportedCallbacks[v]
	if !ok {
		// Should not occur, since the caller should only call this method on
		// known versions, and tests should validate that each known version
		// has a callback function.
		return false, fmt.Errorf("internal error: no callback defined for version %d", v)
	}
	return callback(), nil
}

// supportedProtocolVersion returns the preferred protocol version which is
// expected to be supported by both snapd and the kernel. Any versions included
// in unsupported are not tried.
//
// Any versions which are found to be unsupported are added to the given
// unsupported map so that, in case the returned version reports as being
// unsupported by the kernel, subsequent calls to this function will not
// require duplicate checks of callback functions.
func supportedProtocolVersion(unsupported map[ProtocolVersion]bool) (ProtocolVersion, bool) {
	for _, v := range versions {
		if _, exists := unsupported[v]; exists {
			continue
		}
		if supported, _ := v.supported(); !supported {
			unsupported[v] = true
			continue
		}
		return v, true
	}
	return ProtocolVersion(0), false
}

func MockVersionKnown(f func(v ProtocolVersion) bool) (restore func()) {
	return testutil.Mock(&versionKnown, f)
}
