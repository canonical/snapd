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

package osutil_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

func (s *fshelpersSuite) TestDeviceMajorAndMinor(c *C) {
	// Test with /dev/null (major: 1, minor: 3)
	major, minor, err := osutil.DeviceMajorAndMinor("/dev/null")
	c.Check(err, IsNil)
	c.Check(major, Equals, uint32(1))
	c.Check(minor, Equals, uint32(3))
}

func (s *fshelpersSuite) TestDeviceMajorAndMinorNotExist(c *C) {
	_, _, err := osutil.DeviceMajorAndMinor("/dev/doesnotexist")
	c.Assert(err, DeepEquals, os.ErrNotExist)
}

func (s *fshelpersSuite) TestDeviceMajorAndMinorNotDevice(c *C) {
	name := filepath.Join(c.MkDir(), "notadevice")
	err := os.WriteFile(name, nil, 0644)
	c.Assert(err, IsNil)

	_, _, err = osutil.DeviceMajorAndMinor(name)
	c.Assert(err, ErrorMatches, "not a device")
}
