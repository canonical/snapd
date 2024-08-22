// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !linux

/*
 * Copyright (C) 2018 Canonical Ltd
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

package snapdtool

import (
	"errors"
)

var errUnsupported = errors.New("unsupported on non-Linux systems")

// ExecInSnapdOrCoreSnap makes sure you're executing the binary that ships in
// the snapd/core snap.
// On this OS this is a stub.
func ExecInSnapdOrCoreSnap() {
	return
}

// InternalToolPath returns the path of an internal snapd tool. The tool
// *must* be located inside the same tree as the current binary.
//
// On this OS this is a stub and always returns an error.
func InternalToolPath(tool string) (string, error) {
	return "", errUnsupported
}

// IsReexecd returns true when the current process binary is running from a snap.
//
// On this OS this is a stub and always returns an error.
func IsReexecd() (bool, error) {
	return false, errUnsupported
}

// DistroSupportsReExec returns true if the distribution we are running on can use re-exec.
//
// On this OS this is a stub and always returns false.
func DistroSupportsReExec() bool {
	return false
}

// IsReexecEnabled checks the environment and configuration to assert whether
// reexec has been explicitly enabled/disabled.
//
// On this OS this is a stub and always returns false.
func IsReexecEnabled() bool {
	return false
}

// IsReexecExplicitlyEnabled is a stronger check than IsReexecEnabled as it
// really expects the relevant environment variable to be set.
//
// On this OS this is a stub and always returns false.
func IsReexecExplicitlyEnabled() bool {
	return false
}
