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

package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// The SNAP_REEXEC environment variable controls whether the command
// will attempt to re-exec itself from inside an ubuntu-core snap
// present on the system. If not present in the environ it's assumed
// to be set to 1 (do re-exec); that is: set it to 0 to disable.
const key = "SNAP_REEXEC"

// newCore is the place to look for the core snap; everything in this
// location will be new enough to re-exec into.
const newCore = "/snap/core/current"

// oldCore is the previous location of the core snap. Only things
// newer than minOldRevno will be ok to re-exec into.
const oldCore = "/snap/ubuntu-core/current"

// old ubuntu-core snaps older than this aren't suitable targets for re-execage
const minOldRevno = 126

// ExecInCoreSnap makes sure you're executing the binary that ships in
// the core snap.
func ExecInCoreSnap() {
	if !release.OnClassic {
		// you're already the real deal, natch
		return
	}

	// should we re-exec? no option in the environment means yes
	if !osutil.GetenvBool(key, true) {
		return
	}

	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return
	}

	full := filepath.Join(newCore, exe)
	if !osutil.FileExists(full) {
		if rev, err := os.Readlink(oldCore); err != nil {
			return
		} else if revno, err := strconv.Atoi(rev); err != nil || revno < minOldRevno {
			return
		}

		full = filepath.Join(oldCore, exe)
		if !osutil.FileExists(full) {
			return
		}
	}

	// ensure we do not re-exec into an older version of snapd
	currentSnapd, err := os.Stat(exe)
	if err != nil {
		logger.Noticef("cannot stat %q: %s", exe, err)
		return
	}
	coreSnapSnapd, err := os.Stat(full)
	if err != nil {
		logger.Noticef("cannot stat %q: %s", full, err)
		return
	}
	if currentSnapd.ModTime().After(coreSnapSnapd.ModTime()) {
		logger.Debugf("not restarting into %q: older than %q", full, exe)
		return
	}

	logger.Debugf("restarting into %q", full)

	env := append(os.Environ(), key+"=0")
	panic(syscall.Exec(full, os.Args, env))
}
