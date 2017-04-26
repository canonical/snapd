// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

func mockAndroidbootFile(c *C, newPath string, mode os.FileMode) {
	err := ioutil.WriteFile(newPath, []byte(""), mode)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) makeFakeAndroidbootConfig(c *C) {
	// these files just needs to exist
	a := &androidboot{}
	mockAndroidbootFile(c, a.ConfigFile(), 0644)
}

func (s *PartitionTestSuite) TestNewAndroidbootNoAndroidbootReturnsNil(c *C) {
	dirs.GlobalRootDir = "/something/not/there"

	a := newAndroidboot()
	c.Assert(a, IsNil)
}

func (s *PartitionTestSuite) TestNewAndroidboot(c *C) {
	s.makeFakeAndroidbootConfig(c)

	a := newAndroidboot()
	c.Assert(a, NotNil)
	c.Assert(a, FitsTypeOf, &androidboot{})
}

func (s *PartitionTestSuite) TestSetGetBootVar(c *C) {
	a := newAndroidboot()
	bootVars := map[string]string{}
	bootVars["snap_mode"] = "try"
	a.SetBootVars(bootVars)

	v, err := f.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, "try")
}
