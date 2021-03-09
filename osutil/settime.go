// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"syscall"
	"time"
)

// exposed for different 32-bit vs 64-bit definitions of syscall.Timeval
var timeToTimeval func(time.Time) *syscall.Timeval

// exposed for mocking, since we can't actually call this without being root
// on system running tests
var syscallSettimeofday = syscall.Settimeofday

// SetTime sets the time of the system using settimeofday(). This syscall needs
// to be performed as root or with CAP_SYS_TIME.
func SetTime(t time.Time) error {
	tv := timeToTimeval(t)

	return syscallSettimeofday(tv)
}
