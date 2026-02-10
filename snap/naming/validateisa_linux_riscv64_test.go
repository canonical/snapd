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

package naming_test

import (
	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
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
	for _, ext := range arch.RiscVExtensions {
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

var minimumRVA23Extensions []unix.RISCVHWProbePairs

func (s *ValidateISASuite) TestValidateAssumesISARISCV(c *C) {
	var assumesTests = []struct {
		assumes             []string
		arch                string
		supportedExtensions []unix.RISCVHWProbePairs
		expectedError       string
	}{
		// In this test function, we only check the explicit success case, and one failure
		// case to cover the error path. More detailed tests for the underlying operations are
		// found in the arch package
		{
			// Success case
			assumes:             []string{"isa-riscv64-rva23"},
			arch:                "riscv64",
			supportedExtensions: minimumRVA23Extensions,
		}, {
			// ISA not supported
			assumes:       []string{"isa-riscv64-badisa"},
			arch:          "riscv64",
			expectedError: "isa-riscv64-badisa: validation failed: unsupported ISA for riscv64 architecture: badisa",
		},
	}

	for _, test := range assumesTests {
		// Mock riscv_hwprobe syscall
		restoreRISCVHWProbe := arch.MockRISCVHWProbe(test.supportedExtensions, "")

		err := naming.ValidateAssumes(test.assumes, "", nil, test.arch)

		if test.expectedError == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, test.expectedError)
		}

		restoreRISCVHWProbe()
	}
}
