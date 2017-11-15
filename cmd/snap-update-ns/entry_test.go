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
	"fmt"
	"math"
	"os"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type entrySuite struct{}

var _ = Suite(&entrySuite{})

func (s *entrySuite) TestXSnapdMode(c *C) {
	// Mode has a default value.
	e := &mount.Entry{}
	mode, err := update.XSnapdMode(e)
	c.Assert(err, IsNil)
	c.Assert(mode, Equals, os.FileMode(0755))

	// Mode is parsed from the x-snapd.mode= option.
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

func (s *entrySuite) TestXSnapdUID(c *C) {
	// User has a default value.
	e := &mount.Entry{}
	uid, err := update.XSnapdUID(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(0))

	// User is parsed from the x-snapd.uid= option.
	nobodyUID, err := osutil.FindUid("nobody")
	c.Assert(err, IsNil)
	e = &mount.Entry{Options: []string{"x-snapd.uid=nobody"}}
	uid, err = update.XSnapdUID(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, nobodyUID)

	// Numeric names are used as-is.
	e = &mount.Entry{Options: []string{"x-snapd.uid=123"}}
	uid, err = update.XSnapdUID(e)
	c.Assert(err, IsNil)
	c.Assert(uid, Equals, uint64(123))

	// Unknown user names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd.uid=bogus"}}
	uid, err = update.XSnapdUID(e)
	c.Assert(err, ErrorMatches, `cannot resolve user name "bogus"`)
	c.Assert(uid, Equals, uint64(math.MaxUint64))

	// And even valid values with trailing garbage.
	e = &mount.Entry{Options: []string{"x-snapd.uid=0bogus"}}
	uid, err = update.XSnapdUID(e)
	c.Assert(err, ErrorMatches, `cannot parse user name "0bogus"`)
	c.Assert(uid, Equals, uint64(math.MaxUint64))
}

func (s *entrySuite) TestXSnapdGID(c *C) {
	// Group has a default value.
	e := &mount.Entry{}
	gid, err := update.XSnapdGID(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(0))

	// Group is parsed from the x-snapd-group= option.
	var nogroup string
	var nogroupGID uint64
	// try to cover differences between distributions and find a suitable
	// 'nogroup' like group, eg. Ubuntu uses 'nogroup' while Arch uses
	// 'nobody'
	for _, grp := range []string{"nogroup", "nobody"} {
		nogroup = grp
		if gid, err := osutil.FindGid(grp); err == nil {
			nogroup = grp
			nogroupGID = gid
			break
		}
	}
	c.Assert([]string{"nogroup", "nobody"}, testutil.Contains, nogroup)

	e = &mount.Entry{
		Options: []string{fmt.Sprintf("x-snapd.gid=%s", nogroup)},
	}
	gid, err = update.XSnapdGID(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, nogroupGID)

	// Numeric names are used as-is.
	e = &mount.Entry{Options: []string{"x-snapd.gid=456"}}
	gid, err = update.XSnapdGID(e)
	c.Assert(err, IsNil)
	c.Assert(gid, Equals, uint64(456))

	// Unknown group names are invalid.
	e = &mount.Entry{Options: []string{"x-snapd.gid=bogus"}}
	gid, err = update.XSnapdGID(e)
	c.Assert(err, ErrorMatches, `cannot resolve group name "bogus"`)
	c.Assert(gid, Equals, uint64(math.MaxUint64))

	// And even valid values with trailing garbage.
	e = &mount.Entry{Options: []string{"x-snapd.gid=0bogus"}}
	gid, err = update.XSnapdGID(e)
	c.Assert(err, ErrorMatches, `cannot parse group name "0bogus"`)
	c.Assert(gid, Equals, uint64(math.MaxUint64))
}

func (s *entrySuite) TestXSnapdEntryID(c *C) {
	// Entry ID is optional.
	e := &mount.Entry{}
	c.Assert(update.XSnapdEntryID(e), Equals, "")

	// Entry ID is parsed from the x-snapd.id= option.
	e = &mount.Entry{Options: []string{"x-snapd.id=foo"}}
	c.Assert(update.XSnapdEntryID(e), Equals, "foo")
}

func (s *entrySuite) TestXSnapdParentID(c *C) {
	// Parent entry ID is optional.
	e := &mount.Entry{}
	c.Assert(update.XSnapdParentID(e), Equals, "")

	// Parent entry ID is parsed from the x-snapd.parent-id= option.
	e = &mount.Entry{Options: []string{"x-snap.id=foo", "x-snapd.parent-id=bar"}}
	c.Assert(update.XSnapdParentID(e), Equals, "bar")
}

func (s *entrySuite) TestXSnapdSynthetic(c *C) {
	// Entries are not synthetic unless tagged as such.
	e := &mount.Entry{}
	c.Assert(update.XSnapdSynthetic(e), Equals, false)

	// Tagging is done with x-snapd.synthetic option.
	e = &mount.Entry{Options: []string{"x-snapd.synthetic"}}
	c.Assert(update.XSnapdSynthetic(e), Equals, true)
}
