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
package partition_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-recovery/partition"
	"github.com/snapcore/snapd/testutil"
)

func (s *partitionTestSuite) TestMkfsUnhappy(c *C) {
	err := partition.MakeFilesystem("/dev/node", "some-label", "unsupported-filesystem-type")
	c.Assert(err, ErrorMatches, `cannot create unsupported filesystem "unsupported-filesystem-type"`)
}

func (s *partitionTestSuite) TestMkfsVfat(c *C) {
	mockMkfsVfat := testutil.MockCommand(c, "mkfs.vfat", "")
	defer mockMkfsVfat.Restore()

	err := partition.MakeFilesystem("/dev/node", "some-label", "vfat")
	c.Assert(err, IsNil)
	c.Assert(mockMkfsVfat.Calls(), DeepEquals, [][]string{
		{"mkfs.vfat", "-n", "some-label", "/dev/node"},
	})
}

func (s *partitionTestSuite) TestMkfsExt4(c *C) {
	mockMkfs := testutil.MockCommand(c, "mke2fs", "")
	defer mockMkfs.Restore()

	err := partition.MakeFilesystem("/dev/node", "some-label", "ext4")
	c.Assert(err, IsNil)
	c.Assert(mockMkfs.Calls(), DeepEquals, [][]string{
		{"mke2fs", "-t", "ext4", "-L", "some-label", "/dev/node"},
	})
}
