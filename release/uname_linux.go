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

// KernelVersion returns the version of the kernel or the empty string if one cannot be determined.
func KernelVersion() string {
	var buf syscall.Utsname
	err := syscall.Uname(&buf)
	if err != nil {
		return ""
	}
	// Release is more informative than Version.
	input := buf.Release[:]
	// The Utsname structures uses [65]int8 or [65]uint8, depending on
	// architecture, to represent various fields. We need to conver them to
	// strings.
	output := make([]byte, 0, len(input))
	for _, c := range input {
		// The input buffer has fixed size but we want to break at the first
		// zero we encounter.
		if c == 0 {
			break
		}
		output = append(output, byte(c))
	}
	return string(output)
}
