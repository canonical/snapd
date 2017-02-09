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
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/reexec"
)

// ExecInCoreSnap makes sure you're executing the binary that ships in
// the core snap.
func ExecInCoreSnap() {
	// did we already re-exec?
	if osutil.GetenvBool("SNAP_DID_REEXEC") {
		return
	}

	full := reexec.Path(Version)
	if full == "" {
		return
	}
	logger.Debugf("restarting into %q", full)

	env := append(os.Environ(), "SNAP_DID_REEXEC=1")
	panic(syscall.Exec(full, os.Args, env))
}
