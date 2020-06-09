// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package testutil

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
)

var _ = Suite(&TimeoutTestSuite{})

type TimeoutTestSuite struct {
}

func (ts *TimeoutTestSuite) TestHostScaledTimeout(c *C) {
	currentarch := arch.ArchitectureType(arch.DpkgArchitecture())

	arch.SetArchitecture("amd64")
	amd64_timeout := HostScaledTimeout(2 * time.Second)

	arch.SetArchitecture("riscv64")
	riscv64_timeout := HostScaledTimeout(2 * time.Second)

	arch.SetArchitecture(currentarch)

	c.Check(amd64_timeout, Equals, 2*time.Second)
	c.Check(riscv64_timeout > amd64_timeout, Equals, true)
}
