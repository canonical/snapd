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

package main_test

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type commonSuite struct {
	dir string
	ctx *update.CommonProfileUpdateContext
}

var _ = Suite(&commonSuite{})

func (s *commonSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	s.ctx = update.NewCommonProfileUpdateContext("foo",
		filepath.Join(s.dir, "current.fstab"),
		filepath.Join(s.dir, "desired.fstab"))
}

func (s *commonSuite) TestInstanceName(c *C) {
	c.Check(s.ctx.InstanceName(), Equals, "foo")
}

func (s *commonSuite) TestLoadDesiredProfile(c *C) {
	ctx := s.ctx
	text := "tmpfs /tmp tmpfs defaults 0 0\n"

	// Ask the common profile update helper to read the desired profile.
	profile, err := ctx.LoadCurrentProfile()
	c.Assert(err, IsNil)

	// A profile that is not present on disk just reads as a valid empty profile.
	c.Check(profile.Entries, HasLen, 0)

	// Write a desired user mount profile for snap "foo".
	path := ctx.DesiredProfilePath()
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the common profile update helper to read the desired profile.
	profile, err = ctx.LoadDesiredProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// The profile is returned unchanged.
	c.Check(builder.String(), Equals, text)
}

func (s *commonSuite) TestLoadCurrentProfile(c *C) {
	ctx := s.ctx
	text := "tmpfs /tmp tmpfs defaults 0 0\n"

	// Ask the common profile update helper to read the current profile.
	profile, err := ctx.LoadCurrentProfile()
	c.Assert(err, IsNil)

	// A profile that is not present on disk just reads as a valid empty profile.
	c.Check(profile.Entries, HasLen, 0)

	// Write a current user mount profile for snap "foo".
	path := ctx.CurrentProfilePath()
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	c.Assert(ioutil.WriteFile(path, []byte(text), 0644), IsNil)

	// Ask the common profile update helper to read the current profile.
	profile, err = ctx.LoadCurrentProfile()
	c.Assert(err, IsNil)
	builder := &bytes.Buffer{}
	profile.WriteTo(builder)

	// The profile is returned unchanged.
	c.Check(builder.String(), Equals, text)
}

func (s *commonSuite) TestSaveCurrentProfile(c *C) {
	ctx := s.ctx
	text := "tmpfs /tmp tmpfs defaults 0 0\n"

	// Prepare a mount profile to be saved.
	profile, err := osutil.LoadMountProfileText(text)
	c.Assert(err, IsNil)

	// Prepare the directory for saving the profile.
	path := ctx.CurrentProfilePath()
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)

	// Ask the common profile update to write the current profile.
	c.Assert(ctx.SaveCurrentProfile(profile), IsNil)
	c.Check(path, testutil.FileEquals, text)
}
