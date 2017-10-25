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
	"math"
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

	// And even valid values with trailing garbage.
	e = &mount.Entry{Options: []string{"x-snapd.mode=0700pasta"}}
	mode, err = update.XSnapdMode(e)
	c.Assert(err, ErrorMatches, `cannot parse octal file mode from "0700pasta"`)
	c.Assert(mode, Equals, os.FileMode(0))
}

func (s *entrySuite) TestXSnapdUid(c *C) {
	// User has a default value.
	e := &mount.Entry{}
	uid, err := update.XSnapdUid(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(0))

	// User is parsed from the x-snapd-user= option.
	nobodyUid, err := osutil.FindUid("nobody")
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd.uid=nobody"}}
	uid, err = update.XSnapdUid(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, nobodyUid)

	// Numeric names are used as-is.
	e = &mount.Entry{Options: []string{"x-snapd.uid=123"}}
	uid, err = update.XSnapdUid(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(123))

	// Unknown user names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd.uid=bogus"}}
	uid, err = update.XSnapdUid(e)
	c.Assert(err, ErrorMatches, `cannot resolve user name "bogus"`)
	c.Assert(uid, Equals, uint64(math.MaxUint64))

	// And even valid values with trailing garbage.
	e = &mount.Entry{Options: []string{"x-snapd.uid=0bogus"}}
	uid, err = update.XSnapdUid(e)
	c.Assert(err, ErrorMatches, `cannot parse user name "0bogus"`)
	c.Assert(uid, Equals, uint64(math.MaxUint64))
}

func (s *entrySuite) TestXSnapdGid(c *C) {
	// Group has a default value.
	e := &mount.Entry{}
	gid, err := update.XSnapdGid(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(0))

	// Group is parsed from the x-snapd-group= option.
	nogroupGid, err := osutil.FindGid("nogroup")
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd.gid=nogroup"}}
	gid, err = update.XSnapdGid(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, nogroupGid)

	// Numeric names are used as-is.
	e = &mount.Entry{Options: []string{"x-snapd.gid=456"}}
	gid, err = update.XSnapdGid(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(456))

	// Unknown group names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd.gid=bogus"}}
	gid, err = update.XSnapdGid(e)
	c.Assert(err, ErrorMatches, `cannot resolve group name "bogus"`)
	c.Assert(gid, Equals, uint64(math.MaxUint64))

	// And even valid values with trailing garbage.
	e = &mount.Entry{Options: []string{"x-snapd.gid=0bogus"}}
	gid, err = update.XSnapdGid(e)
	c.Assert(err, ErrorMatches, `cannot parse group name "0bogus"`)
	c.Assert(gid, Equals, uint64(math.MaxUint64))
}
