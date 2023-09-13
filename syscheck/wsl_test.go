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
	"github.com/snapcore/snapd/testutil"
)

type wslSuite struct{}

var _ = Suite(&wslSuite{})

// Mocks WSL check. Values:
// - 0 to mock not being on WSL.
// - 1 to mock being on WSL 1.
// - 2 to mock being on WSL 2.
func mockOnWSL(version int) (restore func()) {
	restoreOnWSL := testutil.Backup(&release.OnWSL)
	restoreWSLVersion := testutil.Backup(&release.WSLVersion)

	release.OnWSL = version != 0
	release.WSLVersion = version

	return func() {
		restoreOnWSL()
		restoreWSLVersion()
	}
}

func (s *wslSuite) TestNonWSL(c *C) {
	defer mockOnWSL(0)()
	c.Check(syscheck.CheckWSL(), IsNil)
}

func (s *wslSuite) TestWSL1(c *C) {
	defer mockOnWSL(1)()
	c.Check(syscheck.CheckWSL(), ErrorMatches, "snapd does not work inside WSL1")
}

func (s *wslSuite) TestWSL2(c *C) {
	defer mockOnWSL(2)()
	c.Check(syscheck.CheckWSL(), IsNil)
}
