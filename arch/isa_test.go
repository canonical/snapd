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
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/testutil"
)

type ISASuite struct {
	testutil.BaseTest
}

var _ = Suite(&ISASuite{})

func (s *ISASuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ISASuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *ISASuite) TestValidateISASupport(c *C) {
	// Test all branches of the switch in order
	// RISCV64
	restoreArch := func() { arch.SetArchitecture(arch.ArchitectureType(arch.DpkgArchitecture())) }
	arch.SetArchitecture("riscv64")

	restoreIsISASupportedByCPUNoError := arch.MockIsISASupportedByCPU(func(s string) error {
		return nil
	})
	err := arch.IsISASupportedByCPU("rva23")
	c.Check(err, IsNil)

	restoreIsISASupportedByCPUNoError()

	restoreIsISASupportedByCPUError := arch.MockIsISASupportedByCPU(func(s string) error {
		return fmt.Errorf("Bang")
	})
	err = arch.IsISASupportedByCPU("rva23")
	c.Check(err, ErrorMatches, "Bang")

	restoreIsISASupportedByCPUError()

	// AMD64 (for the default branch)
	arch.SetArchitecture("amd64")

	err = arch.IsISASupportedByCPU("rva23")
	c.Check(err, ErrorMatches, "ISA specification is not supported for arch: amd64")

	restoreArch()
}
