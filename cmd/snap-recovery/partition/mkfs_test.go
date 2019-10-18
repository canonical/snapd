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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

type mkfsSuite struct {
	testutil.BaseTest

	mockMkfsVfat *testutil.MockCmd
	mockMkfsExt4 *testutil.MockCmd
}

var _ = Suite(&mkfsSuite{})

func (s *mkfsSuite) SetUpTest(c *C) {
	s.mockMkfsVfat = testutil.MockCommand(c, "mkfs.vfat", "")
	s.AddCleanup(s.mockMkfsVfat.Restore)
	s.mockMkfsExt4 = testutil.MockCommand(c, "mkfs.ext4", "")
	s.AddCleanup(s.mockMkfsExt4.Restore)
}

func (s *mkfsSuite) TestMkfsUnhappy(c *C) {
	err := partition.Mkfs("/dev/node", "some-label", "unsupported-filesystem-type")
	c.Assert(err, ErrorMatches, `cannot create unsupported filesystem "unsupported-filesystem-type"`)
}

func (s *mkfsSuite) TestMkfsVfat(c *C) {
	err := partition.Mkfs("/dev/node", "some-label", "vfat")
	c.Assert(err, IsNil)
	// details are already tested in the gadget package
	c.Assert(s.mockMkfsVfat.Calls(), HasLen, 1)
}

func (s *mkfsSuite) TestMkfsExt4(c *C) {
	err := partition.Mkfs("/dev/node", "some-label", "ext4")
	c.Assert(err, IsNil)
	// details are already tested in the gadget package
	c.Assert(s.mockMkfsExt4.Calls(), HasLen, 1)
}

func (s *mkfsSuite) TestMakefilesystemsNothing(c *C) {
	created := map[string]gadget.LaidOutStructure{}
	err := partition.MakeFilesystems(created)
	c.Assert(err, IsNil)
	c.Assert(s.mockMkfsExt4.Calls(), HasLen, 0)
	c.Assert(s.mockMkfsVfat.Calls(), HasLen, 0)
}

func (s *mkfsSuite) TestMakefilesystems(c *C) {
	created := map[string]gadget.LaidOutStructure{
		"/dev/node2": {VolumeStructure: &gadget.VolumeStructure{
			Name:       "Recovery",
			Size:       1258291200,
			Type:       "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			Role:       "system-seed",
			Filesystem: "vfat",
			Content: []gadget.VolumeContent{
				{
					Source: "grubx64.efi",
					Target: "EFI/boot/grubx64.efi",
				},
			},
		},
			StartOffset: 2097152,
			Index:       2,
		},
		"/dev/node3": {VolumeStructure: &gadget.VolumeStructure{
			Name:       "Writable",
			Size:       1258291200,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-data",
			Filesystem: "ext4",
		},
			StartOffset: 1260388352,
			Index:       3,
		},
	}
	err := partition.MakeFilesystems(created)
	c.Assert(err, IsNil)
	c.Assert(s.mockMkfsExt4.Calls(), HasLen, 1)
	c.Assert(s.mockMkfsVfat.Calls(), HasLen, 1)
}
