// -*- Mode: Go; indent-tabs-mode: t -*-

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

package selinux

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

// IsEnabled checks whether SELinux is enabled
func IsEnabled() (bool, error) {
	mnt, err := getSELinuxMount()
	if err != nil {
		return false, fmt.Errorf("failed to obtain SELinux mount path: %v", err)
	}
	return mnt != "", nil
}

// IsEnabled checks whether SELinux is in enforcing mode
func IsEnforcing() (bool, error) {
	mnt, err := getSELinuxMount()
	if err != nil {
		return false, fmt.Errorf("failed to obtain SELinux mount path: %v", err)
	}
	if mnt == "" {
		// not enabled
		return false, nil
	}

	rawState, err := os.ReadFile(filepath.Join(mnt, "enforce"))
	if err != nil {
		return false, err
	}
	switch {
	case bytes.Equal(rawState, []byte("0")):
		return false, nil
	case bytes.Equal(rawState, []byte("1")):
		return true, nil
	}
	return false, fmt.Errorf("unknown SELinux status: %s", rawState)
}

func getSELinuxMount() (string, error) {
	mountinfo, err := osutil.LoadMountInfo()
	if err != nil {
		return "", err
	}
	for _, entry := range mountinfo {
		if entry.FsType == "selinuxfs" {
			return entry.MountDir, nil
		}
	}
	return "", nil
}
