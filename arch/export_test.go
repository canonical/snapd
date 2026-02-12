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

package arch

import (
	"fmt"

	"github.com/snapcore/snapd/testutil"
	"golang.org/x/sys/unix"
)

func MockRuntimeGOARCH(arch string) (restore func()) {
	restore = testutil.Backup(&runtimeGOARCH)
	runtimeGOARCH = arch
	return restore
}

var calledMockRISCVHWProbe = false

// CalledMockRISCVHWProbe returns whether the mocked riscv_hwprobe syscall
// was executed as part of a test plan.
func CalledMockRISCVHWProbe() bool {
	return calledMockRISCVHWProbe
}

// MockRISCVHWProbe mocks the return value of the riscv_hwprobe syscall
// and returns a function to restore to the current value.
func MockRISCVHWProbe(supportedExtensions []RISCVHWProbePairs, syscallError string) (restore func()) {
	// Mock probe function that copies the test case's supportedExtensions over the input
	var mockRISCVHWProbe = func(pairs []RISCVHWProbePairs, set *unix.CPUSet, flags uint) (err error) {
		// Mark that we called the function for some tests
		calledMockRISCVHWProbe = true

		// Return an error if specified in the test case
		if syscallError != "" {
			return fmt.Errorf(syscallError)
		}

		if len(supportedExtensions) != 0 {
			// Otherwise, write the requested value
			pairs[0] = supportedExtensions[0]
			pairs[1] = supportedExtensions[1]
		}

		return nil
	}

	// Replace the normal function with the mock one
	normalRISCVHWProbe := RISCVHWProbe
	RISCVHWProbe = mockRISCVHWProbe

	// And restore the function and the "called" flag
	return func() {
		RISCVHWProbe = normalRISCVHWProbe
		calledMockRISCVHWProbe = false
	}
}

// MockKernelVersion mocks the running kernel version in the form of the
// version string, and returns a function to restore to the current value.
func MockKernelVersion(newKernelVersion string) (restore func()) {
	// Do nothing if no kernel version specified
	if newKernelVersion == "" {
		return func() {}
	}

	originalKernelVersion := KernelVersion
	KernelVersion = func() string { return newKernelVersion }

	return func() { KernelVersion = originalKernelVersion }
}
