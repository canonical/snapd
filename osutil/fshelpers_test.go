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

package osutil

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"

	. "gopkg.in/check.v1"
)

type groupFindGidOwningSuite struct{}

var _ = Suite(&groupFindGidOwningSuite{})

func (s *groupFindGidOwningSuite) TestSelfOwnedFile(c *C) {
	name := filepath.Join(c.MkDir(), "testownedfile")
	err := ioutil.WriteFile(name, nil, 0644)
	c.Assert(err, IsNil)

	gid, err := FindGidOwning(name)
	c.Check(err, IsNil)

	self, err := RealUser()
	c.Assert(err, IsNil)
	c.Check(strconv.FormatUint(gid, 10), Equals, self.Gid)
}

func (s *groupFindGidOwningSuite) TestNoOwnedFile(c *C) {
	_, err := FindGidOwning("/tmp/filedoesnotexistbutwhy")
	c.Assert(err, DeepEquals, os.ErrNotExist)
}
