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

type systemSuite struct{}

var _ = Suite(&systemSuite{})

func (s *systemSuite) TestLoadDesiredProfile(c *C) {
	// Mock directories.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	ctx := update.NewSystemProfileUpdateContext("foo")
	text := "/snap/foo/42/dir /snap/bar/13/dir none bind,rw 0 0\n"

	// Write a desired system mount profile for snap "foo".
	path := update.DesiredSystemProfilePath(ctx.InstanceName())
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the system profile update helper to read the desired profile.
	profile, err := ctx.LoadDesiredProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	c.Check(builder.String(), Equals, text)
}

func (s *systemSuite) TestLoadCurrentProfile(c *C) {
	// Mock directories.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	ctx := update.NewSystemProfileUpdateContext("foo")
	text := "/snap/foo/42/dir /snap/bar/13/dir none bind,rw 0 0\n"

	// Write a current system mount profile for snap "foo".
	path := update.CurrentSystemProfilePath(ctx.InstanceName())
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the system profile update helper to read the current profile.
	profile, err := ctx.LoadCurrentProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// The profile is returned unchanged.
	c.Check(builder.String(), Equals, text)
}

func (s *systemSuite) TestSaveCurrentProfile(c *C) {
	// Mock directories and create directory for runtime mount profiles.
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")
	c.Assert(os.MkdirAll(dirs.SnapRunNsDir, 0755), IsNil)

	ctx := update.NewSystemProfileUpdateContext("foo")
	text := "/snap/foo/42/dir /snap/bar/13/dir none bind,rw 0 0\n"

	// Prepare a mount profile to be saved.
	profile, err := osutil.LoadMountProfileText(text)
	c.Assert(err, IsNil)

	// Ask the system profile update to write the current profile.
	c.Assert(ctx.SaveCurrentProfile(profile), IsNil)
	c.Check(update.CurrentSystemProfilePath(ctx.InstanceName()), testutil.FileEquals, text)
}

func (s *systemSuite) TestDesiredSystemProfilePath(c *C) {
	c.Check(update.DesiredSystemProfilePath("foo"), Equals, "/var/lib/snapd/mount/snap.foo.fstab")
}

func (s *systemSuite) TestCurrentSystemProfilePath(c *C) {
	c.Check(update.CurrentSystemProfilePath("foo"), Equals, "/run/snapd/ns/snap.foo.fstab")
}
