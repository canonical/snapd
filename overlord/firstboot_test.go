// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package overlord

import (
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/firstboot"
)

type FirstBootTestSuite struct {
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(dirs.SnapFirstBootStamp)
	c.Assert(err, IsNil)

	c.Assert(FirstBoot(), Equals, firstboot.ErrNotFirstBoot)
}

func (s *FirstBootTestSuite) TestNoErrorWhenNoGadget(c *C) {
	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(dirs.SnapFirstBootStamp)
	c.Assert(err, IsNil)
}
