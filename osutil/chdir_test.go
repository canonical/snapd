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
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

type ChdirTestSuite struct{}

var _ = Suite(&ChdirTestSuite{})

func (ts *ChdirTestSuite) TestChdir(c *C) {
	tmpdir := c.MkDir()

	cwd := mylog.Check2(os.Getwd())

	c.Assert(cwd, Not(Equals), tmpdir)
	osutil.ChDir(tmpdir, func() error {
		cwd := mylog.Check2(os.Getwd())

		c.Assert(cwd, Equals, tmpdir)
		return err
	})
}

func (ts *ChdirTestSuite) TestChdirErrorNoDir(c *C) {
	mylog.Check(osutil.ChDir("random-dir-that-does-not-exist", func() error {
		return nil
	}))
	c.Assert(err, ErrorMatches, "chdir .*: no such file or directory")
}

func (ts *ChdirTestSuite) TestChdirErrorFromFunc(c *C) {
	mylog.Check(osutil.ChDir("/", func() error {
		return fmt.Errorf("meep")
	}))
	c.Assert(err, ErrorMatches, "meep")
}
