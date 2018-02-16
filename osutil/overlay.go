// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package osutil

import (
	"fmt"
)

// IsRootOverlay returns true if overlayfs is being used for '/'
//
// Currently uses variables and Mock functions from nfs.go
func IsRootOverlay() (bool, error) {
	mountinfo, err := LoadMountInfo(procSelfMountInfo)
	if err != nil {
		return false, fmt.Errorf("cannot parse %s: %s", procSelfMountInfo, err)
	}
	for _, entry := range mountinfo {
		if entry.FsType == "overlay" && entry.MountDir == "/" {
			return true, nil
		}
	}
	fstab, err := LoadMountProfile(etcFstab)
	if err != nil {
		return false, fmt.Errorf("cannot parse %s: %s", etcFstab, err)
	}
	for _, entry := range fstab.Entries {
		if entry.Type == "overlay" && entry.Dir == "/" {
			return true, nil
		}
	}
	return false, nil
}
