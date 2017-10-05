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
	"os"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
)

type entrySuite struct{}

var _ = Suite(&entrySuite{})

func (s *entrySuite) TestXSnapdMode(c *C) {
	// Mode has a default value.
	e := &mount.Entry{}
	mode, err := update.XSnapdMode(e)
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0755))

	// Mode is parsed from the x-snapd-mode= option.
	e = &mount.Entry{Options: []string{"x-snapd.mode=0700"}}
	mode, err = update.XSnapdMode(e)
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0700))

	// Empty value is invalid.
	e = &mount.Entry{Options: []string{"x-snapd.mode="}}
	_, err = update.XSnapdMode(e)
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from ""`)

	// As well as other bogus values.
	e = &mount.Entry{Options: []string{"x-snapd.mode=pasta"}}
	_, err = update.XSnapdMode(e)
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from "pasta"`)
}

func (s *entrySuite) TestXSnapdUid(c *C) {
	// User has a default value.
	e := &mount.Entry{}
	uid, err := update.XSnapdUid(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(0))

	// User is parsed from the x-snapd-user= option.
	daemonUid, err := osutil.FindUid("daemon")
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd.uid=daemon"}}
	uid, err = update.XSnapdUid(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, daemonUid)

	// Unknown user names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd.uid=.bogus"}}
	_, err = update.XSnapdUid(e)
	c.Assert(err, ErrorMatches, `cannot resolve user name ".bogus"`)
}

func (s *entrySuite) TestXSnapdGid(c *C) {
	// Group has a default value.
	e := &mount.Entry{}
	gid, err := update.XSnapdGid(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(0))

	// Group is parsed from the x-snapd-group= option.
	daemonGid, err := osutil.FindGid("daemon")
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd.gid=daemon"}}
	gid, err = update.XSnapdGid(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, daemonGid)

	// Unknown group names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd.gid=.bogus"}}
	_, err = update.XSnapdGid(e)
	c.Assert(err, ErrorMatches, `cannot resolve group name ".bogus"`)
}
