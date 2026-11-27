// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package dmverity

import (
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/kcmdline"
)

// testingBuild is set to true in binaries built with the withtestkeys
// build tag (see testing_gate_testkeys.go).
var testingBuild = false

// testingKernelCmdlineArg is a test-only kernel command line argument
// used to opt-in to using dm-verity.
const testingKernelCmdlineArg = "snapd.testing.dm-verity"

// EnabledForTesting reports whether dm-verity is enabled for testing.
// This is only the case for binaries built with the withtestkeys build tag
// and when the kernel command line has snapd.testing.dm-verity=1.
//
// This is a temporary gate until dm-verity support is complete.
func EnabledForTesting() bool {
	if !testingBuild {
		return false
	}
	m, err := kcmdline.KeyValues(testingKernelCmdlineArg)
	if err != nil {
		logger.Noticef("cannot read kernel command line, dm-verity not enabled for testing: %v", err)
		return false
	}
	return m[testingKernelCmdlineArg] == "1"
}
