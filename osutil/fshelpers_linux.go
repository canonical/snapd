// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// DeviceMajorAndMinor returns major and minor numbers for the devPath device node.
func DeviceMajorAndMinor(devPath string) (uint32, uint32, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(devPath, &stat); err != nil {
		if err == syscall.ENOENT {
			return 0, 0, os.ErrNotExist
		}
		return 0, 0, err
	}

	// Check if it is a device
	if stat.Mode&syscall.S_IFCHR == 0 && stat.Mode&syscall.S_IFBLK == 0 {
		return 0, 0, fmt.Errorf("not a device")
	}

	return unix.Major(stat.Rdev), unix.Minor(stat.Rdev), nil
}
