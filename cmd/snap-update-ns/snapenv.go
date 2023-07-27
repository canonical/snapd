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
	"strconv"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap/snapenv"
)

// snapEnv returns the environment provided by the calling process. The environment
// variables is considered trustworthy if the value environment variable "SNAP_UID"
// matches the provided user id.
func snapEnv(uid sys.UserID) (osutil.Environment, error) {
	env, err := osutil.OSEnvironmentUnescapeUnsafe(snapenv.PreservedUnsafePrefix)
	if err != nil {
		return nil, err
	}

	value, ok := env["SNAP_UID"]
	if !ok {
		return nil, fmt.Errorf("cannot find environment variable %q", "SNAP_UID")
	}
	snapUid, err := strconv.Atoi(value)
	if err != nil {
		return nil, fmt.Errorf("cannot convert environment variable %q value %q to an integer", "SNAP_UID", snapUid)
	}
	if sys.UserID(snapUid) != uid {
		return nil, fmt.Errorf("environment variable %q value %s does not match current uid %d", "SNAP_UID", snapUid, uid)
	}
	return env, nil
}

func snapEnvRealHome(uid sys.UserID) (string, error) {
	env, err := snapEnv(uid)
	if err != nil {
		return "", err
	}

	realHome, ok := env["SNAP_REAL_HOME"]
	if !ok {
		return "", fmt.Errorf("cannot find environment variable %q", "SNAP_REAL_HOME")
	}
	if realHome == "" {
		return "", fmt.Errorf("environment variable %q value is empty", "SNAP_REAL_HOME")
	}
	return realHome, nil
}
