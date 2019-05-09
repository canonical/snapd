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

package gadget_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
)

type updateTestSuite struct{}

var _ = Suite(&updateTestSuite{})

func (u *updateTestSuite) TestResolveVolumeDifferentName(c *C) {
	oldInfo := &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"old": {},
		},
	}
	noMatchInfo := &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"not-old": {},
		},
	}
	oldVol, newVol, err := gadget.ResolveVolume(oldInfo, noMatchInfo)
	c.Assert(err, ErrorMatches, `cannot find entry for volume "old" in updated gadget info`)
	c.Assert(oldVol, IsNil)
	c.Assert(newVol, IsNil)
}

func (u *updateTestSuite) TestResolveVolumeTooMany(c *C) {
	oldInfo := &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"old":         {},
			"another-one": {},
		},
	}
	noMatchInfo := &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"old": {},
		},
	}
	oldVol, newVol, err := gadget.ResolveVolume(oldInfo, noMatchInfo)
	c.Assert(err, ErrorMatches, `cannot update with more than one volume`)
	c.Assert(oldVol, IsNil)
	c.Assert(newVol, IsNil)
}

func (u *updateTestSuite) TestResolveVolumeSimple(c *C) {
	oldInfo := &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"old": {Bootloader: "u-boot"},
		},
	}
	noMatchInfo := &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"old": {Bootloader: "grub"},
		},
	}
	oldVol, newVol, err := gadget.ResolveVolume(oldInfo, noMatchInfo)
	c.Assert(err, IsNil)
	c.Assert(oldVol, DeepEquals, &gadget.Volume{Bootloader: "u-boot"})
	c.Assert(newVol, DeepEquals, &gadget.Volume{Bootloader: "grub"})
}

type canUpdateTestCase struct {
	from gadget.PositionedStructure
	to   gadget.PositionedStructure
	err  string
}

func (u *updateTestSuite) testCanUpdate(c *C, testCases []canUpdateTestCase) {
	for idx, tc := range testCases {
		c.Logf("tc: %v", idx)
		err := gadget.CanUpdateStructure(&tc.from, &tc.to)
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}
	}
}

func (u *updateTestSuite) TestCanUpdateSize(c *C) {

	cases := []canUpdateTestCase{
		{
			// size change
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1*gadget.SizeMiB + 1*gadget.SizeKiB},
			},
			err: "cannot change structure size from [0-9]+ to [0-9]+",
		}, {
			// size change
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB},
			},
			err: "",
		},
	}

	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateOffsetWrite(c *C) {

	cases := []canUpdateTestCase{
		{
			// offset-write change
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 1024},
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 2048},
				},
			},
			err: "cannot change structure offset-write from [0-9]+ to [0-9]+",
		}, {
			// offset-write, change in relative-to structure name
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "bar", Offset: 1024},
				},
			},
			err: `cannot change structure offset-write from foo\+[0-9]+ to bar\+[0-9]+`,
		}, {
			// offset-write, unspecified in old
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "bar", Offset: 1024},
				},
			},
			err: `cannot change structure offset-write from unspecified to bar\+[0-9]+`,
		}, {
			// offset-write, unspecified in new
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			err: `cannot change structure offset-write from foo\+[0-9]+ to unspecified`,
		}, {
			// all ok, both nils
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			err: ``,
		}, {
			// all ok, both fully specified
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			err: ``,
		}, {
			// all ok, both fully specified
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 1024},
				},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 1024},
				},
			},
			err: ``,
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateOffset(c *C) {

	cases := []canUpdateTestCase{
		{
			// explicitly declared start offset change
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB, Offset: asSizePtr(1024)},
				StartOffset:     1024,
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB, Offset: asSizePtr(2048)},
				StartOffset:     2048,
			},
			err: "cannot change structure offset from [0-9]+ to [0-9]+",
		}, {
			// explicitly declared start offset in new structure
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB, Offset: nil},
				StartOffset:     1024,
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB, Offset: asSizePtr(2048)},
				StartOffset:     2048,
			},
			err: "cannot change structure offset from unspecified to [0-9]+",
		}, {
			// explicitly declared start offset in old structure,
			// missing from new
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB, Offset: asSizePtr(1024)},
				StartOffset:     1024,
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB, Offset: nil},
				StartOffset:     2048,
			},
			err: "cannot change structure offset from [0-9]+ to unspecified",
		}, {
			// start offset changed due to positioning
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB},
				StartOffset:     1 * gadget.SizeMiB,
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * gadget.SizeMiB},
				StartOffset:     2 * gadget.SizeMiB,
			},
			err: "cannot change structure start offset from [0-9]+ to [0-9]+",
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateRole(c *C) {

	cases := []canUpdateTestCase{
		{
			// new role
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: ""},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: "system-data"},
			},
			err: `cannot change structure role from "" to "system-data"`,
		}, {
			// explicitly set tole
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: "mbr"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: "system-data"},
			},
			err: `cannot change structure role from "mbr" to "system-data"`,
		}, {
			// implicit legacy role to proper explicit role
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "mbr"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare", Role: "mbr"},
			},
			err: "",
		}, {
			// but not in the opposite direction
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare", Role: "mbr"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "mbr"},
			},
			err: `cannot change structure type from "bare" to "mbr"`,
		}, {
			// start offset changed due to positioning
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: ""},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: ""},
			},
			err: "",
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateType(c *C) {

	cases := []canUpdateTestCase{
		{
			// from hybrid type to GUID
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err: `cannot change structure type from "0C,00000000-0000-0000-0000-dd00deadbeef" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			// from MBR type to GUID (would be stopped at volume update checks)
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err: `cannot change structure type from "0C" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			// from one MBR type to another
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0A"},
			},
			err: `cannot change structure type from "0C" to "0A"`,
		}, {
			// from one MBR type to another
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare"},
			},
			err: `cannot change structure type from "0C" to "bare"`,
		}, {
			// from one GUID to another
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadcafe"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err: `cannot change structure type from "00000000-0000-0000-0000-dd00deadcafe" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare"},
			},
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef"},
			},
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateID(c *C) {

	cases := []canUpdateTestCase{
		{
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{ID: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{ID: "00000000-0000-0000-0000-dd00deadcafe"},
			},
			err: `cannot change structure ID from "00000000-0000-0000-0000-dd00deadbeef" to "00000000-0000-0000-0000-dd00deadcafe"`,
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateBareOrFilesystem(c *C) {

	cases := []canUpdateTestCase{
		{
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: ""},
			},
			err: `cannot change a filesystem structure to a bare one`,
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: ""},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			err: `cannot change a bare structure to filesystem one`,
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "vfat"},
			},
			err: `cannot change filesystem from "ext4" to "vfat"`,
		}, {
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "writable"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			err: `cannot change filesystem label from "writable" to ""`,
		}, {
			// from implicit filesystem label to explicit one
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Role: "system-data"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Role: "system-data", Label: "writable"},
			},
			err: ``,
		}, {
			// all ok
			from: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "do-not-touch"},
			},
			to: gadget.PositionedStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "do-not-touch"},
			},
			err: ``,
		},
	}
	u.testCanUpdate(c, cases)
}
