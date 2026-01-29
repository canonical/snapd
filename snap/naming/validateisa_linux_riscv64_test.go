// -*- Mode: Go; indent-tabs-mode: t -*-
//
//notsetupgo:build linux && riscv64

/*
 * Copyright (C) 2022 Canonical Ltd
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

package naming_test

import (
	"fmt"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type ValidateISASuite struct {
	testutil.BaseTest
}

var _ = Suite(&ValidateISASuite{})

func (s *ValidateISASuite) SetUpSuite(c *C) {
	// Construct bitmasks with the minimal extensions needed
	minimumRVA23Extensions = []unix.RISCVHWProbePairs{
		{
			Key:   unix.RISCV_HWPROBE_KEY_BASE_BEHAVIOR,
			Value: unix.RISCV_HWPROBE_BASE_BEHAVIOR_IMA,
		},
		{Key: unix.RISCV_HWPROBE_KEY_IMA_EXT_0},
	}

	// OR all the required extensions' keys
	for _, ext := range naming.RiscVExtensions {
		if ext.Required {
			minimumRVA23Extensions[1].Value |= ext.Key
		}
	}
}

func (s *ValidateISASuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ValidateISASuite) TearDownTest(c *C) {
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
	normalRISCVHWProbe := naming.RISCVHWProbe
	naming.RISCVHWProbe = mockRISCVHWProbe

	// And restore the function and the "called" flag
	return func() {
		naming.RISCVHWProbe = normalRISCVHWProbe
		riscvHWProbeCalled = false
	}
}

var minimumRVA23Extensions []unix.RISCVHWProbePairs

var riscvHWProbeCalled = false

func (s *ValidateISASuite) TestValidateAssumesISARISCV(c *C) {
	var assumesTests = []struct {
		assumes                  []string
		arch                     string
		supportedExtensions      []unix.RISCVHWProbePairs
		expectedRISCVHWProbeCall bool
		syscallError             string
		err                      string
	}{
		{
			// Success case
			assumes:                  []string{"isa-riscv64-rva23"},
			arch:                     "riscv64",
			supportedExtensions:      minimumRVA23Extensions,
			expectedRISCVHWProbeCall: true,
		}, {
			// Different architecture ignored with no error
			assumes:                  []string{"isa-riscv64-rva23"},
			arch:                     "amd64",
			expectedRISCVHWProbeCall: false,
		}, {
			// ISA not supported
			assumes:                  []string{"isa-riscv64-badisa"},
			arch:                     "riscv64",
			expectedRISCVHWProbeCall: false,
			err:                      "isa-riscv64-badisa: validation failed: unsupported ISA for riscv64 architecture: badisa",
		}, {
			// Base IMA support missing
			assumes: []string{"isa-riscv64-rva23"},
			arch:    "riscv64",
			supportedExtensions: []unix.RISCVHWProbePairs{
				{
					Key:   unix.RISCV_HWPROBE_KEY_BASE_BEHAVIOR,
					Value: 0,
				},
				minimumRVA23Extensions[1],
			},
			expectedRISCVHWProbeCall: true,
			err:                      "isa-riscv64-rva23: validation failed: missing base RISC-V support",
		}, {
			// Missing required Zicboz extension
			assumes: []string{"isa-riscv64-rva23"},
			arch:    "riscv64",
			supportedExtensions: []unix.RISCVHWProbePairs{
				minimumRVA23Extensions[0],
				{

					Key:   unix.RISCV_HWPROBE_KEY_IMA_EXT_0,
					Value: minimumRVA23Extensions[1].Value & ^naming.RISCV_HWPROBE_EXT_ZICBOZ,
				},
			},
			expectedRISCVHWProbeCall: true,
			err:                      "isa-riscv64-rva23: validation failed: missing required RVA23 extension: Zicboz",
		}, {
			// Error in the syscall
			assumes:                  []string{"isa-riscv64-rva23"},
			arch:                     "riscv64",
			supportedExtensions:      minimumRVA23Extensions,
			expectedRISCVHWProbeCall: true,
			syscallError:             "missing CPU...",
			err:                      "isa-riscv64-rva23: validation failed: error while querying RVA23 extensions supported by CPU: missing CPU...",
		},
	}

	for _, test := range assumesTests {
		// Mock probe function
		restoreRISCVHWProbe := MockRISCVHWProbe(test.supportedExtensions, test.syscallError)

		err := naming.ValidateAssumes(test.assumes, "", nil, test.arch)

		if test.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, test.err)
		}

		c.Check(riscvHWProbeCalled, Equals, test.expectedRISCVHWProbeCall)

		restoreRISCVHWProbe()
	}
}
