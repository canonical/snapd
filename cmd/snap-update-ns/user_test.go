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
	"github.com/snapcore/snapd/features"
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
	feature := features.PerUserMountNamespace

	// When the feature is disabled locking is a no-op.
	c.Check(feature.IsEnabled(), Equals, false)

	// Locking is a no-op.
	unlock, err := upCtx.Lock()
	c.Assert(err, IsNil)
	c.Check(unlock, NotNil)
	unlock()

	// When the feature is enabled a lock is acquired and cgroup is frozen.
	c.Assert(ioutil.WriteFile(feature.ControlFile(), nil, 0644), IsNil)
	c.Check(feature.IsEnabled(), Equals, true)
	unlock, err = upCtx.Lock()
	c.Assert(err, IsNil)
	c.Check(unlock, NotNil)
	unlock()
}

func (s *userSuite) TestAssumptions(c *C) {
	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)
	as := upCtx.Assumptions()
	c.Check(as.UnrestrictedPaths(), DeepEquals, []string{"/tmp", "/run/user/1234"})
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
	// Mock directories and create directory for features and runtime mount profiles.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), IsNil)
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)

	upCtx := update.NewUserProfileUpdateContext("foo", false, 1234)

	// Prepare a mount profile to be saved.
	text := "/run/user/1234/doc/by-app/snap.foo /run/user/1234/doc none bind,rw 0 0\n"
	profile, err := osutil.LoadMountProfileText(text)
	c.Assert(err, IsNil)

	feature := features.PerUserMountNamespace

	// Write a fake current user mount profile for snap "foo".
	path := update.CurrentUserProfilePath(upCtx.InstanceName(), upCtx.UID())
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte("banana"), 0644), IsNil)

	// Ask the user profile update to write the current profile.
	// Because the per-user-mount-namespace feature is disabled the profile is not persisted.
	c.Check(feature.IsEnabled(), Equals, false)
	c.Assert(upCtx.SaveCurrentProfile(profile), IsNil)
	c.Check(path, testutil.FileEquals, "banana")

	// Ask the user profile update to write the current profile, again.
	// When the per-user-mount-namespace feature is enabled the profile is saved.
	c.Assert(ioutil.WriteFile(feature.ControlFile(), nil, 0644), IsNil)
	c.Check(feature.IsEnabled(), Equals, true)
	c.Assert(upCtx.SaveCurrentProfile(profile), IsNil)
	c.Check(update.CurrentUserProfilePath(upCtx.InstanceName(), upCtx.UID()), testutil.FileEquals, text)
}

func (s *userSuite) TestDesiredUserProfilePath(c *C) {
	c.Check(update.DesiredUserProfilePath("foo"), Equals, "/var/lib/snapd/mount/snap.foo.user-fstab")
}

func (s *userSuite) TestCurrentUserProfilePath(c *C) {
	c.Check(update.CurrentUserProfilePath("foo", 12345), Equals, "/run/snapd/ns/snap.foo.12345.user-fstab")
}
