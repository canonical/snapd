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

package osutil

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"
)

type ChdirTestSuite struct{}

var _ = Suite(&ChdirTestSuite{})

func (ts *ChdirTestSuite) TestChdir(c *C) {
	tmpdir := c.MkDir()
	actualTmpdir, err := filepath.EvalSymlinks(tmpdir)
	c.Assert(err, IsNil)

	cwd, err := os.Getwd()
	c.Assert(err, IsNil)
	c.Assert(cwd, Not(Equals), tmpdir)
	ChDir(tmpdir, func() error {
		cwd, err := os.Getwd()
		c.Assert(err, IsNil)
		c.Assert(cwd, Equals, actualTmpdir)
		return err
	})
}

func (ts *ChdirTestSuite) TestChdirErrorNoDir(c *C) {
	err := ChDir("random-dir-that-does-not-exist", func() error {
		return nil
	})
	c.Assert(err, ErrorMatches, "chdir .*: no such file or directory")
}

func (ts *ChdirTestSuite) TestChdirErrorFromFunc(c *C) {
	err := ChDir("/", func() error {
		return fmt.Errorf("meep")
	})
	c.Assert(err, ErrorMatches, "meep")
}
