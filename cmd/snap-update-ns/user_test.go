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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type userSuite struct{}

var _ = Suite(&userSuite{})

func (s *userSuite) TestLock(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), IsNil)

	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)

	// Locking is a no-op.
	unlock, err := upCtx.Lock()
	c.Assert(err, IsNil)
	c.Check(unlock, NotNil)
	unlock()
}

func (s *userSuite) TestAssumptions(c *C) {
	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)
	as := upCtx.Assumptions()
	c.Check(as.UnrestrictedPaths(), IsNil)
}

func (s *userSuite) TestLoadDesiredProfile(c *C) {
	// Mock directories but to simplify testing use the real value for XDG.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	dirs.XdgRuntimeDirBase = "/run/user"

	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)

	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n"
	output := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"

	// Write a desired user mount profile for snap "foo".
	path := update.DesiredUserProfilePath("foo")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(input), 0644), IsNil)

	// Ask the user profile update helper to read the desired profile.
	profile, err := upCtx.LoadDesiredProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// Note that the profile read back contains expanded $XDG_RUNTIME_DIR.
	c.Check(builder.String(), Equals, output)
}

func (s *userSuite) TestLoadCurrentProfile(c *C) {
	// Mock directories.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)

	// Write a current user mount profile for snap "foo".
	text := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	path := update.CurrentUserProfilePath(upCtx.InstanceName(), upCtx.UID())
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the user profile update helper to read the current profile.
	profile, err := upCtx.LoadCurrentProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// Note that the profile is empty.
	// Currently user profiles are not persisted so the presence of a profile on-disk is ignored.
	c.Check(builder.String(), Equals, "")
}

func (s *userSuite) TestSaveCurrentProfile(c *C) {
	// Mock directories and create directory runtime mount profiles.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)

	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)

	// Prepare a mount profile to be saved.
	text := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	profile, err := osutil.LoadMountProfileText(text)
	c.Assert(err, IsNil)

	// Write a fake current user mount profile for snap "foo".
	path := update.CurrentUserProfilePath("foo", 1234)
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte("banana"), 0644), IsNil)

	// Ask the user profile update helper to write the current profile.
	err = upCtx.SaveCurrentProfile(profile)
	c.Assert(err, IsNil)

	// Note that the profile was not modified.
	// Currently user profiles are not persisted.
	c.Check(path, testutil.FileEquals, "banana")
}

func (s *userSuite) TestDesiredUserProfilePath(c *C) {
	c.Check(update.DesiredUserProfilePath("foo"), Equals, "/var/lib/snapd/mount/snap.foo.user-fstab")
}

func (s *userSuite) TestCurrentUserProfilePath(c *C) {
	c.Check(update.CurrentUserProfilePath("foo", 12345), Equals, "/run/snapd/ns/snap.foo.12345.user-fstab")
}
