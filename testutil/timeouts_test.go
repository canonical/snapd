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

package testutil_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&TimeoutTestSuite{})

type TimeoutTestSuite struct{}

func (ts *TimeoutTestSuite) TestHostScaledTimeout(c *C) {
	restore := testutil.MockRuntimeARCH("some-fast-arch")
	defer restore()
	default_timeout := testutil.HostScaledTimeout(2 * time.Second)

	restore = testutil.MockRuntimeARCH("riscv64")
	defer restore()
	riscv64_timeout := testutil.HostScaledTimeout(2 * time.Second)

	c.Check(default_timeout, Equals, 2*time.Second)
	c.Check(riscv64_timeout > default_timeout, Equals, true)
}
