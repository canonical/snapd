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

// Package main implements the FIPS bootstrap dispatcher for the snapd snap.
//
// In a FIPS-enabled snapd snap build, the original binaries are renamed with a
// -fips suffix and replaced by symlinks to the dispatcher:
//
//	Symlink (original path)               Dispatch target (renamed binary)
//	<root>/usr/bin/snap                -> <root>/usr/bin/snap-fips
//	<root>/usr/lib/snapd/snapd         -> <root>/usr/lib/snapd/snapd-fips
//	<root>/usr/lib/snapd/snap-repair   -> <root>/usr/lib/snapd/snap-repair-fips
//	<root>/usr/lib/snapd/snap-bootstrap -> <root>/usr/lib/snapd/snap-bootstrap-fips
//
// When invoked (via symlink), the dispatcher checks whether system-wide FIPS
// mode is enabled, sets GOFIPS=1 and configures the OpenSSL module search path
// so the Go FIPS runtime can locate the provider module bundled in the snap,
// then execs into the corresponding -fips binary.
//
// For example, when reexecuting from a native package on classic:
//
//  1. User runs /usr/bin/snap
//  2. snapd reexec logic execs /snap/snapd/current/usr/bin/snap (symlink to dispatcher)
//  3. Dispatcher resolves to /snap/snapd/current/usr/lib/snapd/snap-fips-dispatch
//  4. Dispatcher execs /snap/snapd/current/usr/bin/snap-fips (the FIPS binary)
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snapdtool"
)

var (
	snapdtoolDispatchWithFIPS = snapdtool.DispatchWithFIPS
)

func run(args []string) error {
	logger.SimpleSetup(nil)

	if len(args) == 0 {
		// only theoretical, there should be at least one argument
		return fmt.Errorf("internal error: no arguments passed")
	}

	prog := args[0]
	progBase := filepath.Base(prog)

	logger.Debugf("FIPS execution dispatcher for: %s", prog)

	// This is a short list of binaries which execute cryptography related
	// functions that fall under the FIPS spec.
	targetMapper := map[string]string{
		"snap":           "/usr/bin/snap-fips",
		"snapd":          "/usr/lib/snapd/snapd-fips",
		"snap-repair":    "/usr/lib/snapd/snap-repair-fips",
		"snap-bootstrap": "/usr/lib/snapd/snap-bootstrap-fips",
	}

	target := targetMapper[progBase]
	if target == "" {
		// could be a /snap/bin/<app> symlink
		if osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, progBase)) {
			logger.Debugf("detected snap application execution through symlink")
			// magic symlink execution through a symlink in /snap/<foo>, hand it
			// over to snap command for dispatch
			target = targetMapper["snap"]
		} else {
			if progBase == "snap-fips-dispatch" {
				return fmt.Errorf("this program is not intended to be run directly")
			}

			target = filepath.Join("/usr/lib/snapd", progBase)
		}
	}

	logger.Debugf("dispatch target: %v", target)
	return snapdtoolDispatchWithFIPS(target)
}

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
