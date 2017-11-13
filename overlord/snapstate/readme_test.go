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
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/testutil"
)

type readmeSuite struct{}

var _ = Suite(&readmeSuite{})

func (s *readmeSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *readmeSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *readmeSuite) TestSnapREADME(c *C) {
	f := filepath.Join(dirs.SnapMountDir, "README")

	// Missing file is created.
	c.Assert(snapstate.WriteSnapReadme(), IsNil)
	data, err := ioutil.ReadFile(f)
	c.Assert(err, IsNil)
	c.Check(string(data), testutil.Contains, "https://forum.snapcraft.io/t/the-snap-directory/2817")

	// Corrupted file is cured.
	err = ioutil.WriteFile(f, []byte("corrupted"), 0644)
	c.Assert(err, IsNil)
	c.Assert(snapstate.WriteSnapReadme(), IsNil)
	data, err = ioutil.ReadFile(f)
	c.Assert(err, IsNil)
	c.Check(string(data), testutil.Contains, "https://forum.snapcraft.io/t/the-snap-directory/2817")
}
