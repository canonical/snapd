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
	"syscall"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

// The SNAP_REEXEC environment variable controls whether the command
// will attempt to re-exec itself from inside an ubuntu-core snap
// present on the system. If not present in the environ it's assumed
// to be set to 1 (do re-exec); that is: set it to 0 to disable.
const key = "SNAP_REEXEC"

// SwitchToTheRealOne makes sure you're executing the "real"
// binary. I.e. the one that ships in the ubuntu-core snap.
func ExecInCoreSnap() {
	if !release.OnClassic {
		// you're already the real deal, natch
		return
	}

	if os.Getenv(key) == "0" {
		return
	}

	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return
	}

	exe = filepath.Join("/snap/ubuntu-core/current", exe)
	if !osutil.FileExists(exe) {
		return
	}

	env := append(os.Environ(), key+"=0")
	panic(syscall.Exec(exe, os.Args, env))
}
