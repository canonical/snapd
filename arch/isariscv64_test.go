// -*- Mode: Go; indent-tabs-mode: t -*-
//

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
	"runtime"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/testutil"
)

var oldArch = ""

type NonRISCVISASuite struct {
	testutil.BaseTest
}

var _ = Suite(&NonRISCVISASuite{})

func (s *NonRISCVISASuite) SetUpSuite(c *C) {
	oldArch = arch.DpkgArchitecture()
	arch.SetArchitecture("amd64")
}

func (s *NonRISCVISASuite) TearDownSuite(c *C) {
	arch.SetArchitecture(arch.ArchitectureType(oldArch))
}

func (s *NonRISCVISASuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *NonRISCVISASuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *NonRISCVISASuite) TestValidateAssumesISARISCVWrongArch(c *C) {
	// Check RISCV ISA support on non-RISCV device
	err := arch.IsRISCVISASupported("")

	// This will always error out regardless of the OS
	c.Check(err, ErrorMatches, "cannot validate RiscV ISA support while running on: "+runtime.GOOS+", amd64. Need linux, riscv64.")
}

type RISCVISASuite struct {
	testutil.BaseTest
}

var _ = Suite(&RISCVISASuite{})

func (s *RISCVISASuite) SetUpSuite(c *C) {
	// Construct bitmasks with the minimal extensions needed
	minimumRVA23Extensions = []arch.RISCVHWProbePairs{
		{
			Key:   arch.RISCV_HWPROBE_KEY_BASE_BEHAVIOR,
			Value: arch.RISCV_HWPROBE_BASE_BEHAVIOR_IMA,
		},
		{Key: arch.RISCV_HWPROBE_KEY_IMA_EXT_0},
	}

	// OR all the required extensions' keys
	for _, ext := range arch.RiscVExtensions {
		if ext.Required {
			minimumRVA23Extensions[1].Value |= ext.Key
		}
	}

	oldArch = arch.DpkgArchitecture()
	arch.SetArchitecture("riscv64")
}

func (s *RISCVISASuite) TearDownSuite(c *C) {
	arch.SetArchitecture(arch.ArchitectureType(oldArch))
}

func (s *RISCVISASuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *RISCVISASuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

var minimumRVA23Extensions []arch.RISCVHWProbePairs

func (s *RISCVISASuite) TestValidateAssumesISARISCV(c *C) {
	if runtime.GOOS != "linux" {
		err := arch.IsRISCVISASupported("")

		c.Check(err, ErrorMatches, "cannot validate RiscV ISA support while running on: "+runtime.GOOS+", riscv64. Need linux, riscv64.")
	} else {
		var assumesTests = []struct {
			isa                      string
			arch                     string
			supportedExtensions      []arch.RISCVHWProbePairs
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
				supportedExtensions: []arch.RISCVHWProbePairs{
					{
						Key:   arch.RISCV_HWPROBE_KEY_BASE_BEHAVIOR,
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
				supportedExtensions: []arch.RISCVHWProbePairs{
					minimumRVA23Extensions[0],
					{
						Key:   arch.RISCV_HWPROBE_KEY_IMA_EXT_0,
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
				supportedExtensions: []arch.RISCVHWProbePairs{
					minimumRVA23Extensions[0],
					{
						Key:   arch.RISCV_HWPROBE_KEY_IMA_EXT_0,
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
				supportedExtensions: []arch.RISCVHWProbePairs{
					minimumRVA23Extensions[0],
					{
						Key:   arch.RISCV_HWPROBE_KEY_IMA_EXT_0,
						Value: minimumRVA23Extensions[1].Value & ^arch.RISCV_HWPROBE_EXT_SUPM,
					},
				},
				expectedRISCVHWProbeCall: true,
				kernelVersion:            "6.14.0-24-generic",
				expectedError:            "missing required RVA23 extension: Supm",
			},
		}

		for _, test := range assumesTests {
			// Mock kernel version and riscv_hwprobe
			restoreRISCVHWProbe := arch.MockRISCVHWProbe(test.supportedExtensions, test.hwprobeSyscallError)
			restoreOsutilKernelVersion := arch.MockKernelVersion(test.kernelVersion)

			err := arch.IsRISCVISASupported(test.isa)

			if test.expectedError == "" {
				c.Check(err, IsNil)
			} else {
				c.Check(err, ErrorMatches, test.expectedError)
			}

			c.Check(arch.CalledMockRISCVHWProbe(), Equals, test.expectedRISCVHWProbeCall)

			// Restore functions
			restoreRISCVHWProbe()
			restoreOsutilKernelVersion()
		}
	}
}
