// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
)

type userSuite struct{}

var _ = Suite(&userSuite{})

func (s *userSuite) TestAssumptions(c *C) {
	ctx := update.NewUserProfileUpdateContext("foo", 1234)
	as := ctx.Assumptions()
	c.Check(as.UnrestrictedPaths(), IsNil)
}

func (s *userSuite) TestLoadDesiredProfile(c *C) {
	// Mock directories but to simplify testing use the real value for XDG.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	dirs.XdgRuntimeDirBase = "/run/user"

	ctx := update.NewUserProfileUpdateContext("foo", 1234)

	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n"
	output := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"

	// Write a desired user mount profile for snap "foo".
	path := update.DesiredUserProfilePath("foo")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(input), 0644), IsNil)

	// Ask the user profile update helper to read the desired profile.
	profile, err := ctx.LoadDesiredProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// Note that the profile read back contains expanded $XDG_RUNTIME_DIR.
	c.Check(builder.String(), Equals, output)
}

func (s *userSuite) TestDesiredUserProfilePath(c *C) {
	c.Check(update.DesiredUserProfilePath("foo"), Equals, "/var/lib/snapd/mount/snap.foo.user-fstab")
}
