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

package osutil

import (
	"io/ioutil"
	"os"

	. "gopkg.in/check.v1"
)

type groupSuite struct {
}

var _ = Suite(&groupSuite{})

func (s *groupSuite) TestKnownGroup(c *C) {
	group, err := FindGroup(0)
	c.Check(err, IsNil)
	c.Check(group, Equals, "root")

	gid, err := FindGid("root")
	c.Check(err, IsNil)
	c.Check(gid, Equals, uint64(0))
}

func (s *groupSuite) TestBogusGroup(c *C) {
	group, err := FindGroup(99999)
	c.Check(err, Not(IsNil))
	c.Check(group, Equals, "")

	_, err = FindGid("nosuchgroup")
	c.Check(err, Not(IsNil))
}

func (s *groupSuite) TestSelfOwnedFile(c *C) {
	self, err := RealUser()
	c.Assert(err, IsNil)

	f, err := ioutil.TempFile("", "testownedfile")
	c.Assert(err, IsNil)
	name := f.Name()
	defer f.Close()
	defer os.Remove(name)

	group, err := FindGroupOwning(name)
	c.Assert(err, IsNil)
	c.Check(group.Gid, Equals, self.Gid)
}

func (s *groupSuite) TestNoOwnedFile(c *C) {
	group, err := FindGroupOwning("/tmp/filedoesnotexistbutwhy")
	c.Check(err, Not(IsNil))
	c.Check(group, IsNil)
}
