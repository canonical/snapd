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
	"github.com/snapcore/snapd/testutil"
)

func MockRuntimeGOARCH(arch string) (restore func()) {
	restore = testutil.Backup(&runtimeGOARCH)
	runtimeGOARCH = arch
	return restore
}

// MockRISCVHWProbe mocks the return value of the riscv_hwprobe syscall
// and returns a function to restore to the current value.
func MockRISCVHWProbe(mockRISCVHWProbe func(pairs []RISCVHWProbePairs, set *CPUSet, flags uint) (err error)) (restore func()) {
	// Replace the normal function with the mock one
	origRISCVHWProbe := RISCVHWProbe
	RISCVHWProbe = mockRISCVHWProbe

	// And restore the function
	return func() {
		RISCVHWProbe = origRISCVHWProbe
	}
}

// MockKernelVersion mocks the running kernel version in the form of the
// version string, and returns a function to restore to the current value.
func MockKernelVersion(newKernelVersion func() string) (restore func()) {
	originalKernelVersion := KernelVersion
	KernelVersion = newKernelVersion

	return func() { KernelVersion = originalKernelVersion }
}

// MockIsISASupportedByCPU mocks the return value of the function checking
// if a RISCV ISA is supported on the running system, and returns a function
// to restore to the current value.
func MockIsISASupportedByCPU(newArchisISASupportedByCPU func(isa string) error) (restore func()) {
	originalArchisISASupportedByCPU := IsISASupportedByCPU
	IsISASupportedByCPU = newArchisISASupportedByCPU

	return func() { IsISASupportedByCPU = originalArchisISASupportedByCPU }
}
