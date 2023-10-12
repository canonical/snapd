// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/snapenv"
)

var osutilOSEnvironmentUnescapeUnsafe = osutil.OSEnvironmentUnescapeUnsafe

// snapEnv returns the environment variable SNAP_REAL_HOME provided by the calling process.
func snapEnvRealHome() (string, error) {
	env, err := osutilOSEnvironmentUnescapeUnsafe(snapenv.PreservedUnsafePrefix)
	if err != nil {
		return "", err
	}

	const snapRealHome = "SNAP_REAL_HOME"
	realHome, ok := env[snapRealHome]
	if !ok {
		return "", fmt.Errorf("cannot find environment variable %q", snapRealHome)
	}
	if realHome == "" {
		return "", fmt.Errorf("environment variable %q value is empty", snapRealHome)
	}
	return realHome, nil
}
