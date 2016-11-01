// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	"io/ioutil"
	"os"

	"github.com/snapcore/snapd/dirs"
	. "gopkg.in/check.v1"
)

func mockFastbootFile(c *C, newPath string, mode os.FileMode) {
	err := ioutil.WriteFile(newPath, []byte(""), mode)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) makeFakeFastbootConfig(c *C) {
	// these files just needs to exist
	f := &fastboot{}
	mockFastbootFile(c, f.ConfigFile(), 0644)
}

func (s *PartitionTestSuite) TestNewFastbootNoFastbootReturnsNil(c *C) {
	dirs.GlobalRootDir = "/something/not/there"

	f := newFastboot()
	c.Assert(f, IsNil)
}

func (s *PartitionTestSuite) TestNewFastboot(c *C) {
	s.makeFakeFastbootConfig(c)

	f := newFastboot()
	c.Assert(f, NotNil)
	c.Assert(f, FitsTypeOf, &fastboot{})
}

func (s *PartitionTestSuite) TestSetGetBootVar(c *C) {
	f := newFastboot()
	bootVars := map[string]string{}
	bootVars["snap_mode"] = "try"
	f.SetBootVars(bootVars)

	v, err := f.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "try")
}
