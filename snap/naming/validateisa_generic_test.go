// -*- Mode: Go; indent-tabs-mode: t -*-
//
//go:build !linux || !riscv64

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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
)

type ValidateISASuite struct {
	testutil.BaseTest
}

var _ = Suite(&ValidateISASuite{})

func (s *ValidateISASuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *ValidateISASuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *ValidateISASuite) TestValidateAssumesISARISCVWrongArch(c *C) {
	// Non-riscv64 built executable is running on riscv64
	assumes := []string{"isa-riscv64-rva23"}

	err := naming.ValidateAssumes(assumes, "", nil, "riscv64")

	c.Check(err, ErrorMatches, "isa-riscv64-rva23: validation failed: cannot validate RiscV ISA support while running on: linux, amd64")
}
