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

// A dispatcher for bootstrapping FIPS environment. It is expected to be
// symlinked as /usr/bin/snap,
// /usr/lib/snapd/{snapd,snap-repair,snap-bootstrap}.
//
// The dispatcher sets up the environment by expliclty enabling FIPS support
// (through GOFIPS=1), and injects environment variables such that the Go FIPS
// toolchain runtime can locate the relevant OpenSSL FIPS provider module.
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

func run() error {
	logger.SimpleSetup(nil)

	prog := os.Args[0]
	progBase := filepath.Base(prog)

	logger.Debugf("FIPS execution dispatcher for: %s", prog)

	target := func() string {
		switch progBase {
		case "snapd":
			return "/usr/lib/snapd/snapd-fips"
		case "snap":
			return "/usr/lib/snapd/snap-fips"
		case "snap-repair":
			return "/usr/lib/snapd/snap-repair-fips"
		case "snap-bootstrap":
			return "/usr/lib/snapd/snap-bootstrap-fips"
		default:
			return filepath.Join("/usr/lib/snapd", progBase)
		}
	}()

	if osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, progBase)) {
		logger.Debugf("detected snap application execution through symlink")
		// magic symlink execution through a symlink in /snap/<foo>, hand it
		// over to snap command for dispatch
		target = "/usr/lib/snapd/snap-fips"
	}

	logger.Debugf("dispatch target: %v", target)
	return snapdtool.DispatchWithFIPS(target)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
