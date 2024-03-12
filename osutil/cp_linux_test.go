// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package osutil_test

import (
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func (s *cpSuite) TestCpMulti(c *C) {
	r := osutil.MockMaxCp(2)
	defer r()

	c.Check(osutil.CopyFile(s.f1, s.f2, osutil.CopyFlagDefault), IsNil)
	c.Check(s.f2, testutil.FileEquals, s.data)
}

func (s *cpSuite) TestDoCpErr(c *C) {
	c.Assert(os.WriteFile(s.f2, nil, 0444), IsNil)

	src, err := os.Open(s.f1)
	c.Assert(err, IsNil)
	defer src.Close()

	roFd, err := os.Open(s.f2)
	c.Assert(err, IsNil)
	defer roFd.Close()

	// force an error by asking it to write to a readonly stream
	st, err := src.Stat()
	c.Assert(err, IsNil)
	c.Check(osutil.DoCopyFile(src, roFd, st), NotNil)
}
