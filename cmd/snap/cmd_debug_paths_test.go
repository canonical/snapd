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

package main_test

import (
	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/dirs/dirstest"
	"github.com/snapcore/snapd/release"
)

func (s *SnapSuite) TestPathsUbuntu(c *C) {
	dirstest.MustMockCanonicalSnapMountDir(dirs.GlobalRootDir)
	dirs.SetRootDir(dirs.GlobalRootDir)
	defer dirs.SetRootDir("/")

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/snap\n"+
		"SNAPD_BIN=/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/lib/snapd\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestPathsLibexecDir(c *C) {
	dirstest.MustMockAltSnapMountDir(dirs.GlobalRootDir)
	dirstest.MustMockAltLibExecDir(dirs.GlobalRootDir)
	dirs.SetRootDir(dirs.GlobalRootDir)
	defer dirs.SetRootDir("/")

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/var/lib/snapd/snap\n"+
		"SNAPD_BIN=/var/lib/snapd/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/libexec/snapd\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestPathsAltMountDir(c *C) {
	defer dirs.SetRootDir("/")

	dirstest.MustMockAltSnapMountDir(dirs.GlobalRootDir)
	dirs.SetRootDir(dirs.GlobalRootDir)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/var/lib/snapd/snap\n"+
		"SNAPD_BIN=/var/lib/snapd/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/lib/snapd\n")
	c.Assert(s.Stderr(), Equals, "")

	s.ResetStdStreams()
}

func (s *SnapSuite) TestPathsMyDistro(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "my-distro"})
	defer restore()
	defer dirs.SetRootDir("/")

	d := c.MkDir()
	dirstest.MustMockAltSnapMountDir(d)
	dirs.SetRootDir(d)

	_, err := snap.Parser(snap.Client()).ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	// since it's a custom distro, the test overrides root directory so the resulting paths
	// include an explicit path
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/var/lib/snapd/snap\n"+
		"SNAPD_BIN=/var/lib/snapd/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/lib/snapd\n")
	c.Assert(s.Stderr(), Equals, "")
}
