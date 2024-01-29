// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package snapstate_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type readmeSuite struct{}

var _ = Suite(&readmeSuite{})

func (s *readmeSuite) TestSnapReadmeFedora(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "fedora"})
	defer restore()

	dirs.SetRootDir("/")
	defer dirs.SetRootDir("/")

	c.Assert(snapstate.SnapReadme(), testutil.Contains, "/var/lib/snapd/snap/bin                   - Symlinks to snap applications.\n")
}

func (s *readmeSuite) TestSnapReadmeUbuntu(c *C) {
	restore := release.MockReleaseInfo(&release.OS{ID: "ubuntu"})
	defer restore()

	dirs.SetRootDir("/")
	defer dirs.SetRootDir("/")

	c.Assert(snapstate.SnapReadme(), testutil.Contains, "/snap/bin                   - Symlinks to snap applications.\n")
}

func (s *readmeSuite) TestWriteSnapREADME(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("")

	f := filepath.Join(dirs.SnapMountDir, "README")

	// Missing file is created.
	c.Assert(snapstate.WriteSnapReadme(), IsNil)
	c.Check(f, testutil.FileContains, "https://forum.snapcraft.io/t/the-snap-directory/2817")

	// Corrupted file is cured.
	err := os.Remove(f)
	c.Assert(err, IsNil)
	err = os.WriteFile(f, []byte("corrupted"), 0644)
	c.Assert(err, IsNil)
	c.Assert(snapstate.WriteSnapReadme(), IsNil)
	c.Check(f, testutil.FileContains, "https://forum.snapcraft.io/t/the-snap-directory/2817")
}
