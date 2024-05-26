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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type userSuite struct{}

var _ = Suite(&userSuite{})

func (s *userSuite) TestIsPlausibleHomeHappy(c *C) {
	tmpHomeDir := c.MkDir()
	mylog.Check(update.IsPlausibleHome(tmpHomeDir))

}

func (s *userSuite) TestIsPlausibleHomeErrorPathEmpty(c *C) {
	mylog.Check(update.IsPlausibleHome(""))
	c.Assert(err, ErrorMatches, "cannot allow empty path")
}

func (s *userSuite) TestIsPlausibleHomeErrorPathNotClean(c *C) {
	mylog.Check(update.IsPlausibleHome("/PathNotClean/"))
	c.Assert(err, ErrorMatches, "cannot allow unclean path")
}

func (s *userSuite) TestIsPlausibleHomeErrorPathRelative(c *C) {
	mylog.Check(update.IsPlausibleHome("PathRelative"))
	c.Assert(err, ErrorMatches, "cannot allow relative path")
}

func (s *userSuite) TestIsPlausibleHomeErrorPathNotExist(c *C) {
	tmpHomeDir := c.MkDir() + "/user-does-not-exist"
	mylog.Check(update.IsPlausibleHome(tmpHomeDir))
	c.Assert(err, ErrorMatches, "no such file or directory")
}

func (s *userSuite) TestLock(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), IsNil)
	tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", tmpHomeDir)
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))


	// Locking is a no-op.
	unlock := mylog.Check2(upCtx.Lock())

	c.Check(unlock, NotNil)
	unlock()
}

func (s *userSuite) TestAssumptionsHomeValid(c *C) {
	tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", tmpHomeDir)
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))

	as := upCtx.Assumptions()
	c.Check(as.UnrestrictedPaths(), DeepEquals, []string{tmpHomeDir})
	c.Check(as.ModeForPath(tmpHomeDir+"/dir"), Equals, os.FileMode(0755))
}

func (s *userSuite) TestAssumptionsHomeInvalid(c *C) {
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", "")
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))

	as := upCtx.Assumptions()
	c.Check(as.UnrestrictedPaths(), IsNil)
}

func (s *userSuite) TestLoadDesiredProfileHomeRequiredAndValid(c *C) {
	// Mock directories but to simplify testing use the real value for XDG.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	dirs.XdgRuntimeDirBase = "/run/user"
	tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", tmpHomeDir)
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))


	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n" +
		"none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n"
	output := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n" +
		fmt.Sprintf("none %s/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=%s 0 0\n", tmpHomeDir, tmpHomeDir)

	// Write a desired user mount profile for snap "foo".
	path := update.DesiredUserProfilePath("foo")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(input), 0644), IsNil)

	// Ask the user profile update helper to read the desired profile.
	profile := mylog.Check2(upCtx.LoadDesiredProfile())

	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// Note that the profile read back contains expanded $XDG_RUNTIME_DIR and $HOME.
	c.Check(builder.String(), Equals, output)
}

func (s *userSuite) TestLoadDesiredProfileHomeNotRequiredAndMissing(c *C) {
	// Mock directories but to simplify testing use the real value for XDG.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	dirs.XdgRuntimeDirBase = "/run/user"
	// tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", "")
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))


	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n"
	output := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"

	// Write a desired user mount profile for snap "foo".
	path := update.DesiredUserProfilePath("foo")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(input), 0644), IsNil)

	// Ask the user profile update helper to read the desired profile.
	profile := mylog.Check2(upCtx.LoadDesiredProfile())

	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// Note that the profile read back contains expanded $XDG_RUNTIME_DIR
	c.Check(builder.String(), Equals, output)
}

func (s *userSuite) TestLoadDesiredProfileErrorHomeRequiredAndMissing(c *C) {
	// Mock directories but to simplify testing use the real value for XDG.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	dirs.XdgRuntimeDirBase = "/run/user"
	// tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", "")
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))


	input := "$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0\n" +
		"none $HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=$HOME 0 0\n"

	// Write a desired user mount profile for snap "foo".
	path := update.DesiredUserProfilePath("foo")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(input), 0644), IsNil)

	// Ask the user profile update helper to read the desired profile.
	profile := mylog.Check2(upCtx.LoadDesiredProfile())
	c.Assert(err, ErrorMatches, `cannot expand mount entry \(none \$HOME/.local/share none x-snapd.kind=ensure-dir,x-snapd.must-exist-dir=\$HOME 0 0\): cannot use invalid home directory \"\": cannot allow empty path`)
	c.Assert(profile, IsNil)
}

func (s *userSuite) TestLoadCurrentProfile(c *C) {
	// Mock directories.
	dirs.SetRootDir(c.MkDir())
	tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", tmpHomeDir)
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))


	// Write a current user mount profile for snap "foo".
	text := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	path := update.CurrentUserProfilePath(upCtx.InstanceName(), 1234)
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the user profile update helper to read the current profile.
	profile := mylog.Check2(upCtx.LoadCurrentProfile())

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
	tmpHomeDir := c.MkDir()
	restore := update.MockSnapConfineUserEnv("/run/user/1234/snap.snapname", tmpHomeDir)
	defer restore()
	upCtx := mylog.Check2(update.NewUserProfileUpdateContext("foo", false, 1234))


	// Prepare a mount profile to be saved.
	text := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	profile := mylog.Check2(osutil.LoadMountProfileText(text))


	// Write a fake current user mount profile for snap "foo".
	path := update.CurrentUserProfilePath("foo", 1234)
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(os.WriteFile(path, []byte("banana"), 0644), IsNil)
	mylog.

		// Ask the user profile update helper to write the current profile.
		Check(upCtx.SaveCurrentProfile(profile))


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
