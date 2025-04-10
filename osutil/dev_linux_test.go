// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

func (ts *StatTestSuite) TestDeviceMajorMinor(c *C) {
	dev := osutil.Makedev(11, 12)
	c.Check(osutil.Major(dev), Equals, uint64(11))
	c.Check(osutil.Minor(dev), Equals, uint64(12))
	c.Check(osutil.Makedev(osutil.Major(dev), osutil.Minor(dev)), Equals, dev)
}
