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
	"github.com/snapcore/snapd/release"
)

func (s *SnapSuite) TestPathsUbuntu(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()
	defer dirs.SetRootDir("/")

	dirs.SetRootDir("/")
	_, err := snap.Parser().ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/snap\n"+
		"SNAPD_BIN=/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/lib/snapd\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestPathsFedora(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "fedora"})
	defer restore()
	defer dirs.SetRootDir("/")

	dirs.SetRootDir("/")
	_, err := snap.Parser().ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/var/lib/snapd/snap\n"+
		"SNAPD_BIN=/var/lib/snapd/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/libexec/snapd\n")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *SnapSuite) TestPathsArch(c *C) {
	restore := release.MockReleaseInfo(&release.OS{IDLike: []string{"arch"}})
	defer restore()
	defer dirs.SetRootDir("/")

	dirs.SetRootDir("/")
	_, err := snap.Parser().ParseArgs([]string{"debug", "paths"})
	c.Assert(err, IsNil)
	c.Assert(s.Stdout(), Equals, ""+
		"SNAPD_MOUNT=/var/lib/snapd/snap\n"+
		"SNAPD_BIN=/var/lib/snapd/snap/bin\n"+
		"SNAPD_LIBEXEC=/usr/lib/snapd\n")
	c.Assert(s.Stderr(), Equals, "")
}
