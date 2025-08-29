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

// A dispatcher for bootstrapping FIPS environment. It is expected to be shipped
// only inside the snapd snapd and symlinked as and dispatch to the following
// binaries located within the snapd snap:
//
// <root>/usr/bin/snap					-> <root>/usr/bin/snap-fips
// <root>/usr/lib/snapd/snapd			-> <root>/usr/lib/snapd/snapd-fips
// <root>/usr/lib/snapd/snap-repair		-> <root>/usr/lib/snapd/snap-repair
// <root>/usr/lib/snapd/snap-bootstrap	-> <root>/usr/lib/snapd/snap-bootstrap-fips
//
// The dispatcher sets up the environment by expliclty enabling FIPS support
// (through GOFIPS=1), and injects environment variables such that the Go FIPS
// toolchain runtime can locate the relevant OpenSSL FIPS provider module. Next
// it exects into the target binary located within the rootfs where the
// dispatcher's own binary is located. In case of snapd snap, it effectively
// reexecs into a corresponding FIPS binary in the snap.
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
		return fmt.Errorf("internal error: no arguments passed")
	}

	prog := args[0]
	progBase := filepath.Base(prog)

	logger.Debugf("FIPS execution dispatcher for: %s", prog)

	targetMapper := map[string]string{
		"snapd":          "/usr/lib/snapd/snapd-fips",
		"snap":           "/usr/bin/snap-fips",
		"snap-repair":    "/usr/lib/snapd/snap-repair-fips",
		"snap-bootstrap": "/usr/lib/snapd/snap-bootstrap-fips",
	}

	target := targetMapper[progBase]
	if target == "" {
		if osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, progBase)) {
			logger.Debugf("detected snap application execution through symlink")
			// magic symlink execution through a symlink in /snap/<foo>, hand it
			// over to snap command for dispatch
			target = targetMapper["snap"]
		} else {
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
