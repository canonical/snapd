// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"gopkg.in/check.v1"
)

type renameSuite struct{}

var _ = check.Suite(renameSuite{})

func (renameSuite) TestSwapDirs(c *check.C) {
	dir1 := c.MkDir()
	dir2 := c.MkDir()
	file1 := "file1"
	file2 := "file2"
	c.Assert(os.WriteFile(filepath.Join(dir1, file1), nil, 0644), check.IsNil)
	c.Assert(os.WriteFile(filepath.Join(dir2, file2), nil, 0644), check.IsNil)

	osutil.SwapDirs(dir1, dir2)

	for _, path := range []string{filepath.Join(dir1, file2), filepath.Join(dir2, file1)} {
		exists, isreg, err := osutil.RegularFileExists(path)
		c.Check(exists, check.Equals, true)
		c.Check(isreg, check.Equals, true)
		c.Check(err, check.IsNil)
	}
}
