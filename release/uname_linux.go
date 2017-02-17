// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package release

import (
	"syscall"
)

func int8ToString(input []int8) string {
	output := make([]byte, len(input))
	for i, c := range input {
		output[i] = byte(c)
	}
	return string(output)
}

// KernelVersion returns the version of the kernel or the empty string if one cannot be determined.
func KernelVersion() string {
	var buf syscall.Utsname
	err := syscall.Uname(&buf)
	if err != nil {
		return ""
	}
	// Release is more informative than Version.
	return int8ToString(buf.Release[:])
}
