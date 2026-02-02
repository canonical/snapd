// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build linux && riscv64

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

package arch_test

import (
	"fmt"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/testutil"
)

type ValidateEncodeKernelVersion struct {
	testutil.BaseTest
}

var _ = Suite(&ValidateEncodeKernelVersion{})

func (s *ValidateEncodeKernelVersion) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ValidateEncodeKernelVersion) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

// Mock Uname behavior, returning the desired release string
func MockOsutilKernelVersion(release string) (restore func()) {
	// Do nothing if the test doesn't specify a kernel version to use
	if release == "" {
		return func() {}
	}

	// Create mock returning the desired string
	var mockOsutilKernelVersion = func() (_ string) {
		return release
	}

	// Replace the normal function with the mock one
	normalOsutilKernelVersion := arch.KernelVersion
	arch.KernelVersion = mockOsutilKernelVersion

	// And restore the function
	return func() {
		arch.KernelVersion = normalOsutilKernelVersion
	}
}

func (s *ValidateEncodeKernelVersion) TestEncodeKernelVersion(c *C) {
	var assumesTests = []struct {
		kernelVersion  string
		expectedResult uint32
		expectedError  string
	}{
		{
			// Success case
			kernelVersion:  "6.14.0-24-generic",
			expectedResult: 0x0006000e,
		}, {
			// Incorrect value from uname
			kernelVersion: "6-14-0-24-generic",
			expectedError: "uname returned incorrect value: 6-14-0-24-generic",
		}, {
			// Major number parsing error
			kernelVersion: "abc.14.0-24-generic",
			expectedError: "error parsing major kernel version: abc",
		}, {
			// Minor number parsing error
			kernelVersion: "6.abc.0-24-generic",
			expectedError: "error parsing minor kernel version: abc",
		},
	}

	for _, test := range assumesTests {
		// Mock probe function
		restoreOsutilKernelVersion := MockOsutilKernelVersion(test.kernelVersion)

		c.Check(arch.KernelVersion(), Equals, test.kernelVersion)

		result, err := arch.EncodedKernelVersion()

		if test.expectedError == "" {
			c.Check(err, IsNil)
			c.Check(result, Equals, test.expectedResult)
		} else {
			c.Check(err, ErrorMatches, test.expectedError)
		}

		restoreOsutilKernelVersion()
	}
}

type ISASupportSuite struct {
	testutil.BaseTest
}

var _ = Suite(&ISASupportSuite{})

func (s *ISASupportSuite) SetUpSuite(c *C) {
	// Construct bitmasks with the minimal extensions needed
	minimumRVA23Extensions = []unix.RISCVHWProbePairs{
		{
			Key:   unix.RISCV_HWPROBE_KEY_BASE_BEHAVIOR,
			Value: unix.RISCV_HWPROBE_BASE_BEHAVIOR_IMA,
		},
		{Key: unix.RISCV_HWPROBE_KEY_IMA_EXT_0},
	}

	// OR all the required extensions' keys
	for _, ext := range arch.RiscVExtensions {
		if ext.Required {
			minimumRVA23Extensions[1].Value |= ext.Key
		}
	}
}

func (s *ISASupportSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ISASupportSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

// Mock the Syscall behavior, copying into the 'pairs' argument the bitmasks specified
// by the test case
func MockRISCVHWProbe(supportedExtensions []unix.RISCVHWProbePairs, syscallError string) (restore func()) {
	// Mock probe function that copies the test case's supportedExtensions over the input
	var mockRISCVHWProbe = func(pairs []unix.RISCVHWProbePairs, set *unix.CPUSet, flags uint) (err error) {
		// Mark that we called the function for some tests
		riscvHWProbeCalled = true

		// Return an error if specified in the test case
		if syscallError != "" {
			return fmt.Errorf(syscallError)
		}

		// Otherwise, behave correctly and mock the syscall
		pairs[0] = supportedExtensions[0]
		pairs[1] = supportedExtensions[1]

		return nil
	}

	// Replace the normal function with the mock one
	normalRISCVHWProbe := arch.RISCVHWProbe
	arch.RISCVHWProbe = mockRISCVHWProbe

	// And restore the function and the "called" flag
	return func() {
		arch.RISCVHWProbe = normalRISCVHWProbe
		riscvHWProbeCalled = false
	}
}

var minimumRVA23Extensions []unix.RISCVHWProbePairs

var riscvHWProbeCalled = false

func (s *ISASupportSuite) TestValidateAssumesISARISCV(c *C) {
	var assumesTests = []struct {
		isa                      string
		arch                     string
		supportedExtensions      []unix.RISCVHWProbePairs
		expectedRISCVHWProbeCall bool
		hwprobeSyscallError      string
		kernelVersion            string
		unameSyscallError        string
		expectedError            string
	}{
		{
			// Success case
			isa:                      "rva23",
			arch:                     "riscv64",
			supportedExtensions:      minimumRVA23Extensions,
			expectedRISCVHWProbeCall: true,
		}, {
			// ISA not supported
			isa:                      "badisa",
			arch:                     "riscv64",
			expectedRISCVHWProbeCall: false,
			expectedError:            "unsupported ISA for riscv64 architecture: badisa",
		}, {
			// Base IMA support missing
			isa:  "rva23",
			arch: "riscv64",
			supportedExtensions: []unix.RISCVHWProbePairs{
				{
					Key:   unix.RISCV_HWPROBE_KEY_BASE_BEHAVIOR,
					Value: 0,
				},
				minimumRVA23Extensions[1],
			},
			expectedRISCVHWProbeCall: true,
			expectedError:            "missing base RISC-V support",
		}, {
			// Missing required Zicboz extension, not dependent on kernel version
			isa:  "rva23",
			arch: "riscv64",
			supportedExtensions: []unix.RISCVHWProbePairs{
				minimumRVA23Extensions[0],
				{
					Key:   unix.RISCV_HWPROBE_KEY_IMA_EXT_0,
					Value: minimumRVA23Extensions[1].Value & ^arch.RISCV_HWPROBE_EXT_ZICBOZ,
				},
			},
			expectedRISCVHWProbeCall: true,
			expectedError:            "missing required RVA23 extension: Zicboz",
		}, {
			// Error in the hwprobe syscall
			isa:                      "rva23",
			arch:                     "riscv64",
			supportedExtensions:      minimumRVA23Extensions,
			expectedRISCVHWProbeCall: true,
			hwprobeSyscallError:      "missing CPU...",
			expectedError:            "error while querying RVA23 extensions supported by CPU: missing CPU...",
		}, {
			// Missing required Zicntr extension, introduced in 6.15 kernel
			// does not generate errors when running on 6.14
			isa:  "rva23",
			arch: "riscv64",
			supportedExtensions: []unix.RISCVHWProbePairs{
				minimumRVA23Extensions[0],
				{
					Key:   unix.RISCV_HWPROBE_KEY_IMA_EXT_0,
					Value: minimumRVA23Extensions[1].Value & ^arch.RISCV_HWPROBE_EXT_ZICNTR,
				},
			},
			expectedRISCVHWProbeCall: true,
			kernelVersion:            "6.14.0-24-generic",
		}, {
			// Missing required Supm extension, introduced in 6.13 kernel
			// returns error when running on 6.14
			isa:  "rva23",
			arch: "riscv64",
			supportedExtensions: []unix.RISCVHWProbePairs{
				minimumRVA23Extensions[0],
				{
					Key:   unix.RISCV_HWPROBE_KEY_IMA_EXT_0,
					Value: minimumRVA23Extensions[1].Value & ^arch.RISCV_HWPROBE_EXT_SUPM,
				},
			},
			expectedRISCVHWProbeCall: true,
			kernelVersion:            "6.14.0-24-generic",
			expectedError:            "missing required RVA23 extension: Supm",
		}, {
			// Gracefully handle case where EncodeKernelVersion returns error
			// due to malformed output of osutil.KernelVersion()
			isa:           "rva23",
			arch:          "riscv64",
			kernelVersion: "6-14-0-24-generic",
			expectedError: "error while querying installed kernel version: uname returned incorrect value: 6-14-0-24-generic",
		},
	}

	for _, test := range assumesTests {
		// Mock functions
		restoreRISCVHWProbe := MockRISCVHWProbe(test.supportedExtensions, test.hwprobeSyscallError)
		restoreOsutilKernelVersion := MockOsutilKernelVersion(test.kernelVersion)

		err := arch.IsRISCVISASupported(test.isa)

		if test.expectedError == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, test.expectedError)
		}

		c.Check(riscvHWProbeCalled, Equals, test.expectedRISCVHWProbeCall)

		// Restore functions
		restoreRISCVHWProbe()
		restoreOsutilKernelVersion()
	}
}
