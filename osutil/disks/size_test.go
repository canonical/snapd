// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package disks_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

type sizeSuite struct{}

var _ = Suite(&sizeSuite{})

func (ts *sizeSuite) TestSizeHappy(c *C) {
	mockBlockdev := testutil.MockCommand(c, "blockdev", "echo 1024")
	defer mockBlockdev.Restore()

	size, err := disks.Size("/dev/some-device")
	c.Assert(err, IsNil)
	c.Check(size, Equals, uint64(1024*512))

	c.Check(mockBlockdev.Calls(), DeepEquals, [][]string{
		{"blockdev", "--getsz", "/dev/some-device"},
	})
}

func (ts *sizeSuite) TestSizeErrFailure(c *C) {
	mockBlockdev := testutil.MockCommand(c, "blockdev", "echo some-error-message; exit 1")
	defer mockBlockdev.Restore()

	_, err := disks.Size("/dev/some-device")
	c.Check(err, ErrorMatches, "cannot get disk size: some-error-message")
}

func (ts *sizeSuite) TestSizeErrSizeParsing(c *C) {
	mockBlockdev := testutil.MockCommand(c, "blockdev", "echo NaN")
	defer mockBlockdev.Restore()

	_, err := disks.Size("/dev/some-device")
	c.Check(err, ErrorMatches, `cannot parse disk size output: strconv.ParseUint: parsing "NaN": invalid syntax`)
}
