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
package volmgr_test

import (
	"io/ioutil"
	"path"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-install/volmgr"
	"github.com/snapcore/snapd/testutil"
)

func (s *volmgrTestSuite) TestWipe(c *C) {
	data := []byte("12345678")
	temp := path.Join(c.MkDir(), "myfile")
	err := ioutil.WriteFile(temp, data, 0600)
	c.Assert(err, IsNil)
	c.Assert(temp, testutil.FilePresent)
	err = volmgr.Wipe(temp)
	c.Assert(err, IsNil)
	c.Assert(temp, testutil.FileAbsent)
}

func (s *volmgrTestSuite) TestCreateKey(c *C) {
	data, err := volmgr.CreateKey(16)
	c.Assert(err, IsNil)
	c.Assert(data, Not(DeepEquals), make([]byte, 16))
}
