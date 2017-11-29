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
	"regexp"

	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
)

var (
	validModeRe      = regexp.MustCompile("^0[0-7]{3}$")
	validUserGroupRe = regexp.MustCompile("(^[0-9]+$)|(^[a-z_][a-z0-9_-]*[$]?$)")
)

// XSnapdMode returns the file mode associated with x-snapd.mode mount option.
// If the mode is not specified explicitly then a default mode of 0755 is assumed.
func XSnapdMode(e *mount.Entry) (os.FileMode, error) {
	if opt, ok := e.OptStr("x-snapd.mode"); ok {
		if !validModeRe.MatchString(opt) {
			return 0, fmt.Errorf("cannot parse octal file mode from %q", opt)
		}
		var mode os.FileMode
		n, err := fmt.Sscanf(opt, "%o", &mode)
		if err != nil || n != 1 {
			return 0, fmt.Errorf("cannot parse octal file mode from %q", opt)
		}
		return mode, nil
	}
	return 0755, nil
}

// XSnapdUID returns the user associated with x-snapd-user mount option.  If
// the mode is not specified explicitly then a default "root" use is
// returned.
func XSnapdUID(e *mount.Entry) (uid uint64, err error) {
	if opt, ok := e.OptStr("x-snapd.uid"); ok {
		if !validUserGroupRe.MatchString(opt) {
			return math.MaxUint64, fmt.Errorf("cannot parse user name %q", opt)
		}
		// Try to parse a numeric ID first.
		if n, err := fmt.Sscanf(opt, "%d", &uid); n == 1 && err == nil {
			return uid, nil
		}
		// Fall-back to system name lookup.
		if uid, err = osutil.FindUid(opt); err != nil {
			// The error message from FindUid is not very useful so just skip it.
			return math.MaxUint64, fmt.Errorf("cannot resolve user name %q", opt)
		}
		return uid, nil
	}
	return 0, nil
}

// XSnapdGID returns the user associated with x-snapd-user mount option.  If
// the mode is not specified explicitly then a default "root" use is
// returned.
func XSnapdGID(e *mount.Entry) (gid uint64, err error) {
	if opt, ok := e.OptStr("x-snapd.gid"); ok {
		if !validUserGroupRe.MatchString(opt) {
			return math.MaxUint64, fmt.Errorf("cannot parse group name %q", opt)
		}
		// Try to parse a numeric ID first.
		if n, err := fmt.Sscanf(opt, "%d", &gid); n == 1 && err == nil {
			return gid, nil
		}
		// Fall-back to system name lookup.
		if gid, err = osutil.FindGid(opt); err != nil {
			// The error message from FindGid is not very useful so just skip it.
			return math.MaxUint64, fmt.Errorf("cannot resolve group name %q", opt)
		}
		return gid, nil
	}
	return 0, nil
}

// XSnapdEntryID returns the identifier of a given mount enrty.
//
// Identifiers are kept in the x-snapd.id mount option. The value is a string
// that identifies a mount entry and is stable across invocations of snapd. In
// absence of that identifier the entry mount point is returned.
func XSnapdEntryID(e *mount.Entry) string {
	if val, ok := e.OptStr("x-snapd.id"); ok {
		return val
	}
	return e.Dir
}

// XSnapdNeededBy the identifier of an entry which needs this entry to function.
//
// The "needed by" identifiers are kept in the x-snapd.needed-by mount option.
// The value is a string that identifies another mount entry which, in order to
// be feasible, has spawned one or more additional support entries. Each such
// entry contains the needed-by attribute.
func XSnapdNeededBy(e *mount.Entry) string {
	val, _ := e.OptStr("x-snapd.needed-by")
	return val
}

// XSnapdSynthetic returns true of a given mount entry is synthetic.
//
// Synthetic mount entries are created by snap-update-ns itself, separately
// from what snapd instructed. Such entries are needed to make other things
// possible.  They are identified by having the "x-snapd.synthetic" mount
// option.
func XSnapdSynthetic(e *mount.Entry) bool {
	return e.OptBool("x-snapd.synthetic")
}
