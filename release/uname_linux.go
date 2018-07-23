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

// We have to implement separate functions for the kernel version and the
// machine name at the moment as the utsname struct is either using int8
// or uint8 depending on the architecture the code is built for. As there
// is no easy way to generlize this implements the same code twice. The
// way to get this solved is by using []byte inside the utsname struct
// instead of []int8/[]uint8. See https://github.com/golang/go/issues/20753
// for details.

func getKernelRelease(buf *syscall.Utsname) string {
	// The Utsname structures uses [65]int8 or [65]uint8, depending on
	// architecture, to represent various fields. We need to convert them to
	// strings.
	input := buf.Release[:]
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

func getMachineName(buf *syscall.Utsname) string {
	// The Utsname structures uses [65]int8 or [65]uint8, depending on
	// architecture, to represent various fields. We need to convert them to
	// strings.
	input := buf.Machine[:]
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

// KernelVersion returns the version of the kernel or the string "unknown" if one cannot be determined.
var KernelVersion = kernelVersion

func kernelVersion() string {
	var buf syscall.Utsname
	err := syscall.Uname(&buf)
	if err != nil {
		return "unknown"
	}
	// Release is more informative than Version.
	return getKernelRelease(&buf)
}

// MockKernelVersion replaces the function that returns the kernel version string.
func MockKernelVersion(version string) (restore func()) {
	old := KernelVersion
	KernelVersion = func() string { return version }
	return func() {
		KernelVersion = old
	}
}

// Machine returns the name of the machine or the string "unknown" if one cannot be determined.
func Machine() string {
	var buf syscall.Utsname
	err := syscall.Uname(&buf)
	if err != nil {
		return "unknown"
	}
	return getMachineName(&buf)
}
