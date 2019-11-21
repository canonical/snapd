/*
 * Copyright (C) 2019 Canonical Ltd
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

package sanity

import (
	"fmt"
	"syscall"
)

var (
	loopControlPath  = "/dev/loop-control"
	loopControlMajor = 10
	loopControlMinor = 237
)

var majorMinor = func(rdev int) (major, minor int) {
	major = int(rdev / 256)
	minor = int(rdev % 256)
	return major, minor
}

func validateLoopControl() error {
	var st syscall.Stat_t
	err := syscall.Stat(loopControlPath, &st)
	if err != nil {
		return fmt.Errorf("cannot stat %q: %v", loopControlPath, err)
	}

	major, minor := majorMinor(int(st.Rdev))
	if major != loopControlMajor {
		return fmt.Errorf("unexpected major number for %q", loopControlPath)
	}
	if minor != loopControlMinor {
		return fmt.Errorf("unexpected minor number for %q", loopControlPath)
	}

	return nil
}
