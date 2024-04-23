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

package udev_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/udev"
)

type helperVersionSuite struct {
}

var _ = Suite(&helperVersionSuite{})

func (s *helperVersionSuite) TestOld(c *C) {
	top := c.MkDir()
	dirs.SetRootDir(top)

	c.Assert(os.MkdirAll(dirs.DistroLibExecDir, 0755), IsNil)
	infoFile := filepath.Join(dirs.DistroLibExecDir, "info")
	c.Assert(os.WriteFile(infoFile, []byte(`VERSION=2.61.9`), 0644), IsNil)

	defer udev.MockUseOldCallReset()()
	c.Check(udev.UseOldCall(), Equals, true)
}

func (s *helperVersionSuite) TestNew(c *C) {
	top := c.MkDir()
	dirs.SetRootDir(top)

	c.Assert(os.MkdirAll(dirs.DistroLibExecDir, 0755), IsNil)
	infoFile := filepath.Join(dirs.DistroLibExecDir, "info")
	c.Assert(os.WriteFile(infoFile, []byte(`VERSION=2.62`), 0644), IsNil)

	defer udev.MockUseOldCallReset()()
	c.Check(udev.UseOldCall(), Equals, false)
}

func (s *helperVersionSuite) TestGarbage(c *C) {
	top := c.MkDir()
	dirs.SetRootDir(top)

	c.Assert(os.MkdirAll(dirs.DistroLibExecDir, 0755), IsNil)
	infoFile := filepath.Join(dirs.DistroLibExecDir, "info")
	c.Assert(os.WriteFile(infoFile, []byte(`garbage`), 0644), IsNil)

	defer udev.MockUseOldCallReset()()
	c.Check(udev.UseOldCall(), Equals, false)
}
