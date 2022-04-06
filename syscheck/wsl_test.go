// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package syscheck_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/syscheck"
)

type wslSuite struct{}

var _ = Suite(&wslSuite{})

func mockOnWSL(on bool) (restore func()) {
	old := release.OnWSL
	release.OnWSL = on
	return func() {
		release.OnWSL = old
	}
}

func (s *wslSuite) TestNonWSL(c *C) {
	defer mockOnWSL(false)()

	c.Check(syscheck.CheckWSL(), IsNil)
}

func (s *wslSuite) TestWSL(c *C) {
	defer mockOnWSL(true)()

	c.Check(syscheck.CheckWSL(), ErrorMatches, "snapd does not work inside WSL")
}
