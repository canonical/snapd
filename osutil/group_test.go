// -*- Mode: Go; indent-tabs-mode: t -*-

// +build cgo

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

package osutil_test

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type groupSuite struct{}

var _ = Suite(&groupSuite{})

func (s *groupSuite) TestKnownGroup(c *C) {
	group, err := osutil.FindGroup(0)
	c.Check(err, IsNil)
	c.Check(group, Equals, "root")

	gid, err := osutil.FindGid("root")
	c.Check(err, IsNil)
	c.Check(gid, Equals, uint64(0))
}

func (s *groupSuite) TestBogusGroup(c *C) {
	group, err := osutil.FindGroup(99999)
	c.Check(err, Not(IsNil))
	c.Check(group, Equals, "")

	_, err = osutil.FindGid("nosuchgroup")
	c.Check(err, Not(IsNil))
}

func (s *groupSuite) TestSelfOwnedFile(c *C) {
	self, err := osutil.RealUser()
	c.Assert(err, IsNil)

	f, err := ioutil.TempFile("", "testownedfile")
	c.Assert(err, IsNil)
	name := f.Name()
	defer f.Close()
	defer os.Remove(name)

	group, err := osutil.FindGroupOwning(name)
	c.Assert(err, IsNil)
	c.Check(group.Gid, Equals, self.Gid)
}

func (s *groupSuite) TestNoOwnedFile(c *C) {
	group, err := osutil.FindGroupOwning("/tmp/filedoesnotexistbutwhy")
	c.Check(err, Not(IsNil))
	c.Check(group, IsNil)
}
