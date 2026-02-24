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

package naming_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type ValidateRISCVISASuite struct {
	testutil.BaseTest
}

var _ = Suite(&ValidateRISCVISASuite{})

func (s *ValidateRISCVISASuite) SetUpSuite(c *C) {
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
}

func (s *ValidateRISCVISASuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ValidateRISCVISASuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

var minimumRVA23Extensions []arch.RISCVHWProbePairs

func (s *ValidateRISCVISASuite) TestValidateAssumesISARISCV(c *C) {
	var assumesTests = []struct {
		assumes                  []string
		arch                     string
		isRISCVISASupportedError string
		expectedError            string
	}{
		// In this test function, we only check the explicit success case, and one failure
		// case to cover the error path. More detailed tests for the underlying operations are
		// found in the arch package
		{
			// Success case
			assumes: []string{"isa-riscv64-rva23"},
			arch:    "riscv64",
		}, {
			// ISA not supported
			assumes:                  []string{"isa-riscv64-badisa"},
			arch:                     "riscv64",
			isRISCVISASupportedError: "unsupported ISA for riscv64 architecture: badisa",
			expectedError:            "isa-riscv64-badisa: unsupported ISA for riscv64 architecture: badisa",
		},
	}

	for _, test := range assumesTests {
		// Mock function checking for ISA support
		restoreIsRISCVISASupported := naming.MockArchIsISASupportedByCPU(func(isa string) error {
			if test.isRISCVISASupportedError == "" {
				return nil
			} else {
				return fmt.Errorf(test.isRISCVISASupportedError)
			}
		})

		err := naming.ValidateAssumes(test.assumes, "", nil, test.arch)

		if test.expectedError == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, test.expectedError)
		}

		restoreIsRISCVISASupported()
	}
}
