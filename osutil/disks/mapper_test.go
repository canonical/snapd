// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd

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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

type mapperSuite struct{}

var _ = Suite(&mapperSuite{})

func (ts *mapperSuite) TestCreateLinearSizeOffsetErr(c *C) {
	for _, tc := range []struct {
		offset, size uint64
		expectedErr  string
	}{
		{123, 2048, `cannot create mapper "mapper-name" on /dev/sda1: offset 123 must be aligned to 512 bytes`},
		{512, 1111, `cannot create mapper "mapper-name" on /dev/sda1: size 1111 must be aligned to 512 bytes`},
		{512, 512, `cannot create mapper "mapper-name" on /dev/sda1: size 512 must be larger than the offset 512`},
	} {
		_ := mylog.Check2(disks.CreateLinearMapperDevice("/dev/sda1", "mapper-name", "", tc.offset, tc.size))
		c.Check(err, ErrorMatches, tc.expectedErr)
	}
}

func (ts *mapperSuite) TestCreateLinearMapperErr(c *C) {
	mockCmd := testutil.MockCommand(c, "dmsetup", "echo fail; exit 1")
	defer mockCmd.Restore()

	_ := mylog.Check2(disks.CreateLinearMapperDevice("/dev/sda1", "mapper-name", "", 512, 1024))
	c.Check(err, ErrorMatches, `cannot create mapper "mapper-name" on /dev/sda1: fail`)
}

func (ts *mapperSuite) TestCreateLinearMapperHappyNoUUID(c *C) {
	mockCmd := testutil.MockCommand(c, "dmsetup", "")
	defer mockCmd.Restore()

	uuid := ""
	mapperDevice := mylog.Check2(disks.CreateLinearMapperDevice("/dev/sda1", "mapper-name", uuid, 512, 2048))


	c.Check(mapperDevice, Equals, "/dev/mapper/mapper-name")
	c.Check(mockCmd.Calls(), DeepEquals, [][]string{
		{"dmsetup", "create", "mapper-name", "--table", "0 4 linear /dev/sda1 1"},
	})
}

func (ts *mapperSuite) TestCreateLinearMapperHappyWithUUID(c *C) {
	mockCmd := testutil.MockCommand(c, "dmsetup", "")
	defer mockCmd.Restore()

	mapperDevice := mylog.Check2(disks.CreateLinearMapperDevice("/dev/sda1", "mapper-name", "some-uuid", 512, 2048))


	c.Check(mapperDevice, Equals, "/dev/mapper/mapper-name")
	c.Check(mockCmd.Calls(), DeepEquals, [][]string{
		{"dmsetup", "create", "mapper-name", "--uuid", "some-uuid", "--table", "0 4 linear /dev/sda1 1"},
	})
}
