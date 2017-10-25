// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
)

// XSnapdMode returns the file mode associated with x-snapd.mode mount option.
// If the mode is not specified explicitly then a default mode of 0755 is assumed.
func XSnapdMode(e *mount.Entry) (os.FileMode, error) {
	for _, opt := range e.Options {
		if strings.HasPrefix(opt, "x-snapd.mode=") {
			kv := strings.SplitN(opt, "=", 2)
			var mode os.FileMode
			n, err := fmt.Sscanf(kv[1], "%o", &mode)
			if err != nil || n != 1 {
				return 0, fmt.Errorf("cannot parse octal file mode from %q", kv[1])
			}
			return mode, nil
		}
	}
	return 0755, nil
}

// XSnapdUid returns the user associated with x-snapd-user mount option.  If
// the mode is not specified explicitly then a default "root" use is
// returned.
func XSnapdUid(e *mount.Entry) (uid uint64, err error) {
	for _, opt := range e.Options {
		if strings.HasPrefix(opt, "x-snapd.uid=") {
			kv := strings.SplitN(opt, "=", 2)
			// Try to parse a numeric ID first.
			if n, err := fmt.Sscanf(kv[1], "%d", &uid); n == 1 && err == nil {
				return uid, nil
			}
			// Fall-back to system name lookup.
			if uid, err = osutil.FindUid(kv[1]); err != nil {
				// The error message from FindUid is not very useful so just skip it.
				return math.MaxUint64, fmt.Errorf("cannot resolve user name %q", kv[1])
			}
			return uid, nil
		}
	}
	return 0, nil
}

// XSnapdGid returns the user associated with x-snapd-user mount option.  If
// the mode is not specified explicitly then a default "root" use is
// returned.
func XSnapdGid(e *mount.Entry) (gid uint64, err error) {
	for _, opt := range e.Options {
		if strings.HasPrefix(opt, "x-snapd.gid=") {
			kv := strings.SplitN(opt, "=", 2)
			// Try to parse a numeric ID first.
			if n, err := fmt.Sscanf(kv[1], "%d", &gid); n == 1 && err == nil {
				return gid, nil
			}
			// Fall-back to system name lookup.
			if gid, err = osutil.FindGid(kv[1]); err != nil {
				// The error message from FindGid is not very useful so just skip it.
				return math.MaxUint64, fmt.Errorf("cannot resolve group name %q", kv[1])
			}
			return gid, nil
		}
	}
	return 0, nil
}
