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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

var (
	uc16Model = &gadgettest.ModelCharacteristics{}
	uc20Model = &gadgettest.ModelCharacteristics{HasModes: true}
)

type updateTestSuite struct {
	restoreVolumeStructureToLocationMap func()

	testutil.BaseTest
}

var _ = Suite(&updateTestSuite{})

func (s *updateTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return nil, nil, fmt.Errorf("unmocked volume structure to loc map")
	})
	restoreDoer := sync.Once{}
	s.restoreVolumeStructureToLocationMap = func() {
		restoreDoer.Do(r)
	}
	s.AddCleanup(func() {
		s.restoreVolumeStructureToLocationMap()
	})
}

func (u *updateTestSuite) TestResolveVolumeDifferentName(c *C) {
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"old": {},
		},
	}
	noMatchInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
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
		Volumes: map[string]*gadget.Volume{
			"old":         {},
			"another-one": {},
		},
	}
	noMatchInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
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
		Volumes: map[string]*gadget.Volume{
			"old": {Bootloader: "u-boot"},
		},
	}
	noMatchInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"old": {Bootloader: "grub"},
		},
	}
	oldVol, newVol, err := gadget.ResolveVolume(oldInfo, noMatchInfo)
	c.Assert(err, IsNil)
	c.Assert(oldVol, DeepEquals, &gadget.Volume{Bootloader: "u-boot"})
	c.Assert(newVol, DeepEquals, &gadget.Volume{Bootloader: "grub"})
}

type canUpdateTestCase struct {
	from   gadget.VolumeStructure
	to     gadget.VolumeStructure
	schema string
	err    string
}

func (u *updateTestSuite) testCanUpdate(c *C, testCases []canUpdateTestCase) {
	for idx, tc := range testCases {
		c.Logf("tc: %v", idx)
		schema := tc.schema
		if schema == "" {
			schema = "gpt"
		}
		fromVss := &gadget.Volume{Schema: schema,
			Structure: []gadget.VolumeStructure{tc.from}}
		toVss := &gadget.Volume{Schema: schema,
			Structure: []gadget.VolumeStructure{tc.to}}
		err := gadget.CanUpdateStructure(fromVss, 0, toVss, 0)
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}
	}
}

func (u *updateTestSuite) TestCanUpdateSize(c *C) {
	mokVol := &gadget.Volume{}
	partSizeVol := &gadget.Volume{Partial: []gadget.PartialProperty{gadget.PartialSize}}
	cases := []canUpdateTestCase{
		{
			// size change
			from: gadget.VolumeStructure{MinSize: quantity.SizeMiB, Size: quantity.SizeMiB, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: quantity.SizeMiB + quantity.SizeKiB, Size: quantity.SizeMiB + quantity.SizeKiB, EnclosingVolume: mokVol},
			err:  `new valid structure size range \[1049600, 1049600\] is not compatible with current \(\[1048576, 1048576\]\)`,
		}, {
			// no size change
			from: gadget.VolumeStructure{MinSize: quantity.SizeMiB, Size: quantity.SizeMiB, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: quantity.SizeMiB, Size: quantity.SizeMiB, EnclosingVolume: mokVol},
			err:  "",
		}, {
			// range ok
			from: gadget.VolumeStructure{MinSize: 10, Size: 20, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 0, Size: 10, EnclosingVolume: mokVol},
			err:  "",
		}, {
			// range ok
			from: gadget.VolumeStructure{MinSize: 10, Size: 20, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 0, Size: 15, EnclosingVolume: mokVol},
			err:  "",
		}, {
			// range ok
			from: gadget.VolumeStructure{MinSize: 10, Size: 20, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 15, Size: 18, EnclosingVolume: mokVol},
			err:  "",
		}, {
			// range ok
			from: gadget.VolumeStructure{MinSize: 10, Size: 20, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 15, Size: 25, EnclosingVolume: mokVol},
			err:  "",
		}, {
			// range out
			from: gadget.VolumeStructure{MinSize: 10, Size: 20, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 1, Size: 9, EnclosingVolume: mokVol},
			err:  `new valid structure size range \[1, 9\] is not compatible with current \(\[10, 20\]\)`,
		}, {
			// from is partial, ok
			from: gadget.VolumeStructure{MinSize: 0, Size: 0, EnclosingVolume: partSizeVol},
			to:   gadget.VolumeStructure{MinSize: 1, Size: 9, EnclosingVolume: mokVol},
		}, {
			// to is partial, ok
			from: gadget.VolumeStructure{MinSize: 1, Size: 9, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 0, Size: 0, EnclosingVolume: partSizeVol},
		}, {
			// both are partial, ok
			from: gadget.VolumeStructure{MinSize: 0, Size: 0, EnclosingVolume: partSizeVol},
			to:   gadget.VolumeStructure{MinSize: 0, Size: 0, EnclosingVolume: partSizeVol},
		}, {
			// from is partial, but has MinSize so we are out of range
			from: gadget.VolumeStructure{MinSize: 10, Size: 0, EnclosingVolume: partSizeVol},
			to:   gadget.VolumeStructure{MinSize: 1, Size: 9, EnclosingVolume: mokVol},
			err:  `new valid structure size range \[1, 9\] is not compatible with current \(\[10, 18446744073709551615\]\)`,
		}, {
			// range out
			from: gadget.VolumeStructure{MinSize: 10, Size: 20, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{MinSize: 21, Size: 25, EnclosingVolume: mokVol},
			err:  `new valid structure size range \[21, 25\] is not compatible with current \(\[10, 20\]\)`,
		},
	}

	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateOffsetWrite(c *C) {
	// We do not care about changes in offset-write, so we check that nothing
	// errors here.
	mokVol := &gadget.Volume{}
	cases := []canUpdateTestCase{
		{
			// offset-write change
			from: gadget.VolumeStructure{
				OffsetWrite: &gadget.RelativeOffset{Offset: 1024}, EnclosingVolume: mokVol},
			to: gadget.VolumeStructure{
				OffsetWrite: &gadget.RelativeOffset{Offset: 2048}, EnclosingVolume: mokVol},
		}, {
			// both nils
			from: gadget.VolumeStructure{
				OffsetWrite:     nil,
				EnclosingVolume: mokVol,
			},
			to: gadget.VolumeStructure{
				OffsetWrite:     nil,
				EnclosingVolume: mokVol,
			},
		}, {
			// both fully specified
			from: gadget.VolumeStructure{
				OffsetWrite:     &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				EnclosingVolume: mokVol,
			},
			to: gadget.VolumeStructure{
				OffsetWrite:     &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				EnclosingVolume: mokVol,
			},
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateOffset(c *C) {
	mokVol := &gadget.Volume{}
	cases := []canUpdateTestCase{
		{
			// explicitly declared start offset change
			from: gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(1024), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(2048), EnclosingVolume: mokVol},
			err:  `new valid structure offset range \[2048, 2048\] is not compatible with current \(\[1024, 1024\]\)`,
		}, {
			// explicitly declared start offset in new structure
			from: gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: nil, EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(2048), EnclosingVolume: mokVol},
			err:  `new valid structure offset range \[2048, 2048\] is not compatible with current \(\[0, 0\]\)`,
		}, {
			// explicitly declared start offset in old structure,
			// missing from new
			from: gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(1024), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: nil, EnclosingVolume: mokVol},
			err:  `new valid structure offset range \[0, 0\] is not compatible with current \(\[1024, 1024\]\)`,
		}, {
			// start offset changed due to layout
			from: gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(1 * quantity.OffsetMiB), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(2 * quantity.OffsetMiB), EnclosingVolume: mokVol},
			err:  `new valid structure offset range \[2097152, 2097152\] is not compatible with current \(\[1048576, 1048576\]\)`,
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateRole(c *C) {

	cases := []canUpdateTestCase{
		{
			// new role
			from: gadget.VolumeStructure{Role: "", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Role: "system-data", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure role from "" to "system-data"`,
		}, {
			// explicitly set tole
			from: gadget.VolumeStructure{Role: "mbr", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Role: "system-data", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure role from "mbr" to "system-data"`,
		}, {
			// implicit legacy role to proper explicit role
			from: gadget.VolumeStructure{Type: "mbr", Role: "mbr", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "bare", Role: "mbr", EnclosingVolume: &gadget.Volume{}},
			err:  "",
		}, {
			// but not in the opposite direction
			from: gadget.VolumeStructure{Type: "bare", Role: "mbr", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "mbr", Role: "mbr", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure type from "bare" to "mbr"`,
		}, {
			// start offset changed due to layout
			from: gadget.VolumeStructure{Role: "", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Role: "", EnclosingVolume: &gadget.Volume{}},
			err:  "",
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateType(c *C) {

	cases := []canUpdateTestCase{
		{
			// from hybrid type to GUID -> fine, we keep one of the types
			from: gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
		}, {
			// from hybrid type to MBR -> fine, we keep one of the types
			from: gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
		}, {
			// from MBR to hybrid -> fine, we keep one of the types
			from: gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
		}, {
			// from GUID to hybrid -> fine, we keep one of the types
			from: gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
		}, {
			// from MBR type to GUID (would be stopped at volume update checks)
			from: gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure type from "0C" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			// from one MBR type to another
			from: gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "0A", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure type from "0C" to "0A"`,
		}, {
			// from one MBR type to another
			from: gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "bare", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure type from "0C" to "bare"`,
		}, {
			// from one GUID to another
			from: gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadcafe", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure type from "00000000-0000-0000-0000-dd00deadcafe" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			from: gadget.VolumeStructure{Type: "bare", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "bare", EnclosingVolume: &gadget.Volume{}},
		}, {
			from: gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "0C", EnclosingVolume: &gadget.Volume{}},
		}, {
			from: gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
		}, {
			from: gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateID(c *C) {

	cases := []canUpdateTestCase{
		{
			from: gadget.VolumeStructure{ID: "00000000-0000-0000-0000-dd00deadbeef", Offset: asOffsetPtr(0), EnclosingVolume: &gadget.Volume{}},
			to:   gadget.VolumeStructure{ID: "00000000-0000-0000-0000-dd00deadcafe", Offset: asOffsetPtr(0), EnclosingVolume: &gadget.Volume{}},
			err:  `cannot change structure ID from "00000000-0000-0000-0000-dd00deadbeef" to "00000000-0000-0000-0000-dd00deadcafe"`,
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateBareOrFilesystem(c *C) {
	mokVol := &gadget.Volume{}
	partFsVol := &gadget.Volume{Partial: []gadget.PartialProperty{gadget.PartialFilesystem}}
	cases := []canUpdateTestCase{
		{
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			err:  `cannot change a filesystem structure to a bare one`,
		}, {
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			err:  `cannot change a bare structure to filesystem one`,
		}, {
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "vfat", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			err:  `cannot change filesystem from "ext4" to "vfat"`,
		}, {
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "writable", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			err:  `cannot change filesystem label from "writable" to ""`,
		}, {
			// From/to both undefined filesystem is ok
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "", Offset: asOffsetPtr(0), EnclosingVolume: partFsVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "", Offset: asOffsetPtr(0), EnclosingVolume: partFsVol},
		}, {
			// From undefined to defined filesystem is ok
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "", Offset: asOffsetPtr(0), EnclosingVolume: partFsVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
		}, {
			// From defined to undefined filesystem is wrong
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "", Offset: asOffsetPtr(0), EnclosingVolume: partFsVol},
			err:  `cannot change filesystem from "ext4" to ""`,
		}, {
			// all ok
			from: gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "do-not-touch", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			to:   gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "do-not-touch", Offset: asOffsetPtr(0), EnclosingVolume: mokVol},
			err:  ``,
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateName(c *C) {

	cases := []canUpdateTestCase{
		{
			from:   gadget.VolumeStructure{Name: "foo", Type: "0C", EnclosingVolume: &gadget.Volume{}},
			to:     gadget.VolumeStructure{Name: "mbr-ok", Type: "0C", EnclosingVolume: &gadget.Volume{}},
			err:    ``,
			schema: "mbr",
		}, {
			from:   gadget.VolumeStructure{Name: "foo", Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			to:     gadget.VolumeStructure{Name: "gpt-unhappy", Type: "00000000-0000-0000-0000-dd00deadbeef", EnclosingVolume: &gadget.Volume{}},
			err:    `cannot change structure name from "foo" to "gpt-unhappy"`,
			schema: "gpt",
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateOffsetRange(c *C) {
	fromV := &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Offset: asOffsetPtr(0), MinSize: 10, Size: 20, EnclosingVolume: &gadget.Volume{}},
			// Valid offset range for second structure is [10, 20]
			{MinSize: 10, Size: 10, EnclosingVolume: &gadget.Volume{}},
		},
	}
	toV := &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Offset: asOffsetPtr(0), MinSize: 10, Size: 10, EnclosingVolume: &gadget.Volume{}},
			{MinSize: 10, Size: 10, EnclosingVolume: &gadget.Volume{}},
		},
	}

	err := gadget.CanUpdateStructure(fromV, 1, toV, 1)
	c.Check(err, IsNil)

	toV = &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Offset: asOffsetPtr(0), MinSize: 15, Size: 21, EnclosingVolume: &gadget.Volume{}},
			{MinSize: 10, Size: 10, EnclosingVolume: &gadget.Volume{}},
		},
	}

	err = gadget.CanUpdateStructure(fromV, 1, toV, 1)
	c.Check(err, IsNil)

	toV = &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Offset: asOffsetPtr(0), MinSize: 21, Size: 30, EnclosingVolume: &gadget.Volume{}},
			{MinSize: 10, Size: 10, EnclosingVolume: &gadget.Volume{}},
		},
	}

	err = gadget.CanUpdateStructure(fromV, 1, toV, 1)
	c.Check(err.Error(), Equals,
		`new valid structure offset range [21, 30] is not compatible with current ([10, 20])`)
}

func (u *updateTestSuite) TestCanUpdateVolume(c *C) {
	mbrVol := &gadget.Volume{Schema: "mbr"}
	mbrLaidOut := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{EnclosingVolume: mbrVol}}

	for idx, tc := range []struct {
		from gadget.PartiallyLaidOutVolume
		to   gadget.LaidOutVolume
		err  string
	}{
		{
			from: gadget.PartiallyLaidOutVolume{
				Volume: &gadget.Volume{Schema: "gpt"},
			},
			to: gadget.LaidOutVolume{
				Volume: mbrVol,
			},
			err: `incompatible schema change from gpt to mbr`,
		}, {
			from: gadget.PartiallyLaidOutVolume{
				Volume: &gadget.Volume{ID: "00000000-0000-0000-0000-0000deadbeef"},
			},
			to: gadget.LaidOutVolume{
				Volume: &gadget.Volume{ID: "00000000-0000-0000-0000-0000deadcafe"},
			},
			err: `cannot change volume ID from "00000000-0000-0000-0000-0000deadbeef" to "00000000-0000-0000-0000-0000deadcafe"`,
		}, {
			from: gadget.PartiallyLaidOutVolume{
				Volume: &gadget.Volume{},
				LaidOutStructure: []gadget.LaidOutStructure{
					{}, {},
				},
			},
			to: gadget.LaidOutVolume{
				Volume: &gadget.Volume{},
				LaidOutStructure: []gadget.LaidOutStructure{
					{},
				},
			},
			err: `cannot change the number of structures within volume from 2 to 1`,
		}, {
			// valid
			from: gadget.PartiallyLaidOutVolume{
				Volume: mbrVol,
				LaidOutStructure: []gadget.LaidOutStructure{
					*mbrLaidOut, *mbrLaidOut,
				},
			},
			to: gadget.LaidOutVolume{
				Volume: mbrVol,
				LaidOutStructure: []gadget.LaidOutStructure{
					*mbrLaidOut, *mbrLaidOut,
				},
			},
			err: ``,
		},
	} {
		c.Logf("tc: %v", idx)
		err := gadget.CanUpdateVolume(&tc.from, &tc.to)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}

	}
}

type mockUpdater struct {
	updateCb   func() error
	backupCb   func() error
	rollbackCb func() error
}

func callOrNil(f func() error) error {
	if f != nil {
		return f()
	}
	return nil
}

func (m *mockUpdater) Backup() error {
	return callOrNil(m.backupCb)
}

func (m *mockUpdater) Rollback() error {
	return callOrNil(m.rollbackCb)
}

func (m *mockUpdater) Update() error {
	return callOrNil(m.updateCb)
}

func (u *updateTestSuite) updateDataSet(c *C) (oldData gadget.GadgetData, newData gadget.GadgetData, rollbackDir string) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		VolumeName: "foo",
		Name:       "first",
		Offset:     asOffsetPtr(quantity.OffsetMiB),
		MinSize:    5 * quantity.SizeMiB,
		Size:       5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
		YamlIndex: 0,
	}
	fsStruct := gadget.VolumeStructure{
		VolumeName: "foo",
		Name:       "second",
		Offset:     asOffsetPtr((1 + 5) * quantity.OffsetMiB),
		MinSize:    10 * quantity.SizeMiB,
		Size:       10 * quantity.SizeMiB,
		Filesystem: "ext4",
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/second-content", Target: "/"},
		},
		YamlIndex: 1,
	}
	lastStruct := gadget.VolumeStructure{
		VolumeName: "foo",
		Name:       "third",
		Offset:     asOffsetPtr((1 + 5 + 10) * quantity.OffsetMiB),
		MinSize:    5 * quantity.SizeMiB,
		Size:       5 * quantity.SizeMiB,
		Filesystem: "vfat",
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/third-content", Target: "/"},
		},
		YamlIndex: 2,
	}
	// start with identical data for new and old infos, they get updated by
	// the caller as needed
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Name:       "foo",
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct, fsStruct, lastStruct},
			},
		},
	}
	newInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Name:       "foo",
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct, fsStruct, lastStruct},
			},
		},
	}

	// reasonably default volume structure to location map - individual tests
	// can override this
	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		// XXX return something that we'll check
		return map[string]map[int]gadget.StructureLocation{
			"foo": {
				0: {
					Device: "/dev/foo",
					Offset: quantity.OffsetMiB,
				},
				1: {
					RootMountPoint: "/foo",
				},
				2: {
					RootMountPoint: "/foo",
				},
			},
		}, nil, nil
	})
	u.AddCleanup(r)

	oldRootDir := c.MkDir()
	makeSizedFile(c, filepath.Join(oldRootDir, "first.img"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldRootDir, "/second-content/foo"), 0, nil)
	makeSizedFile(c, filepath.Join(oldRootDir, "/third-content/bar"), 0, nil)
	oldData = gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}

	newRootDir := c.MkDir()
	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), 900*quantity.SizeKiB, nil)
	makeSizedFile(c, filepath.Join(newRootDir, "/second-content/foo"), quantity.SizeKiB, nil)
	makeSizedFile(c, filepath.Join(newRootDir, "/third-content/bar"), quantity.SizeKiB, nil)
	newData = gadget.GadgetData{Info: newInfo, RootDir: newRootDir}
	gadget.SetEnclosingVolumeInStructs(oldData.Info.Volumes)
	gadget.SetEnclosingVolumeInStructs(newData.Info.Volumes)

	rollbackDir = c.MkDir()
	return oldData, newData, rollbackDir
}

type mockUpdateProcessObserver struct {
	beforeWriteCalled int
	canceledCalled    int
	beforeWriteErr    error
	canceledErr       error
}

func (m *mockUpdateProcessObserver) Observe(op gadget.ContentOperation, partRole, targetRootDir, relativeTargetPath string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
	return gadget.ChangeAbort, errors.New("unexpected call")
}

func (m *mockUpdateProcessObserver) BeforeWrite() error {
	m.beforeWriteCalled++
	return m.beforeWriteErr
}

func (m *mockUpdateProcessObserver) Canceled() error {
	m.canceledCalled++
	return m.canceledErr
}

func (u *updateTestSuite) TestUpdateApplyHappy(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// update two structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		switch updaterForStructureCalls {
		case 0:
			c.Check(ps.Name(), Equals, "first")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.VolumeStructure.Size, Equals, 5*quantity.SizeMiB)
			c.Check(ps.IsPartition(), Equals, true)
			// non MBR start offset defaults to 1MiB
			c.Check(ps.StartOffset, Equals, 1*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Check(ps.LaidOutContent[0].Image, Equals, "first.img")
			c.Check(ps.LaidOutContent[0].Size, Equals, 900*quantity.SizeKiB)
		case 1:
			c.Check(ps.Name(), Equals, "second")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 10*quantity.SizeMiB)
			// foo's start offset + foo's size
			c.Check(ps.StartOffset, Equals, (1+5)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Check(ps.VolumeStructure.Content[0].UnresolvedSource, Equals, "/second-content")
			c.Check(ps.VolumeStructure.Content[0].Target, Equals, "/")
		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected call")
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		"first":  true,
		"second": true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"first":  true,
		"second": true,
	})
	c.Assert(updaterForStructureCalls, Equals, 2)
	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUC16FullLogic(c *C) {
	u.restoreVolumeStructureToLocationMap()
	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	rollbackDir := c.MkDir()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.UC16YAMLImplicitSystemData, uc16Model)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/sda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("EFI System")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/sda1"): gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	// add a writable mountpoint for the system-boot partition
	restore = osutil.MockMountInfo(
		fmt.Sprintf(`27 27 600:3 / %[1]s/boot/ubuntu rw,relatime shared:7 - vfat %[1]s/dev/sda2 rw`, dirs.GlobalRootDir),
	)
	defer restore()

	// update all 3 structs

	// mbr and bios images
	for i, imgName := range []string{"mbr.img", "bios.img"} {
		imgPath := filepath.Join(newData.RootDir, imgName)
		err = os.WriteFile(imgPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i].Content = []gadget.VolumeContent{{Image: imgName}}
		newData.Info.Volumes["pc"].Structure[i].Update.Edition = 1
	}
	// system-boot - partition w/ filesystem struct
	grubImg := filepath.Join(newData.RootDir, "grub.efi")
	err = os.WriteFile(grubImg, nil, 0644)
	c.Assert(err, IsNil)
	newData.Info.Volumes["pc"].Structure[2].Update.Edition = 1
	newData.Info.Volumes["pc"].Structure[2].Content = []gadget.VolumeContent{{UnresolvedSource: "grub.efi"}}

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		switch updaterForStructureCalls {
		case 0:
			// mbr raw structure
			c.Check(ps.Name(), Equals, "mbr")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(440))
			c.Check(ps.IsPartition(), Equals, false)
			// no offset since we are updating the MBR itself
			c.Check(ps.StartOffset, Equals, quantity.Offset(0))
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/sda",
				Offset: quantity.Offset(0),
			})
		case 1:
			// bios boot
			c.Check(ps.Name(), Equals, "BIOS Boot")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/sda",
				Offset: quantity.OffsetMiB,
			})
		case 2:
			// EFI system partition
			c.Check(ps.Name(), Equals, "EFI System")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "vfat")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 50*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/boot/ubuntu"),
			})
		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected call")
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(updaterForStructureCalls, Equals, 3)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		"mbr":        true,
		"BIOS Boot":  true,
		"EFI System": true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"mbr":        true,
		"BIOS Boot":  true,
		"EFI System": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUC20MissingInitialMapFullLogicOnlySystemBoot(c *C) {
	u.restoreVolumeStructureToLocationMap()
	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	rollbackDir := c.MkDir()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.MultiVolumeUC20GadgetYaml, uc20Model)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// note don't need to mock anything for the second volume on disk, we don't
	// consider it at all

	// setup symlink for the BIOS Boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("BIOS Boot")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	// set all structs on system-boot volume to be updated - only structs on
	// system-boot volume can be updated as per policy since the initial mapping
	// was missing

	// mbr - bare structure, and bios - partition w/o filesystem
	for i, imgName := range []string{"mbr.img", "bios.img"} {
		imgPath := filepath.Join(newData.RootDir, imgName)
		err = os.WriteFile(imgPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i].Content = []gadget.VolumeContent{{Image: imgName}}
		newData.Info.Volumes["pc"].Structure[i].Update.Edition = 1
	}
	// ubuntu-{seed,boot,save,data}
	for i, fName := range []string{"seed", "boot", "save", "data"} {
		fPath := filepath.Join(newData.RootDir, fName)
		err = os.WriteFile(fPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i+2].Content = []gadget.VolumeContent{{UnresolvedSource: fName}}
		newData.Info.Volumes["pc"].Structure[i+2].Update.Edition = 1
	}

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						Device: "/dev/vda",
					},
					1: {
						Device: "/dev/vda",
						Offset: quantity.OffsetMiB,
					},
					2: {
						RootMountPoint: "/run/mnt/ubuntu-seed",
					},
					3: {
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
					4: {
						RootMountPoint: "/run/mnt/ubuntu-save",
					},
					5: {
						RootMountPoint: "/run/mnt/data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc":  gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		switch updaterForStructureCalls {
		case 0:
			// mbr raw structure
			c.Check(ps.Name(), Equals, "mbr")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(440))
			c.Check(ps.IsPartition(), Equals, false)
			// no offset since we are updating the MBR itself
			c.Check(ps.StartOffset, Equals, quantity.Offset(0))
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/vda",
				Offset: quantity.Offset(0),
			})
		case 1:
			// bios boot
			c.Check(ps.Name(), Equals, "BIOS Boot")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/vda",
				Offset: quantity.OffsetMiB,
			})
		case 5:
			// ubuntu-seed
			c.Check(ps.Name(), Equals, "ubuntu-seed")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "vfat")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 1200*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.ResolvedContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-seed",
			})
		case 4:
			// ubuntu-boot
			c.Check(ps.Name(), Equals, "ubuntu-boot")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 750*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1+1200)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.ResolvedContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-boot",
			})
		case 2:
			// ubuntu-save
			c.Check(ps.Name(), Equals, "ubuntu-save")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 16*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1+1200+750)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.ResolvedContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-save",
			})
		case 3:
			// ubuntu-data
			c.Check(ps.Name(), Equals, "ubuntu-data")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			// NOTE: this is the laid out size, not the actual size (since data
			// gets expanded), but the update op doesn't actually care about the
			// size so it's okay
			c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeGiB)
			c.Check(ps.StartOffset, Equals, (1+1+1200+750+16)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.ResolvedContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/data",
			})

		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected call")
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(updaterForStructureCalls, Equals, 6)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"BIOS Boot":   true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
		"ubuntu-data": true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"BIOS Boot":   true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
		"ubuntu-data": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUC20MissingInitialMapFullLogicOnlySystemBootSeedFslabelCaps(c *C) {
	u.restoreVolumeStructureToLocationMap()
	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	rollbackDir := c.MkDir()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.MultiVolumeUC20GadgetYamlNoBIOS, uc20Model)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// note don't need to mock anything for the second volume on disk, we don't
	// consider it at all

	// setup symlink for the ubuntu-seed partition, in capitals
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label", disks.BlkIDEncodeLabel("UBUNTU-SEED")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMappingSeedFsLabelCaps,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMappingSeedFsLabelCaps,
	})
	defer restore()

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	// set all structs on system-boot volume to be updated - only structs on
	// system-boot volume can be updated as per policy since the initial mapping
	// was missing

	// mbr - bare structure
	imgName := "mbr.img"
	imgPath := filepath.Join(newData.RootDir, imgName)
	err = os.WriteFile(imgPath, nil, 0644)
	c.Assert(err, IsNil)
	newData.Info.Volumes["pc"].Structure[0].Content = []gadget.VolumeContent{{Image: imgName}}
	newData.Info.Volumes["pc"].Structure[0].Update.Edition = 1
	// ubuntu-{seed,boot,save} - we do not put content in ubuntu-data
	for i, fName := range []string{"seed", "boot", "save"} {
		fPath := filepath.Join(newData.RootDir, fName)
		err = os.WriteFile(fPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i+1].Content = []gadget.VolumeContent{{UnresolvedSource: fName}}
		newData.Info.Volumes["pc"].Structure[i+1].Update.Edition = 1
	}

	// Force an update in "edition" in ubuntu-data even if we do not have
	// content, to check that updates are ignored in this case.
	newData.Info.Volumes["pc"].Structure[4].Update.Edition = 1

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						Device: "/dev/vda",
					},
					1: {
						RootMountPoint: "/run/mnt/ubuntu-seed",
					},
					2: {
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
					3: {
						RootMountPoint: "/run/mnt/ubuntu-save",
					},
					4: {
						RootMountPoint: "/run/mnt/data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc":  gadget.OnDiskStructsFromGadget(newData.Info.Volumes["pc"]),
				"foo": gadget.OnDiskStructsFromGadget(newData.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		switch updaterForStructureCalls {
		case 0:
			// mbr raw structure
			c.Check(ps.Name(), Equals, "mbr")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(440))
			c.Check(ps.IsPartition(), Equals, false)
			// no offset since we are updating the MBR itself
			c.Check(ps.StartOffset, Equals, quantity.Offset(0))
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/vda",
				Offset: quantity.Offset(0),
			})
		case 3:
			// ubuntu-seed
			c.Check(ps.Name(), Equals, "ubuntu-seed")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "vfat")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 1200*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, 1*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-seed",
			})
		case 2:
			// ubuntu-boot
			c.Check(ps.Name(), Equals, "ubuntu-boot")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 750*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1200)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-boot",
			})
		case 1:
			// ubuntu-save
			c.Check(ps.Name(), Equals, "ubuntu-save")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 16*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1200+750)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-save",
			})
		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected call")
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	restore = disks.MockUdevPropertiesForDevice(func(typeOpt, dev string) (map[string]string, error) {
		switch dev {
		case filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/UBUNTU-SEED"):
			c.Assert(typeOpt, Equals, "--name")
			return map[string]string{
				"ID_FS_TYPE": "vfat",
			}, nil
		case "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda":
			c.Assert(typeOpt, Equals, "--path")
			return map[string]string{}, nil
		default:
			c.Errorf("unexpected udev device properties requested: %s", dev)
			return nil, fmt.Errorf("unexpected udev device: %s", dev)
		}
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(updaterForStructureCalls, Equals, 4)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUC20MissingInitialMapFullLogicOnlySystemBootEvenIfAllVolsHaveUpdates(c *C) {
	u.restoreVolumeStructureToLocationMap()
	mockLog, restore := logger.MockLogger()
	defer restore()

	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	rollbackDir := c.MkDir()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.MultiVolumeUC20GadgetYaml, uc20Model)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("BIOS Boot")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore = disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	// try to update all structures on both volumes, but only the structures on
	// the system-boot volume will end up getting updated as per policy

	// mbr - bare structure, and bios - partition w/o filesystem
	for i, imgName := range []string{"mbr.img", "bios.img"} {
		imgPath := filepath.Join(newData.RootDir, imgName)
		err = os.WriteFile(imgPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i].Content = []gadget.VolumeContent{{Image: imgName}}
		newData.Info.Volumes["pc"].Structure[i].Update.Edition = 1
	}
	// ubuntu-{seed,boot,save,data}
	for i, fName := range []string{"seed", "boot", "save", "data"} {
		fPath := filepath.Join(newData.RootDir, fName)
		err = os.WriteFile(fPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i+2].Content = []gadget.VolumeContent{{UnresolvedSource: fName}}
		newData.Info.Volumes["pc"].Structure[i+2].Update.Edition = 1
	}

	for i, imgName := range []string{"bare", "part"} {
		imgPath := filepath.Join(newData.RootDir, imgName)
		err = os.WriteFile(imgPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["foo"].Structure[i].Content = []gadget.VolumeContent{{Image: imgName}}
		newData.Info.Volumes["foo"].Structure[i].Update.Edition = 1
	}
	fName := "some-file"
	fPath := filepath.Join(newData.RootDir, fName)
	err = os.WriteFile(fPath, nil, 0644)
	c.Assert(err, IsNil)
	newData.Info.Volumes["foo"].Structure[2].Content = []gadget.VolumeContent{{UnresolvedSource: fName}}
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 1

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						Device: "/dev/vda",
					},
					1: {
						Device: "/dev/vda",
						Offset: quantity.OffsetMiB,
					},
					2: {
						RootMountPoint: "/run/mnt/ubuntu-seed",
					},
					3: {
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
					4: {
						RootMountPoint: "/run/mnt/ubuntu-save",
					},
					5: {
						RootMountPoint: "/run/mnt/data",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc":  gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		switch updaterForStructureCalls {
		case 0:
			// mbr raw structure
			c.Check(ps.Name(), Equals, "mbr")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(440))
			c.Check(ps.IsPartition(), Equals, false)
			// no offset since we are updating the MBR itself
			c.Check(ps.StartOffset, Equals, quantity.Offset(0))
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/vda",
				Offset: quantity.Offset(0),
			})
		case 1:
			// bios boot
			c.Check(ps.Name(), Equals, "BIOS Boot")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				Device: "/dev/vda",
				Offset: quantity.OffsetMiB,
			})
		case 5:
			// ubuntu-seed
			c.Check(ps.Name(), Equals, "ubuntu-seed")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "vfat")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 1200*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-seed",
			})
		case 4:
			// ubuntu-boot
			c.Check(ps.Name(), Equals, "ubuntu-boot")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 750*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1+1200)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-boot",
			})
		case 2:
			// ubuntu-save
			c.Check(ps.Name(), Equals, "ubuntu-save")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.VolumeStructure.Size, Equals, 16*quantity.SizeMiB)
			c.Check(ps.StartOffset, Equals, (1+1+1200+750)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/ubuntu-save",
			})
		case 3:
			// ubuntu-data
			c.Check(ps.Name(), Equals, "ubuntu-data")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem(), Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			// NOTE: this is the laid out size, not the actual size (since data
			// gets expanded), but the update op doesn't actually care about the
			// size so it's okay
			c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeGiB)
			c.Check(ps.StartOffset, Equals, (1+1+1200+750+16)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.VolumeStructure.Content, HasLen, 1)
			c.Assert(loc, Equals, gadget.StructureLocation{
				RootMountPoint: "/run/mnt/data",
			})

		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected call")
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(updaterForStructureCalls, Equals, 6)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"BIOS Boot":   true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
		"ubuntu-data": true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"BIOS Boot":   true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
		"ubuntu-data": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)

	c.Assert(mockLog.String(), testutil.Contains, "skipping update on non-supported volume foo to structure barething")
	c.Assert(mockLog.String(), testutil.Contains, "skipping update on non-supported volume foo to structure nofspart")
	c.Assert(mockLog.String(), testutil.Contains, "skipping update on non-supported volume foo to structure some-filesystem")
}

func (u *updateTestSuite) TestUpdateApplyUC20WithInitialMapAllVolumesUpdatedFullLogic(c *C) {
	u.restoreVolumeStructureToLocationMap()

	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	rollbackDir := c.MkDir()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.MultiVolumeUC20GadgetYaml, uc20Model)
	c.Assert(err, IsNil)

	err = os.MkdirAll(dirs.SnapDeviceDir, 0755)
	c.Assert(err, IsNil)
	// write out the provided traits JSON so we can at least load the traits for
	// mocking via setupForVolumeStructureToLocation
	err = os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.VMMultiVolumeUC20DiskTraitsJSON),
		0644,
	)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("BIOS Boot")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
		filepath.Join(dirs.GlobalRootDir, "/dev/vdb1"): gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
		"/dev/vdb": gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily

	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 525:3 / %[1]s/foo/some-filesystem rw,relatime shared:7 - vfat %[1]s/dev/vdb2 rw
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	// update all structures
	// mbr - bare structure, and bios - partition w/o filesystem
	for i, imgName := range []string{"mbr.img", "bios.img"} {
		imgPath := filepath.Join(newData.RootDir, imgName)
		err = os.WriteFile(imgPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i].Content = []gadget.VolumeContent{{Image: imgName}}
		newData.Info.Volumes["pc"].Structure[i].Update.Edition = 1
	}
	// ubuntu-{seed,boot,save,data}
	for i, fName := range []string{"seed", "boot", "save", "data"} {
		fPath := filepath.Join(newData.RootDir, fName)
		err = os.WriteFile(fPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["pc"].Structure[i+2].Content = []gadget.VolumeContent{{UnresolvedSource: fName}}
		newData.Info.Volumes["pc"].Structure[i+2].Update.Edition = 1
	}
	// foo
	for i, imgName := range []string{"bare", "part"} {
		imgPath := filepath.Join(newData.RootDir, imgName)
		err = os.WriteFile(imgPath, nil, 0644)
		c.Assert(err, IsNil)
		newData.Info.Volumes["foo"].Structure[i].Content = []gadget.VolumeContent{{Image: imgName}}
		newData.Info.Volumes["foo"].Structure[i].Update.Edition = 1
	}
	fName := "some-file"
	fPath := filepath.Join(newData.RootDir, fName)
	err = os.WriteFile(fPath, nil, 0644)
	c.Assert(err, IsNil)
	newData.Info.Volumes["foo"].Structure[2].Content = []gadget.VolumeContent{{UnresolvedSource: fName}}
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 1

	muo := &mockUpdateProcessObserver{}
	pcUpdaterForStructureCalls := 0
	fooUpdaterForStructureCalls := 0
	pcUpdateCalls := make(map[string]bool)
	pcBackupCalls := make(map[string]bool)
	fooUpdateCalls := make(map[string]bool)
	fooBackupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		var mu *mockUpdater

		switch ps.VolumeStructure.VolumeName {
		case "pc":
			switch pcUpdaterForStructureCalls {
			case 0:
				// mbr raw structure
				c.Check(ps.Name(), Equals, "mbr")
				c.Check(ps.HasFilesystem(), Equals, false)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(440))
				c.Check(ps.IsPartition(), Equals, false)
				// no offset since we are updating the MBR itself
				c.Check(ps.StartOffset, Equals, quantity.Offset(0))
				c.Assert(ps.LaidOutContent, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					Device: "/dev/vda",
					Offset: quantity.Offset(0),
				})
			case 1:
				// bios boot
				c.Check(ps.Name(), Equals, "BIOS Boot")
				c.Check(ps.HasFilesystem(), Equals, false)
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeMiB)
				c.Check(ps.StartOffset, Equals, quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 1)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					Device: "/dev/vda",
					Offset: quantity.OffsetMiB,
				})
			case 5:
				// ubuntu-seed
				c.Check(ps.Name(), Equals, "ubuntu-seed")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "vfat")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, 1200*quantity.SizeMiB)
				c.Check(ps.StartOffset, Equals, (1+1)*quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed"),
				})
			case 4:
				// ubuntu-boot
				c.Check(ps.Name(), Equals, "ubuntu-boot")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "ext4")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, 750*quantity.SizeMiB)
				c.Check(ps.StartOffset, Equals, (1+1+1200)*quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot"),
				})
			case 2:
				// ubuntu-save
				c.Check(ps.Name(), Equals, "ubuntu-save")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "ext4")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, 16*quantity.SizeMiB)
				c.Check(ps.StartOffset, Equals, (1+1+1200+750)*quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save"),
				})
			case 3:
				// ubuntu-data
				c.Check(ps.Name(), Equals, "ubuntu-data")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "ext4")
				c.Check(ps.IsPartition(), Equals, true)
				// NOTE: this is the laid out size, not the actual size (since data
				// gets expanded), but the update op doesn't actually care about the
				// size so it's okay
				c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeGiB)
				c.Check(ps.StartOffset, Equals, (1+1+1200+750+16)*quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data"),
				})
			}
			pcUpdaterForStructureCalls++

			mu = &mockUpdater{
				backupCb: func() error {
					pcBackupCalls[ps.Name()] = true
					return nil
				},
				updateCb: func() error {
					pcUpdateCalls[ps.Name()] = true
					return nil
				},
				rollbackCb: func() error {
					c.Fatalf("unexpected call")
					return errors.New("not called")
				},
			}
		case "foo":
			switch fooUpdaterForStructureCalls {
			case 0:
				c.Check(ps.Name(), Equals, "barething")
				c.Check(ps.HasFilesystem(), Equals, false)
				c.Check(ps.IsPartition(), Equals, false)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(4096))
				c.Check(ps.StartOffset, Equals, quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 1)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					Device: "/dev/vdb",
					Offset: quantity.OffsetMiB,
				})
			case 1:
				c.Check(ps.Name(), Equals, "nofspart")
				c.Check(ps.HasFilesystem(), Equals, false)
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.Size(4096))
				c.Check(ps.StartOffset, Equals, quantity.OffsetMiB+4096)
				c.Assert(ps.LaidOutContent, HasLen, 1)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					Device: "/dev/vdb",
					Offset: quantity.OffsetMiB + 4096,
				})
			case 2:
				c.Check(ps.Name(), Equals, "some-filesystem")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "ext4")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeGiB)
				c.Check(ps.StartOffset, Equals, quantity.OffsetMiB+4096+4096)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/foo/some-filesystem"),
				})
			default:
				c.Fatalf("unexpected call")
			}
			fooUpdaterForStructureCalls++

			mu = &mockUpdater{
				backupCb: func() error {
					fooBackupCalls[ps.Name()] = true
					return nil
				},
				updateCb: func() error {
					fooUpdateCalls[ps.Name()] = true
					return nil
				},
				rollbackCb: func() error {
					c.Fatalf("unexpected call")
					return errors.New("not called")
				},
			}
		}

		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(pcUpdaterForStructureCalls, Equals, 6)
	c.Assert(fooUpdaterForStructureCalls, Equals, 3)
	c.Assert(pcBackupCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"BIOS Boot":   true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
		"ubuntu-data": true,
	})
	c.Assert(pcUpdateCalls, DeepEquals, map[string]bool{
		"mbr":         true,
		"BIOS Boot":   true,
		"ubuntu-seed": true,
		"ubuntu-boot": true,
		"ubuntu-save": true,
		"ubuntu-data": true,
	})

	c.Assert(fooBackupCalls, DeepEquals, map[string]bool{
		"barething":       true,
		"nofspart":        true,
		"some-filesystem": true,
	})
	c.Assert(fooUpdateCalls, DeepEquals, map[string]bool{
		"barething":       true,
		"nofspart":        true,
		"some-filesystem": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUC20WithInitialMapIncompatibleStructureChangesOnMultiVolumeUpdate(c *C) {
	u.restoreVolumeStructureToLocationMap()

	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir: c.MkDir(),
	}

	rollbackDir := c.MkDir()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.MultiVolumeUC20GadgetYaml, uc20Model)
	c.Assert(err, IsNil)

	err = os.MkdirAll(dirs.SnapDeviceDir, 0755)
	c.Assert(err, IsNil)
	// write out the provided traits JSON so we can at least load the traits for
	// mocking via setupForVolumeStructureToLocation
	err = os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.VMMultiVolumeUC20DiskTraitsJSON),
		0644,
	)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						Device: "/dev/vda",
					},
					1: {
						Device: "/dev/vda",
						Offset: quantity.OffsetMiB,
					},
					2: {
						RootMountPoint: "/run/mnt/ubuntu-seed",
					},
					3: {
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
					4: {
						RootMountPoint: "/run/mnt/ubuntu-save",
					},
					5: {
						RootMountPoint: "/run/mnt/data",
					},
				},
				"foo": {
					0: {},
					1: {
						Device: "/dev/vdb1",
					},
					2: {
						Device: "/dev/vdb2",
					},
				},
			},
			map[string]map[int]*gadget.OnDiskStructure{
				"pc":  gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			}, nil
	})
	defer r()

	// change the new nofspart structure size which is an incompatible change

	// nofspart
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[1].MinSize = quantity.SizeKiB
	newData.Info.Volumes["foo"].Structure[1].Size = quantity.SizeKiB
	imgName := "img"
	imgPath := filepath.Join(newData.RootDir, imgName)
	err = os.WriteFile(imgPath, nil, 0644)
	c.Assert(err, IsNil)
	newData.Info.Volumes["foo"].Structure[1].Content = []gadget.VolumeContent{{Image: imgName}}

	muo := &mockUpdateProcessObserver{}
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Fatalf("unexpected call")
		return nil, errors.New("not called")
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot update volume structure #1 \("nofspart"\) for volume foo: new valid structure size range \[1024, 1024\] is not compatible with current \(\[4096, 4096\]\)`)
}

func (u *updateTestSuite) TestUpdateApplyUC20KernelAssetsOnAllVolumesWithInitialMapAllVolumesUpdatedFullLogic(c *C) {
	u.restoreVolumeStructureToLocationMap()

	oldKernelDir := c.MkDir()
	newKernelDir := c.MkDir()

	kernelYaml := []byte(`assets:
  ref:
    update: true
    content:
      - kernel-content
      - kernel-content2`)
	makeSizedFile(c, filepath.Join(newKernelDir, "meta/kernel.yaml"), 0, kernelYaml)
	makeSizedFile(c, filepath.Join(oldKernelDir, "meta/kernel.yaml"), 0, kernelYaml)

	makeSizedFile(c, filepath.Join(newKernelDir, "kernel-content"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(newKernelDir, "kernel-content2"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldKernelDir, "kernel-content"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldKernelDir, "kernel-content2"), quantity.SizeMiB, nil)

	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir:       c.MkDir(),
		KernelRootDir: oldKernelDir,
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir:       c.MkDir(),
		KernelRootDir: newKernelDir,
	}

	rollbackDir := c.MkDir()

	const multiVolWithKernel = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: $kernel:ref/kernel-content
            target: /
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
  foo:
    schema: gpt
    structure:
      - name: barething
        type: bare
        size: 4096
      - name: nofspart
        type: A11D2A7C-D82A-4C2F-8A01-1805240E6626
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
        content:
          - source: $kernel:ref/kernel-content2
            target: /
`

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), oldKernelDir, multiVolWithKernel, uc20Model)
	c.Assert(err, IsNil)

	err = os.MkdirAll(dirs.SnapDeviceDir, 0755)
	c.Assert(err, IsNil)
	// write out the provided traits JSON so we can at least load the traits for
	// mocking via setupForVolumeStructureToLocation
	err = os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.VMMultiVolumeUC20DiskTraitsJSON),
		0644,
	)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("BIOS Boot")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
		filepath.Join(dirs.GlobalRootDir, "/dev/vdb1"): gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
		"/dev/vdb": gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily

	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 525:3 / %[1]s/foo/some-filesystem rw,relatime shared:7 - vfat %[1]s/dev/vdb2 rw
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	// update the kernel asset referencing structures

	// ubuntu-seed
	newData.Info.Volumes["pc"].Structure[2].Update.Edition = 1

	// some filesystem
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 1

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"pc": {
					0: {
						Device: "/dev/vda",
					},
					1: {
						Device: "/dev/vda",
						Offset: quantity.OffsetMiB,
					},
					2: {
						RootMountPoint: "/run/mnt/ubuntu-seed",
					},
					3: {
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
					4: {
						RootMountPoint: "/run/mnt/ubuntu-save",
					},
					5: {
						RootMountPoint: "/run/mnt/data",
					},
				},
				"foo": {
					0: {
						Device: "/dev/vdb",
					},
					1: {
						Device: "/dev/vdb",
						Offset: quantity.OffsetMiB,
					},
					2: {
						RootMountPoint: "/foo/some-filesystem",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"pc":  gadget.OnDiskStructsFromGadget(gd.Info.Volumes["pc"]),
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	pcUpdaterForStructureCalls := 0
	fooUpdaterForStructureCalls := 0
	pcUpdateCalls := make(map[string]bool)
	pcBackupCalls := make(map[string]bool)
	fooUpdateCalls := make(map[string]bool)
	fooBackupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		var mu *mockUpdater

		switch ps.VolumeStructure.VolumeName {
		case "pc":
			switch pcUpdaterForStructureCalls {
			case 0:
				// ubuntu-seed
				c.Check(ps.Name(), Equals, "ubuntu-seed")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "vfat")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, 1200*quantity.SizeMiB)
				c.Check(ps.StartOffset, Equals, (1+1)*quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, DeepEquals, []gadget.VolumeContent{
					{
						UnresolvedSource: "$kernel:ref/kernel-content",
						Target:           "/",
					},
				})
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: "/run/mnt/ubuntu-seed",
				})
			}
			pcUpdaterForStructureCalls++

			mu = &mockUpdater{
				backupCb: func() error {
					pcBackupCalls[ps.Name()] = true
					return nil
				},
				updateCb: func() error {
					pcUpdateCalls[ps.Name()] = true
					return nil
				},
				rollbackCb: func() error {
					c.Fatalf("unexpected call")
					return errors.New("not called")
				},
			}
		case "foo":
			switch fooUpdaterForStructureCalls {
			case 0:
				c.Check(ps.Name(), Equals, "some-filesystem")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "ext4")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeGiB)
				c.Check(ps.StartOffset, Equals, quantity.OffsetMiB+4096+4096)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, DeepEquals, []gadget.VolumeContent{
					{
						UnresolvedSource: "$kernel:ref/kernel-content2",
						Target:           "/",
					},
				})
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: "/foo/some-filesystem",
				})
			default:
				c.Fatalf("unexpected call")
			}
			fooUpdaterForStructureCalls++

			mu = &mockUpdater{
				backupCb: func() error {
					fooBackupCalls[ps.Name()] = true
					return nil
				},
				updateCb: func() error {
					fooUpdateCalls[ps.Name()] = true
					return nil
				},
				rollbackCb: func() error {
					c.Fatalf("unexpected call")
					return errors.New("not called")
				},
			}
		}

		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(pcUpdaterForStructureCalls, Equals, 1)
	c.Assert(fooUpdaterForStructureCalls, Equals, 1)
	c.Assert(pcBackupCalls, DeepEquals, map[string]bool{
		"ubuntu-seed": true,
	})
	c.Assert(pcUpdateCalls, DeepEquals, map[string]bool{
		"ubuntu-seed": true,
	})

	c.Assert(fooBackupCalls, DeepEquals, map[string]bool{
		"some-filesystem": true,
	})
	c.Assert(fooUpdateCalls, DeepEquals, map[string]bool{
		"some-filesystem": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUC20KernelAssetsOnSingleVolumeWithInitialMapAllVolumesUpdatedFullLogic(c *C) {
	u.restoreVolumeStructureToLocationMap()

	oldKernelDir := c.MkDir()
	newKernelDir := c.MkDir()

	kernelYaml := []byte(`assets:
  ref:
    update: true
    content:
      - kernel-content
      - kernel-content2`)
	makeSizedFile(c, filepath.Join(newKernelDir, "meta/kernel.yaml"), 0, kernelYaml)
	makeSizedFile(c, filepath.Join(oldKernelDir, "meta/kernel.yaml"), 0, kernelYaml)

	makeSizedFile(c, filepath.Join(newKernelDir, "kernel-content"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(newKernelDir, "kernel-content2"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldKernelDir, "kernel-content"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldKernelDir, "kernel-content2"), quantity.SizeMiB, nil)

	oldData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir:       c.MkDir(),
		KernelRootDir: oldKernelDir,
	}

	newData := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{},
		},
		RootDir:       c.MkDir(),
		KernelRootDir: newKernelDir,
	}

	rollbackDir := c.MkDir()

	const multiVolWithKernel = `
volumes:
  pc:
    schema: gpt
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: $kernel:ref/kernel-content
            target: /
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
  foo:
    schema: gpt
    structure:
      - name: barething
        type: bare
        size: 4096
      - name: nofspart
        type: A11D2A7C-D82A-4C2F-8A01-1805240E6626
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
        content:
          - source: some-file
            target: /
`

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), oldKernelDir, multiVolWithKernel, uc20Model)
	c.Assert(err, IsNil)

	err = os.MkdirAll(dirs.SnapDeviceDir, 0755)
	c.Assert(err, IsNil)
	// write out the provided traits JSON so we can at least load the traits for
	// mocking via setupForVolumeStructureToLocation
	err = os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.VMMultiVolumeUC20DiskTraitsJSON),
		0644,
	)
	c.Assert(err, IsNil)

	// put the same volumes into both the old and the new data so they are
	// identical to start
	for volName, laidOutVol := range allLaidOutVolumes {
		// need to make separate copies of the volume since laidOUutVol.Volume
		// is a pointer
		numStructures := len(laidOutVol.Volume.Structure)
		newData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(newData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)

		oldData.Info.Volumes[volName] = &gadget.Volume{
			Schema:     laidOutVol.Volume.Schema,
			Bootloader: laidOutVol.Volume.Bootloader,
			ID:         laidOutVol.Volume.ID,
			Structure:  make([]gadget.VolumeStructure, numStructures),
			Name:       laidOutVol.Volume.Name,
		}
		copy(oldData.Info.Volumes[volName].Structure, laidOutVol.Volume.Structure)
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("BIOS Boot")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
		filepath.Join(dirs.GlobalRootDir, "/dev/vdb1"): gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
		"/dev/vdb": gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily

	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 525:3 / %[1]s/foo/some-filesystem rw,relatime shared:7 - vfat %[1]s/dev/vdb2 rw
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	// update the kernel asset referencing structures

	// ubuntu-seed
	newData.Info.Volumes["pc"].Structure[2].Update.Edition = 1

	// some filesystem
	someFile := filepath.Join(newData.RootDir, "some-file")
	err = os.WriteFile(someFile, nil, 0644)
	c.Assert(err, IsNil)
	newData.Info.Volumes["foo"].Structure[2].Content = []gadget.VolumeContent{{UnresolvedSource: "some-file"}}
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 1

	muo := &mockUpdateProcessObserver{}
	pcUpdaterForStructureCalls := 0
	fooUpdaterForStructureCalls := 0
	pcUpdateCalls := make(map[string]bool)
	pcBackupCalls := make(map[string]bool)
	fooUpdateCalls := make(map[string]bool)
	fooBackupCalls := make(map[string]bool)
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		var mu *mockUpdater

		switch ps.VolumeStructure.VolumeName {
		case "pc":
			switch pcUpdaterForStructureCalls {
			case 0:
				// ubuntu-seed
				c.Check(ps.Name(), Equals, "ubuntu-seed")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "vfat")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, 1200*quantity.SizeMiB)
				c.Check(ps.StartOffset, Equals, (1+1)*quantity.OffsetMiB)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, DeepEquals, []gadget.VolumeContent{
					{
						UnresolvedSource: "$kernel:ref/kernel-content",
						Target:           "/",
					},
				})
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed"),
				})
			}
			pcUpdaterForStructureCalls++

			mu = &mockUpdater{
				backupCb: func() error {
					pcBackupCalls[ps.Name()] = true
					return nil
				},
				updateCb: func() error {
					pcUpdateCalls[ps.Name()] = true
					return nil
				},
				rollbackCb: func() error {
					c.Fatalf("unexpected call")
					return errors.New("not called")
				},
			}
		case "foo":
			switch fooUpdaterForStructureCalls {
			case 0:
				c.Check(ps.Name(), Equals, "some-filesystem")
				c.Check(ps.HasFilesystem(), Equals, true)
				c.Check(ps.Filesystem(), Equals, "ext4")
				c.Check(ps.IsPartition(), Equals, true)
				c.Check(ps.VolumeStructure.Size, Equals, quantity.SizeGiB)
				c.Check(ps.StartOffset, Equals, quantity.OffsetMiB+4096+4096)
				c.Assert(ps.LaidOutContent, HasLen, 0)
				c.Assert(ps.VolumeStructure.Content, HasLen, 1)
				c.Assert(loc, Equals, gadget.StructureLocation{
					RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/foo/some-filesystem"),
				})
			default:
				c.Fatalf("unexpected call")
			}
			fooUpdaterForStructureCalls++

			mu = &mockUpdater{
				backupCb: func() error {
					fooBackupCalls[ps.Name()] = true
					return nil
				},
				updateCb: func() error {
					fooUpdateCalls[ps.Name()] = true
					return nil
				},
				rollbackCb: func() error {
					c.Fatalf("unexpected call")
					return errors.New("not called")
				},
			}
		}

		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc20Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(pcUpdaterForStructureCalls, Equals, 1)
	c.Assert(fooUpdaterForStructureCalls, Equals, 1)
	c.Assert(pcBackupCalls, DeepEquals, map[string]bool{
		"ubuntu-seed": true,
	})
	c.Assert(pcUpdateCalls, DeepEquals, map[string]bool{
		"ubuntu-seed": true,
	})

	c.Assert(fooBackupCalls, DeepEquals, map[string]bool{
		"some-filesystem": true,
	})
	c.Assert(fooUpdateCalls, DeepEquals, map[string]bool{
		"some-filesystem": true,
	})

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyOnlyWhenNeeded(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// first structure is updated
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 0
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	// second one is not, lower edition
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	// third one is not, same edition
	oldData.Info.Volumes["foo"].Structure[2].Update.Edition = 3
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)

		switch updaterForStructureCalls {
		case 0:
			// only called for the first structure
			c.Assert(ps.Name(), Equals, "first")
		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			rollbackCb: func() error {
				c.Fatalf("unexpected call")
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyErrorLayout(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name:   "foo",
		Offset: asOffsetPtr(0),
		Size:   5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	bareStructUpdate := bareStruct
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct},
			},
		},
	}
	newInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStructUpdate},
			},
		},
	}
	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"foo": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	newRootDir := c.MkDir()
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}
	gadget.SetEnclosingVolumeInStructs(newData.Info.Volumes)

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}
	gadget.SetEnclosingVolumeInStructs(oldData.Info.Volumes)

	rollbackDir := c.MkDir()

	// both old and new bare struct data is missing

	// cannot lay out the new volume when bare struct data is missing
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot lay out the new volume foo: cannot lay out structure #0 \("foo"\): content "first.img": .* no such file or directory`)

	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), quantity.SizeMiB, nil)

	// Update does not error out when when the bare struct data of the old volume is missing
	err = gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, Equals, gadget.ErrNoUpdate)
}

func (u *updateTestSuite) TestUpdateApplyErrorIllegalVolumeUpdate(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name:   "foo",
		Offset: asOffsetPtr(0),
		Size:   5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	bareStructUpdate := bareStruct
	bareStructUpdate.Name = "foo update"
	bareStructUpdate.Update.Edition = 1
	bareStructUpdate.Offset = asOffsetPtr(5 * quantity.OffsetMiB)

	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct},
			},
		},
	}
	newInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				// more structures than old
				Structure: []gadget.VolumeStructure{bareStruct, bareStructUpdate},
			},
		},
	}
	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"foo": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
				},
			},
			nil
	})
	defer r()

	newRootDir := c.MkDir()
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}
	gadget.SetEnclosingVolumeInStructs(newData.Info.Volumes)

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}
	gadget.SetEnclosingVolumeInStructs(oldData.Info.Volumes)

	rollbackDir := c.MkDir()

	makeSizedFile(c, filepath.Join(oldRootDir, "first.img"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), 900*quantity.SizeKiB, nil)

	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot apply update to volume foo: cannot change the number of structures within volume from 1 to 2`)
}

func (u *updateTestSuite) TestUpdateApplyErrorIllegalStructureUpdate(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name:   "foo",
		Offset: asOffsetPtr(0),
		Size:   5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	fsStruct := gadget.VolumeStructure{
		Name:       "foo",
		Filesystem: "ext4",
		Offset:     asOffsetPtr(0),
		Size:       5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/", Target: "/"},
		},
		Update: gadget.VolumeUpdate{Edition: 5},
	}
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct},
			},
		},
	}
	newInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{fsStruct},
			},
		},
	}
	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"foo": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	newRootDir := c.MkDir()
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}
	gadget.SetEnclosingVolumeInStructs(newData.Info.Volumes)

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}
	gadget.SetEnclosingVolumeInStructs(oldData.Info.Volumes)

	rollbackDir := c.MkDir()

	makeSizedFile(c, filepath.Join(oldRootDir, "first.img"), quantity.SizeMiB, nil)

	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot update volume structure #0 \("foo"\) for volume foo: cannot change a bare structure to filesystem one`)
}

func (u *updateTestSuite) TestUpdateApplyErrorRenamedVolume(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name: "foo",
		Size: 5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct},
			},
		},
	}
	newInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			// same volume info but using a different name which ends up being
			// counted as having the old one removed and a new one added
			"foo-new": oldInfo.Volumes["foo"],
		},
	}

	oldData := gadget.GadgetData{Info: oldInfo, RootDir: c.MkDir()}
	newData := gadget.GadgetData{Info: newInfo, RootDir: c.MkDir()}
	rollbackDir := c.MkDir()

	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Fatalf("unexpected call")
		return &mockUpdater{}, nil
	})
	defer restore()

	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot update gadget assets: volumes were both added and removed`)
}

func (u *updateTestSuite) TestUpdateApplyUpdatesAreOptInWithDefaultPolicy(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name:   "foo",
		Offset: asOffsetPtr(0),
		Size:   5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
		Update: gadget.VolumeUpdate{
			Edition: 5,
		},
	}
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct},
			},
		},
	}

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}
	makeSizedFile(c, filepath.Join(oldRootDir, "first.img"), quantity.SizeMiB, nil)
	gadget.SetEnclosingVolumeInStructs(oldData.Info.Volumes)

	newRootDir := c.MkDir()
	// same volume description
	newData := gadget.GadgetData{Info: oldInfo, RootDir: newRootDir}
	// different content, but updates are opt in
	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), 900*quantity.SizeKiB, nil)
	gadget.SetEnclosingVolumeInStructs(newData.Info.Volumes)

	rollbackDir := c.MkDir()

	muo := &mockUpdateProcessObserver{}

	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Fatalf("unexpected call")
		return &mockUpdater{}, nil
	})
	defer restore()

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"foo": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
				},
			}, nil
	})
	defer r()

	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, Equals, gadget.ErrNoUpdate)

	// nothing was updated
	c.Assert(muo.beforeWriteCalled, Equals, 0)
}

func (u *updateTestSuite) policyDataSet(c *C) (oldData gadget.GadgetData, newData gadget.GadgetData, rollbackDir string) {
	oldData, newData, rollbackDir = u.updateDataSet(c)
	noPartitionStruct := gadget.VolumeStructure{
		VolumeName: "foo",
		Name:       "no-partition",
		Type:       "bare",
		Offset:     asOffsetPtr((1 + 5 + 10 + 5) * quantity.OffsetMiB),
		MinSize:    5 * quantity.SizeMiB,
		Size:       5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	mbrStruct := gadget.VolumeStructure{
		VolumeName: "foo",
		Name:       "mbr",
		Role:       "mbr",
		MinSize:    446,
		Size:       446,
		Offset:     asOffsetPtr(0),
	}

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		// XXX map
		return map[string]map[int]gadget.StructureLocation{
			"foo": {
				0: {
					Device: "/dev/foo",
					Offset: quantity.OffsetMiB,
				},
				1: {
					RootMountPoint: "/foo",
				},
				2: {
					RootMountPoint: "/foo",
				},
				3: {
					Device: "/dev/foo",
					Offset: 10000,
				},
				4: {
					Device: "/dev/foo",
					Offset: 0,
				},
			},
		}, nil, nil
	})
	u.AddCleanup(r)

	oldVol := oldData.Info.Volumes["foo"]
	oldStructs := oldVol.Structure
	oldVol.Structure = []gadget.VolumeStructure{mbrStruct}
	oldVol.Structure = append(oldVol.Structure, oldStructs...)
	oldVol.Structure = append(oldVol.Structure, noPartitionStruct)
	oldData.Info.Volumes["foo"] = oldVol
	gadget.SetEnclosingVolumeInStructs(oldData.Info.Volumes)

	newVol := newData.Info.Volumes["foo"]
	newStructs := newVol.Structure
	newVol.Structure = []gadget.VolumeStructure{mbrStruct}
	newVol.Structure = append(newVol.Structure, newStructs...)
	newVol.Structure = append(newVol.Structure, noPartitionStruct)
	newData.Info.Volumes["foo"] = newVol
	gadget.SetEnclosingVolumeInStructs(newData.Info.Volumes)

	c.Assert(oldData.Info.Volumes["foo"].Structure, HasLen, 5)
	c.Assert(newData.Info.Volumes["foo"].Structure, HasLen, 5)
	return oldData, newData, rollbackDir
}

func (u *updateTestSuite) TestUpdateApplyUpdatesArePolicyControlled(c *C) {
	oldData, newData, rollbackDir := u.policyDataSet(c)
	c.Assert(oldData.Info.Volumes["foo"].Structure, HasLen, 5)
	c.Assert(newData.Info.Volumes["foo"].Structure, HasLen, 5)
	// all structures have higher Edition, thus all would be updated under
	// the default policy
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3
	newData.Info.Volumes["foo"].Structure[3].Update.Edition = 4
	newData.Info.Volumes["foo"].Structure[4].Update.Edition = 5

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
					3: {
						RootMountPoint: "/foo",
					},
					4: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
					1: {},
					2: {},
					3: {},
					4: {},
				},
			},
			nil
	})
	defer r()

	toUpdate := map[string]int{}
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		toUpdate[ps.Name()]++
		return &mockUpdater{}, nil
	})
	defer restore()

	policySeen := map[string]int{}
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, func(_, to *gadget.LaidOutStructure) (bool, gadget.ResolvedContentFilterFunc) {
		policySeen[to.Name()]++
		return false, nil
	}, nil)
	c.Assert(err, Equals, gadget.ErrNoUpdate)
	c.Assert(policySeen, DeepEquals, map[string]int{
		"first":        1,
		"second":       1,
		"third":        1,
		"no-partition": 1,
		"mbr":          1,
	})
	c.Assert(toUpdate, DeepEquals, map[string]int{})

	// try with different policy
	policySeen = map[string]int{}
	err = gadget.Update(uc16Model, oldData, newData, rollbackDir, func(_, to *gadget.LaidOutStructure) (bool, gadget.ResolvedContentFilterFunc) {
		policySeen[to.Name()]++
		return to.Name() == "second", nil
	}, nil)
	c.Assert(err, IsNil)
	c.Assert(policySeen, DeepEquals, map[string]int{
		"first":        1,
		"second":       1,
		"third":        1,
		"no-partition": 1,
		"mbr":          1,
	})
	c.Assert(toUpdate, DeepEquals, map[string]int{
		"second": 1,
	})
}

func (u *updateTestSuite) TestUpdateApplyUpdatesDefaultPolicy(c *C) {
	oldData, newData, rollbackDir := u.policyDataSet(c)
	c.Assert(oldData.Info.Volumes["foo"].Structure, HasLen, 5)
	c.Assert(newData.Info.Volumes["foo"].Structure, HasLen, 5)

	c.Check(oldData.Info.Volumes["foo"].Structure[1].Name, Equals, "first")
	// no previous edition specified in the structure, as if it was not
	// specified in gadget.yaml
	c.Assert(oldData.Info.Volumes["foo"].Structure[1].Update, DeepEquals, gadget.VolumeUpdate{})
	// new one has edition set explicitly
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 5

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
					3: {
						RootMountPoint: "/foo",
					},
					4: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
					1: {},
					2: {},
					3: {},
					4: {},
				},
			},
			nil
	})
	defer r()

	toUpdate := map[string]int{}
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		toUpdate[ps.Name()]++
		return &mockUpdater{}, nil
	})
	defer restore()

	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, IsNil)
	c.Assert(toUpdate, DeepEquals, map[string]int{
		// the structure was picked for update
		"first": 1,
	})
}

func (u *updateTestSuite) TestUpdateApplyUpdatesRemodelPolicy(c *C) {
	oldData, newData, rollbackDir := u.policyDataSet(c)

	// old structures have higher Edition, no update would occur under the default policy
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	oldData.Info.Volumes["foo"].Structure[2].Update.Edition = 3
	oldData.Info.Volumes["foo"].Structure[3].Update.Edition = 4
	oldData.Info.Volumes["foo"].Structure[4].Update.Edition = 5

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
					3: {
						RootMountPoint: "/foo",
					},
					4: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
					1: {},
					2: {},
					3: {},
					4: {},
				},
			},
			nil
	})
	defer r()

	toUpdate := map[string]int{}
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		toUpdate[ps.Name()] = toUpdate[ps.Name()] + 1
		return &mockUpdater{}, nil
	})
	defer restore()

	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, gadget.RemodelUpdatePolicy, nil)
	c.Assert(err, IsNil)
	c.Assert(toUpdate, DeepEquals, map[string]int{
		"first":        1,
		"second":       1,
		"third":        1,
		"no-partition": 1,
		// 'mbr' is skipped by the remodel update
	})
}

func (u *updateTestSuite) TestUpdateApplyBackupFails(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// update both structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
					1: {},
					2: {},
				},
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			updateCb: func() error {
				c.Fatalf("unexpected update call")
				return errors.New("not called")
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected rollback call")
				return errors.New("not called")
			},
		}
		if updaterForStructureCalls == 1 {
			c.Assert(ps.Name(), Equals, "second")
			updater.backupCb = func() error {
				return errors.New("failed")
			}
		}
		updaterForStructureCalls++
		return updater, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot backup volume structure #1 \("second"\) on volume foo: failed`)

	// update was canceled before backup pass completed
	c.Check(muo.canceledCalled, Equals, 1)
	c.Check(muo.beforeWriteCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUpdateFailsThenRollback(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// update all structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
					1: {},
					2: {},
				},
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	rollbackCalls := make(map[string]bool)
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				rollbackCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
		}
		if updaterForStructureCalls == 1 {
			c.Assert(ps.Name(), Equals, "second")
			// fail update of 2nd structure
			updater.updateCb = func() error {
				updateCalls[ps.Name()] = true
				return errors.New("failed")
			}
		}
		updaterForStructureCalls++
		return updater, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot update volume structure #1 \("second"\) on volume foo: failed`)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		// all were backed up
		"first":  true,
		"second": true,
		"third":  true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"first":  true,
		"second": true,
		// third was never updated, as second failed
	})
	c.Assert(rollbackCalls, DeepEquals, map[string]bool{
		"first":  true,
		"second": true,
		// third does not need as it was not updated
	})
	// backup pass completed
	c.Check(muo.beforeWriteCalled, Equals, 1)
	// and then the update was canceled
	c.Check(muo.canceledCalled, Equals, 1)
}

func (u *updateTestSuite) TestUpdateApplyUpdateErrorRollbackFail(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	oldData, newData, rollbackDir := u.updateDataSet(c)
	// update all structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	rollbackCalls := make(map[string]bool)
	updaterForStructureCalls := 0
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name()] = true
				return nil
			},
			rollbackCb: func() error {
				rollbackCalls[ps.Name()] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name()] = true
				return nil
			},
		}
		switch updaterForStructureCalls {
		case 1:
			c.Assert(ps.Name(), Equals, "second")
			// rollback fails on 2nd structure
			updater.rollbackCb = func() error {
				rollbackCalls[ps.Name()] = true
				return errors.New("rollback failed with different error")
			}
		case 2:
			c.Assert(ps.Name(), Equals, "third")
			// fail update of 3rd structure
			updater.updateCb = func() error {
				updateCalls[ps.Name()] = true
				return errors.New("update error")
			}
		}
		updaterForStructureCalls++
		return updater, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	// preserves update error
	c.Assert(err, ErrorMatches, `cannot update volume structure #2 \("third"\) on volume foo: update error`)
	c.Assert(backupCalls, DeepEquals, map[string]bool{
		// all were backed up
		"first":  true,
		"second": true,
		"third":  true,
	})
	c.Assert(updateCalls, DeepEquals, map[string]bool{
		"first":  true,
		"second": true,
		"third":  true,
	})
	c.Assert(rollbackCalls, DeepEquals, map[string]bool{
		"first":  true,
		"second": true,
		"third":  true,
	})

	c.Check(logbuf.String(), testutil.Contains, `cannot update gadget: cannot update volume structure #2 ("third") on volume foo: update error`)
	c.Check(logbuf.String(), testutil.Contains, `cannot rollback volume structure #1 ("second") update on volume foo: rollback failed with different error`)
}

func (u *updateTestSuite) TestUpdateApplyBadUpdater(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// update all structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
					1: {},
					2: {},
				},
			},
			nil
	})
	defer r()

	r = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		return nil, errors.New("bad updater for structure")
	})
	defer r()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot prepare update for volume structure #0 \("first"\) on volume foo: bad updater for structure`)
}

func (u *updateTestSuite) TestUpdaterForStructure(c *C) {
	gadgetRootDir := c.MkDir()
	rollbackDir := c.MkDir()
	rootDir := c.MkDir()

	dirs.SetRootDir(rootDir)
	defer dirs.SetRootDir("/")

	// prepare some state for mocked mount point lookup
	err := os.MkdirAll(filepath.Join(rootDir, "/dev"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(rootDir, "/dev/disk/by-label"), 0755)
	c.Assert(err, IsNil)
	fakedevice := filepath.Join(rootDir, "/dev/sdxxx2")
	err = os.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(rootDir, "/dev/disk/by-label/writable"))
	c.Assert(err, IsNil)
	mountInfo := `170 27 8:2 / /some/mount/point rw,relatime shared:58 - ext4 %s/dev/sdxxx2 rw
`
	restore := osutil.MockMountInfo(fmt.Sprintf(mountInfo, rootDir))
	defer restore()

	psBare := &gadget.LaidOutStructure{
		OnDiskStructure: gadget.OnDiskStructure{StartOffset: 1 * quantity.OffsetMiB},
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem:      "none",
			Size:            10 * quantity.SizeMiB,
			EnclosingVolume: &gadget.Volume{},
		},
	}
	updater, err := gadget.UpdaterForStructure(gadget.StructureLocation{}, psBare, gadgetRootDir, rollbackDir, nil)
	c.Assert(err, IsNil)
	c.Assert(updater, FitsTypeOf, &gadget.RawStructureUpdater{})

	psFs := &gadget.LaidOutStructure{
		OnDiskStructure: gadget.OnDiskStructure{StartOffset: 1 * quantity.OffsetMiB},
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem:      "ext4",
			Size:            10 * quantity.SizeMiB,
			Label:           "writable",
			EnclosingVolume: &gadget.Volume{},
		},
	}
	updater, err = gadget.UpdaterForStructure(gadget.StructureLocation{RootMountPoint: "/"}, psFs, gadgetRootDir, rollbackDir, nil)
	c.Assert(err, IsNil)
	c.Assert(updater, FitsTypeOf, &gadget.MountedFilesystemUpdater{})

	// trigger errors
	updater, err = gadget.UpdaterForStructure(gadget.StructureLocation{Device: "/dev/vda"}, psBare, gadgetRootDir, "", nil)
	c.Assert(err, ErrorMatches, "internal error: backup directory cannot be unset")
	c.Assert(updater, IsNil)
}

func (u *updateTestSuite) TestUpdaterMultiVolumesAddedRemovedErrors(c *C) {
	multiVolume := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"1": {},
				"2": {},
			},
		},
	}
	singleVolume := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"1": {},
			},
		},
	}

	// an update to a gadget with multiple volumes when we had just a single one
	// before fails
	err := gadget.Update(uc16Model, singleVolume, multiVolume, "some-rollback-dir", nil, nil)
	c.Assert(err, ErrorMatches, "cannot update gadget assets: volumes were removed")

	// same for an update removing volumes
	err = gadget.Update(uc16Model, multiVolume, singleVolume, "some-rollback-dir", nil, nil)
	c.Assert(err, ErrorMatches, "cannot update gadget assets: volumes were added")
}

func (u *updateTestSuite) TestUpdateApplyNoChangedContentInAll(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// first structure is updated
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 0
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	// so is the second structure
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	expectedStructs := []string{"first", "second"}
	updateCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		mu := &mockUpdater{
			updateCb: func() error {
				c.Assert(expectedStructs, testutil.Contains, ps.Name())
				updateCalls++
				return gadget.ErrNoUpdate
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected rollback call for structure: %v", ps)
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, Equals, gadget.ErrNoUpdate)
	// update called for 2 structures
	c.Assert(updateCalls, Equals, 2)
	// nothing was updated, but the backup pass still executed
	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyNoChangedContentInSome(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	// first structure is updated
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 0
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	// so is the second structure
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	muo := &mockUpdateProcessObserver{}
	expectedStructs := []string{"first", "second"}
	updateCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		mu := &mockUpdater{
			updateCb: func() error {
				c.Assert(expectedStructs, testutil.Contains, ps.Name())
				updateCalls++
				if ps.Name() == "first" {
					return gadget.ErrNoUpdate
				}
				return nil
			},
			rollbackCb: func() error {
				c.Fatalf("unexpected rollback call for structure: %v", ps)
				return errors.New("not called")
			},
		}
		return mu, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	// update called for 2 structures
	c.Assert(updateCalls, Equals, 2)
	// at least one structure had an update
	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyObserverBeforeWriteErrs(c *C) {
	oldData, newData, rollbackDir := u.updateDataSet(c)
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			updateCb: func() error {
				c.Fatalf("unexpected call")
				return fmt.Errorf("unexpected call")
			},
		}
		return updater, nil
	})
	defer restore()

	// go go go
	muo := &mockUpdateProcessObserver{
		beforeWriteErr: errors.New("before write fail"),
	}
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot observe prepared update: before write fail`)
	// update was canceled before backup pass completed
	c.Check(muo.canceledCalled, Equals, 0)
	c.Check(muo.beforeWriteCalled, Equals, 1)
}

func (u *updateTestSuite) TestUpdateApplyObserverCanceledErrs(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	oldData, newData, rollbackDir := u.updateDataSet(c)
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1

	r := gadget.MockVolumeStructureToLocationMap(func(gd gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						Device: "/dev/foo",
						Offset: quantity.OffsetMiB,
					},
					1: {
						RootMountPoint: "/foo",
					},
					2: {
						RootMountPoint: "/foo",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"foo": gadget.OnDiskStructsFromGadget(gd.Info.Volumes["foo"]),
			},
			nil
	})
	defer r()

	backupErr := errors.New("backup fails")
	updateErr := errors.New("update fails")
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			backupCb: func() error { return backupErr },
			updateCb: func() error { return updateErr },
		}
		return updater, nil
	})
	defer restore()

	// go go go
	muo := &mockUpdateProcessObserver{
		canceledErr: errors.New("canceled fail"),
	}
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot backup volume structure #0 .*: backup fails`)
	// canceled called after backup pass
	c.Check(muo.canceledCalled, Equals, 1)
	c.Check(muo.beforeWriteCalled, Equals, 0)

	c.Check(logbuf.String(), testutil.Contains, `cannot observe canceled prepare update: canceled fail`)

	// backup works, update fails, triggers another canceled call
	backupErr = nil
	err = gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot update volume structure #0 .*: update fails`)
	// canceled called after backup pass
	c.Check(muo.canceledCalled, Equals, 2)
	c.Check(muo.beforeWriteCalled, Equals, 1)

	c.Check(logbuf.String(), testutil.Contains, `cannot observe canceled update: canceled fail`)
}

func (u *updateTestSuite) TestKernelUpdatePolicy(c *C) {
	for _, tc := range []struct {
		from, to *gadget.LaidOutStructure
		update   bool
	}{
		// trivial
		{
			from: &gadget.LaidOutStructure{},
			to: &gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{},
			},
			update: false,
		},
		// gadget content only, nothing for the kernel
		{
			from: &gadget.LaidOutStructure{},
			to: &gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Content: []gadget.VolumeContent{
						{UnresolvedSource: "something"},
					},
				},
			},
		},
		// ensure that only the `KernelUpdate` of the `to`
		// structure is relevant
		{
			from: &gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Content: []gadget.VolumeContent{
						{
							UnresolvedSource: "$kernel:ref",
						},
					},
				},
			},
			to: &gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{},
			},
			update: false,
		},
		// happy case, kernelUpdate is true
		{
			from: &gadget.LaidOutStructure{},
			to: &gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Content: []gadget.VolumeContent{
						{
							UnresolvedSource: "other",
						},
						{
							UnresolvedSource: "$kernel:ref",
						},
					},
				},
			},
			update: true,
		},
	} {
		needsUpdate, filter := gadget.KernelUpdatePolicy(tc.from, tc.to)
		if tc.update {
			c.Check(needsUpdate, Equals, true, Commentf("%v", tc))
			c.Check(filter, NotNil)
		} else {
			c.Check(needsUpdate, Equals, false, Commentf("%v", tc))
			c.Check(filter, IsNil)
		}
	}
}

func (u *updateTestSuite) TestKernelUpdatePolicyFunc(c *C) {
	from := &gadget.LaidOutStructure{}
	to := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Content: []gadget.VolumeContent{
				{
					UnresolvedSource: "other",
				},
				{
					UnresolvedSource: "$kernel:ref",
				},
			},
		},
		ResolvedContent: []gadget.ResolvedContent{
			{
				ResolvedSource: "/gadget/path/to/other",
			},
			{
				ResolvedSource: "/kernel/path/to/ref",
				KernelUpdate:   true,
			},
		},
	}
	needsUpdate, filter := gadget.KernelUpdatePolicy(from, to)
	c.Check(needsUpdate, Equals, true)
	c.Assert(filter, NotNil)
	c.Check(filter(&to.ResolvedContent[0]), Equals, false)
	c.Check(filter(&to.ResolvedContent[1]), Equals, true)
}

func (u *updateTestSuite) TestUpdateApplyUpdatesWithKernelPolicy(c *C) {
	// prepare the stage
	fsStruct := gadget.VolumeStructure{
		VolumeName: "foo",
		Name:       "foo",
		Offset:     asOffsetPtr(0),
		Size:       5 * quantity.SizeMiB,
		Filesystem: "ext4",
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/second-content", Target: "/"},
			{UnresolvedSource: "$kernel:ref/kernel-content", Target: "/"},
		},
	}
	vol := &gadget.Volume{
		Name:       "foo",
		Bootloader: "grub",
		Schema:     "gpt",
		Structure:  []gadget.VolumeStructure{fsStruct},
	}
	vol.Structure[0].EnclosingVolume = vol
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": vol,
		},
	}

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"foo": {
					0: {
						RootMountPoint: "/foo",
					},
				},
			},
			map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
				},
			},
			nil
	})
	defer r()

	oldRootDir := c.MkDir()
	oldKernelDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir, KernelRootDir: oldKernelDir}
	makeSizedFile(c, filepath.Join(oldRootDir, "some-content"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldKernelDir, "kernel-content"), quantity.SizeMiB, nil)

	newRootDir := oldRootDir
	newKernelDir := c.MkDir()
	kernelYamlFn := filepath.Join(newKernelDir, "meta/kernel.yaml")
	makeSizedFile(c, kernelYamlFn, 0, []byte(`
assets:
  ref:
    update: true
    content:
    - kernel-content`))

	// same volume description
	newData := gadget.GadgetData{Info: oldInfo, RootDir: newRootDir, KernelRootDir: newKernelDir}
	// different file from gadget
	makeSizedFile(c, filepath.Join(newRootDir, "some-content"), 2*quantity.SizeMiB, nil)
	// same file from kernel, it is still updated because kernel sets
	// the update flag
	makeSizedFile(c, filepath.Join(newKernelDir, "kernel-content"), quantity.SizeMiB, nil)

	rollbackDir := c.MkDir()
	muo := &mockUpdateProcessObserver{}

	// Check that filtering happened via the KernelUpdatePolicy and the
	// updater is only called with the kernel content, not with the
	// gadget content.
	mockUpdaterCalls := 0
	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		mockUpdaterCalls++
		c.Check(ps.ResolvedContent, DeepEquals, []gadget.ResolvedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					UnresolvedSource: "$kernel:ref/kernel-content",
					Target:           "/",
				},
				ResolvedSource: filepath.Join(newKernelDir, "kernel-content"),
				KernelUpdate:   true,
			},
		})
		return &mockUpdater{}, nil
	})
	defer restore()

	// exercise KernelUpdatePolicy here
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, gadget.KernelUpdatePolicy, muo)
	c.Assert(err, IsNil)

	// ensure update for kernel content happened
	c.Assert(mockUpdaterCalls, Equals, 1)
	c.Assert(muo.beforeWriteCalled, Equals, 1)
}

func (u *updateTestSuite) TestUpdateApplyUpdatesWithMissingKernelRefInGadget(c *C) {
	// kernel.yaml has "$kernel:ref" style content
	kernelYaml := []byte(`
assets:
  ref:
    update: true
    content:
    - kernel-content`)
	// but gadget.yaml does not have this, which violates kernel
	// update policy rule no. 1 from update.go
	fsStruct := gadget.VolumeStructure{
		Name:       "foo",
		Offset:     asOffsetPtr(0),
		Size:       5 * quantity.SizeMiB,
		Filesystem: "ext4",
		Content: []gadget.VolumeContent{
			// Note that there is no "$kernel:ref" here
			{UnresolvedSource: "/content", Target: "/"},
		},
	}
	info := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{fsStruct},
			},
		},
	}

	gadgetDir := c.MkDir()
	oldKernelDir := c.MkDir()
	oldData := gadget.GadgetData{Info: info, RootDir: gadgetDir, KernelRootDir: oldKernelDir}
	makeSizedFile(c, filepath.Join(gadgetDir, "some-content"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldKernelDir, "kernel-content"), quantity.SizeMiB, nil)

	newKernelDir := c.MkDir()
	kernelYamlFn := filepath.Join(newKernelDir, "meta/kernel.yaml")
	makeSizedFile(c, kernelYamlFn, 0, kernelYaml)

	newData := gadget.GadgetData{Info: info, RootDir: gadgetDir, KernelRootDir: newKernelDir}
	makeSizedFile(c, filepath.Join(gadgetDir, "content"), 2*quantity.SizeMiB, nil)
	rollbackDir := c.MkDir()
	muo := &mockUpdateProcessObserver{}

	restore := gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		panic("should not get called")
	})
	defer restore()

	r := gadget.MockVolumeStructureToLocationMap(func(_ gadget.GadgetData, _ gadget.Model, _ map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{"foo": {}},
			map[string]map[int]*gadget.OnDiskStructure{
				"foo": {
					0: {},
				},
			},
			nil
	})
	defer r()

	// exercise KernelUpdatePolicy here
	err := gadget.Update(uc16Model, oldData, newData, rollbackDir, gadget.KernelUpdatePolicy, muo)
	c.Assert(err, ErrorMatches, `gadget does not consume any of the kernel assets needing synced update "ref"`)

	// ensure update for kernel content didn't happen
	c.Assert(muo.beforeWriteCalled, Equals, 0)
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateWithBareStructure(c *C) {
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.MockExtraVolumeYAML, nil)
	c.Assert(err, IsNil)

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, IsNil)

	c.Assert(traits, DeepEquals, gadgettest.MockExtraVolumeDeviceTraits)
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateGPTSingleVolume(c *C) {
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": {
			DevNode: "/dev/foo",
			DevPath: "/sys/block/foo",
			DevNum:  "525:1",
			// assume 34 sectors at end for GPT headers backup
			DiskUsableSectorEnd: 6000*1024*1024/512 - 34,
			DiskSizeInBytes:     6000 * 1024 * 1024,
			SectorSizeBytes:     512,
			DiskSchema:          "gpt",
			ID:                  "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
			Structure: []disks.Partition{
				{
					PartitionLabel:   "nofspart",
					PartitionUUID:    "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
					FilesystemType:   "",
					Major:            525,
					Minor:            2,
					KernelDeviceNode: "/dev/foo1",
					KernelDevicePath: "/sys/block/foo/foo1",
					DiskIndex:        1,
					StartInBytes:     1024 * 1024,
					SizeInBytes:      4096,
				},
				{
					PartitionLabel:   "some-filesystem",
					PartitionUUID:    "DA2ADBC8-90DF-4B1D-A93F-A92516C12E01",
					FilesystemLabel:  "some-filesystem",
					FilesystemUUID:   "3E3D392C-5D50-4C84-8A6E-09B7A3FEA2C7",
					FilesystemType:   "ext4",
					Major:            525,
					Minor:            3,
					KernelDeviceNode: "/dev/foo2",
					KernelDevicePath: "/sys/block/foo/foo2",
					DiskIndex:        2,
					StartInBytes:     1024*1024 + 4096,
					SizeInBytes:      1024 * 1024 * 1024,
				},
			},
		},
	})
	defer restore()

	const yaml = `
volumes:
  foo:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: nofspart
        type: EBBEADAF-22C9-E33B-8F5D-0E81686A68CB
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`
	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), yaml, nil)
	c.Assert(err, IsNil)

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, IsNil)
	c.Assert(traits, DeepEquals, gadget.DiskVolumeDeviceTraits{
		OriginalDevicePath: "/sys/block/foo",
		OriginalKernelPath: "/dev/foo",
		DiskID:             "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
		SectorSize:         512,
		Size:               6000 * 1024 * 1024,
		Schema:             "gpt",
		Structure: []gadget.DiskStructureDeviceTraits{
			{
				PartitionLabel:     "nofspart",
				PartitionUUID:      "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
				OriginalDevicePath: "/sys/block/foo/foo1",
				OriginalKernelPath: "/dev/foo1",
				Offset:             0x100000,
				Size:               0x1000,
			},
			{
				PartitionLabel:     "some-filesystem",
				PartitionUUID:      "DA2ADBC8-90DF-4B1D-A93F-A92516C12E01",
				OriginalDevicePath: "/sys/block/foo/foo2",
				OriginalKernelPath: "/dev/foo2",
				FilesystemLabel:    "some-filesystem",
				FilesystemUUID:     "3E3D392C-5D50-4C84-8A6E-09B7A3FEA2C7",
				FilesystemType:     "ext4",
				Offset:             0x101000,
				Size:               0x40000000,
			},
		},
	})
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateMinSize(c *C) {
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": {
			DevNode: "/dev/foo",
			DevPath: "/sys/block/foo",
			DevNum:  "525:1",
			// assume 34 sectors at end for GPT headers backup
			DiskUsableSectorEnd: 6000*1024*1024/512 - 34,
			DiskSizeInBytes:     6000 * 1024 * 1024,
			SectorSizeBytes:     512,
			DiskSchema:          "gpt",
			ID:                  "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
			Structure: []disks.Partition{
				{
					PartitionLabel:   "nofspart",
					PartitionUUID:    "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
					FilesystemType:   "",
					Major:            525,
					Minor:            2,
					KernelDeviceNode: "/dev/foo1",
					KernelDevicePath: "/sys/block/foo/foo1",
					DiskIndex:        1,
					StartInBytes:     1024 * 1024,
					SizeInBytes:      4096,
				},
				{
					PartitionLabel:   "some-filesystem",
					PartitionUUID:    "DA2ADBC8-90DF-4B1D-A93F-A92516C12E01",
					FilesystemLabel:  "some-filesystem",
					FilesystemUUID:   "3E3D392C-5D50-4C84-8A6E-09B7A3FEA2C7",
					FilesystemType:   "ext4",
					Major:            525,
					Minor:            3,
					KernelDeviceNode: "/dev/foo2",
					KernelDevicePath: "/sys/block/foo/foo2",
					DiskIndex:        2,
					StartInBytes:     1024*1024 + 4096,
					SizeInBytes:      1024 * 1024 * 1024,
				},
			},
		},
	})
	defer restore()

	const yaml = `
volumes:
  foo:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: nofspart
        type: EBBEADAF-22C9-E33B-8F5D-0E81686A68CB
        min-size: 4096
        size: 8192
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`
	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), yaml, nil)
	c.Assert(err, IsNil)

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, IsNil)
	c.Assert(traits, DeepEquals, gadget.DiskVolumeDeviceTraits{
		OriginalDevicePath: "/sys/block/foo",
		OriginalKernelPath: "/dev/foo",
		DiskID:             "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
		SectorSize:         512,
		Size:               6000 * 1024 * 1024,
		Schema:             "gpt",
		Structure: []gadget.DiskStructureDeviceTraits{
			{
				PartitionLabel:     "nofspart",
				PartitionUUID:      "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
				OriginalDevicePath: "/sys/block/foo/foo1",
				OriginalKernelPath: "/dev/foo1",
				Offset:             0x100000,
				Size:               0x1000,
			},
			{
				PartitionLabel:     "some-filesystem",
				PartitionUUID:      "DA2ADBC8-90DF-4B1D-A93F-A92516C12E01",
				OriginalDevicePath: "/sys/block/foo/foo2",
				OriginalKernelPath: "/dev/foo2",
				FilesystemLabel:    "some-filesystem",
				FilesystemUUID:     "3E3D392C-5D50-4C84-8A6E-09B7A3FEA2C7",
				FilesystemType:     "ext4",
				Offset:             0x101000,
				Size:               0x40000000,
			},
		},
	})
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateGPTMultiVolume(c *C) {
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
		"/dev/vdb": gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	vols, err := gadgettest.LayoutMultiVolumeFromYaml(
		c.MkDir(),
		"",
		gadgettest.MultiVolumeUC20GadgetYaml,
		uc20Model,
	)
	c.Assert(err, IsNil)

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(vols["pc"].Volume, "/dev/vda", nil)
	c.Assert(err, IsNil)
	c.Assert(traits, DeepEquals, gadgettest.VMSystemVolumeDeviceTraits)

	traitsExtra, err := gadget.DiskTraitsFromDeviceAndValidate(vols["foo"].Volume, "/dev/vdb", nil)
	c.Assert(err, IsNil)
	c.Assert(traitsExtra, DeepEquals, gadgettest.VMExtraVolumeDeviceTraits)
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateGPTExtraOnDiskStructure(c *C) {
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	// yaml doesn't have some-filesystem in it
	const yaml = `
volumes:
  foo:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: barething
        type: bare
        size: 1024
      - name: nofspart
        type: EBBEADAF-22C9-E33B-8F5D-0E81686A68CB
        size: 4096
`
	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), yaml, nil)
	c.Assert(err, IsNil)

	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, ErrorMatches, `volume foo is not compatible with disk /dev/foo: cannot find disk partition /dev/foo2 \(starting at 1053696\) in gadget`)
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateGPTExtraLaidOutStructure(c *C) {

	mockDisk := &disks.MockDiskMapping{
		DevNode: "/dev/foo",
		DevPath: "/sys/block/foo",
		DevNum:  "525:1",
		// assume 34 sectors at end for GPT headers backup
		DiskUsableSectorEnd: 6000*1024*1024/512 - 34,
		DiskSizeInBytes:     6000 * 1024 * 1024,
		SectorSizeBytes:     512,
		DiskSchema:          "gpt",
		ID:                  "651AC800-B9FB-4B9D-B6D3-A72EB54D9006",
		// on disk structure is missing ubuntu-data from the YAML below
		Structure: []disks.Partition{
			{
				PartitionLabel:   "nofspart",
				PartitionUUID:    "C5A930DF-E86A-4BAE-A4C5-C861353796E6",
				FilesystemType:   "",
				Major:            525,
				Minor:            2,
				KernelDeviceNode: "/dev/foo1",
				KernelDevicePath: "/sys/block/foo/foo1",
				DiskIndex:        1,
				StartInBytes:     1024 * 1024,
				SizeInBytes:      4096,
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": mockDisk,
	})
	defer restore()

	const yaml = `
volumes:
  foo:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: nofspart
        type: EBBEADAF-22C9-E33B-8F5D-0E81686A68CB
        size: 4096
      - filesystem: ext4
        name: ubuntu-data
        role: system-data
        size: 1500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
`
	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), yaml, nil)
	c.Assert(err, IsNil)

	// we can't build the device traits because the two are not compatible, even
	// though the last structure is system-data which may not exist before
	// install mode and thus be "compatible" in some contexts, but
	// DiskTraitsFromDeviceAndValidate is more strict and requires all
	// structures to exist and to match
	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, ErrorMatches, `volume foo is not compatible with disk /dev/foo: cannot find gadget structure "ubuntu-data" on disk`)

	// if we add a structure to the mock disk which is smaller than the ondisk
	// layout, we still reject it because the on disk must be at least the size
	// that the gadget mentions
	mockDisk.Structure = append(mockDisk.Structure, disks.Partition{
		PartitionLabel:   "ubuntu-data",
		PartitionUUID:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
		FilesystemType:   "ext4",
		Major:            525,
		Minor:            3,
		KernelDeviceNode: "/dev/foo2",
		KernelDevicePath: "/sys/block/foo/foo2",
		DiskIndex:        2,
		StartInBytes:     1024*1024 + 4096,
		SizeInBytes:      4096,
	})

	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": mockDisk,
	})
	defer restore()

	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, ErrorMatches, `volume foo is not compatible with disk /dev/foo: cannot find disk partition /dev/foo2 \(starting at 1052672\) in gadget: on disk size 4096 \(4 KiB\) is smaller than gadget min size 1572864000 \(1.46 GiB\)`)

	// same size is okay though
	mockDisk.Structure[1].SizeInBytes = 1500 * 1024 * 1024
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": mockDisk,
	})
	defer restore()

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, IsNil)

	// it has the right size
	c.Assert(traits.Structure[1].Size, Equals, 1500*quantity.SizeMiB)

	// bigger is okay too
	mockDisk.Structure[1].SizeInBytes = 3200 * 1024 * 1024
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": mockDisk,
	})
	defer restore()

	traits, err = gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/foo", nil)
	c.Assert(err, IsNil)

	// and it has the on disk size
	c.Assert(traits.Structure[1].Size, Equals, 3200*quantity.SizeMiB)
}

func (u *updateTestSuite) TestDiskTraitsFromDeviceAndValidateDOSSingleVolume(c *C) {
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/mmcblk0": gadgettest.ExpectedRaspiMockDiskMapping,
	})
	defer restore()

	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.RaspiSimplifiedYaml, nil)
	c.Assert(err, IsNil)

	opts := &gadget.DiskVolumeValidationOptions{
		// make this non-nil so that it matches the non-nil (but empty) map in
		// gadgettest/examples.go
		ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{},
	}
	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/mmcblk0", opts)
	c.Assert(err, IsNil)
	c.Assert(traits, DeepEquals, gadgettest.ExpectedRaspiDiskVolumeDeviceTraits)
}

func (s *updateTestSuite) TestDiskTraitsFromDeviceAndValidateImplicitSystemDataHappy(c *C) {
	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, nil)
	c.Assert(err, IsNil)

	// the volume cannot be found with no opts set
	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/sda", nil)
	c.Assert(err, ErrorMatches, `volume pc is not compatible with disk /dev/sda: cannot find disk partition /dev/sda3 \(starting at 54525952\) in gadget`)

	// with opts for pc then it can be found
	opts := &gadget.DiskVolumeValidationOptions{
		AllowImplicitSystemData: true,
	}

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/sda", opts)
	c.Assert(err, IsNil)

	c.Assert(traits, DeepEquals, gadgettest.UC16ImplicitSystemDataDeviceTraits)
}

func (s *updateTestSuite) TestDiskTraitsFromDeviceAndValidateImplicitSystemDataRaspiHappy(c *C) {
	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/mmcblk0": gadgettest.ExpectedRaspiUC18MockDiskMapping,
	})
	defer restore()

	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.RaspiUC18SimplifiedYaml, nil)
	c.Assert(err, IsNil)

	// the volume cannot be found with no opts set
	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/mmcblk0", nil)
	c.Assert(err.Error(), Equals, `volume pi is not compatible with disk /dev/mmcblk0: cannot find disk partition /dev/mmcblk0p2 (starting at 269484032) in gadget: disk partition "" offset 269484032 (257 MiB) is not in the valid gadget interval (min: 1048576 (1 MiB): max: 1048576 (1 MiB))`)

	// with opts for pc then it can be found
	opts := &gadget.DiskVolumeValidationOptions{
		AllowImplicitSystemData: true,
	}

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol.Volume, "/dev/mmcblk0", opts)
	c.Assert(err, IsNil)

	c.Assert(traits, DeepEquals, gadgettest.ExpectedRaspiUC18DiskVolumeDeviceTraits)
}

func (s *updateTestSuite) TestSearchForVolumeWithTraitsImplicitSystemData(c *C) {
	allowImplicitDataOpts := &gadget.DiskVolumeValidationOptions{
		AllowImplicitSystemData: true,
	}
	testSearchForVolumeWithTraits(c,
		gadgettest.UC16YAMLImplicitSystemData,
		"pc",
		uc16Model,
		gadgettest.UC16ImplicitSystemDataMockDiskMapping,
		gadgettest.UC16ImplicitSystemDataDeviceTraits,
		allowImplicitDataOpts,
	)
}

func (s *updateTestSuite) TestSearchForVolumeWithTraitsFails(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()
	unrelatedDisk := &disks.MockDiskMapping{
		DevNum:  "1:1",
		DevPath: "/sys/devices/fooo",
		DevNode: "/dev/fooo",
	}

	r := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/fooo": unrelatedDisk,
	})
	defer r()

	allVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.UC16YAMLImplicitSystemData, uc16Model)
	c.Assert(err, IsNil)

	laidOutVol := allVolumes["pc"]

	// first go around we use the device path which matches
	r = disks.MockDevicePathToDiskMapping(map[string]*disks.MockDiskMapping{
		"/sys/devices/fooo": unrelatedDisk,
	})
	defer r()

	allowImplicitDataOpts := &gadget.DiskVolumeValidationOptions{
		AllowImplicitSystemData: true,
	}

	_, _, err = gadget.SearchVolumeWithTraitsAndMatchParts(laidOutVol.Volume, gadgettest.UC16ImplicitSystemDataDeviceTraits, allowImplicitDataOpts)
	c.Assert(err, ErrorMatches, "cannot find physical disk laid out to map with volume pc")
}

func (s *updateTestSuite) TestSearchForVolumeWithTraitsNonSystemBoot(c *C) {
	testSearchForVolumeWithTraits(c,
		gadgettest.MultiVolumeUC20GadgetYaml,
		"foo",
		uc20Model,
		gadgettest.VMExtraVolumeDiskMapping,
		gadgettest.VMExtraVolumeDeviceTraits,
		nil,
	)
}

func (s *updateTestSuite) TestSearchForVolumeWithTraitsUC20Encryption(c *C) {
	encryptOpts := &gadget.DiskVolumeValidationOptions{
		ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{
			"ubuntu-data": {Method: gadget.EncryptionLUKS},
			"ubuntu-save": {Method: gadget.EncryptionLUKS},
		},
	}

	testSearchForVolumeWithTraits(c,
		gadgettest.RaspiSimplifiedYaml,
		"pi",
		uc20Model,
		gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping,
		gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits,
		encryptOpts,
	)
}

func testSearchForVolumeWithTraits(c *C,
	gadgetYaml string,
	volName string,
	model gadget.Model,
	realMapping *disks.MockDiskMapping,
	traits gadget.DiskVolumeDeviceTraits,
	validateOpts *gadget.DiskVolumeValidationOptions,
) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()
	otherDisk := &disks.MockDiskMapping{
		DevNum:  "1:1",
		DevPath: traits.OriginalDevicePath,
		DevNode: "/dev/fooo",
	}

	r := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		traits.OriginalKernelPath: realMapping,
		"/dev/fooo":               otherDisk,
	})
	defer r()

	allVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgetYaml, model)
	c.Assert(err, IsNil)

	laidOutVol := allVolumes[volName]

	// first go around we use the device path which matches
	r = disks.MockDevicePathToDiskMapping(map[string]*disks.MockDiskMapping{
		traits.OriginalDevicePath: realMapping,
	})
	defer r()

	d, _, err := gadget.SearchVolumeWithTraitsAndMatchParts(laidOutVol.Volume, traits, validateOpts)
	c.Assert(err, IsNil)
	c.Assert(d.Dev(), Equals, realMapping.DevNum)

	// now make the device path change to something else
	r = disks.MockDevicePathToDiskMapping(map[string]*disks.MockDiskMapping{
		"/sys/devices/new":        realMapping,
		traits.OriginalDevicePath: otherDisk,
	})
	defer r()

	// we still find it because we fall back on the device name from the traits
	// (/dev/sda)
	d2, _, err := gadget.SearchVolumeWithTraitsAndMatchParts(laidOutVol.Volume, traits, validateOpts)
	c.Assert(err, IsNil)
	c.Assert(d2.Dev(), Equals, realMapping.DevNum)

	// now try the last fallback which is the disk ID

	// because we can't make the first check of DiskFromDeviceName that comes
	// from checking traits.OriginalKernelPath fail, but then the subsequent one
	// to validate the disk successful, we have to instead just set the
	// OriginalKernelPath to empty so it skips the check entirely
	traits.OriginalKernelPath = ""

	devicePathMapping := map[string]*disks.MockDiskMapping{
		traits.OriginalDevicePath: otherDisk,
	}

	// mock two disks in /sys/block
	blockDir := filepath.Join(dirs.SysfsDir, "block")
	err = os.MkdirAll(blockDir, 0755)
	c.Assert(err, IsNil)
	for _, f := range []string{"real", "other"} {
		blockDevSym := filepath.Join(blockDir, f)
		err := os.Symlink("something", blockDevSym)
		c.Assert(err, IsNil)

		switch f {
		case "real":
			devicePathMapping[blockDevSym] = realMapping
		case "other":
			devicePathMapping[blockDevSym] = otherDisk
		}
	}

	r = disks.MockDevicePathToDiskMapping(devicePathMapping)
	defer r()

	d3, _, err := gadget.SearchVolumeWithTraitsAndMatchParts(laidOutVol.Volume, traits, validateOpts)
	c.Assert(err, IsNil)
	c.Assert(d3.Dev(), Equals, realMapping.DevNum)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingInvalidYAMLDoesNotBlockOverallRefresh(c *C) {
	// copied from managers tests
	structureName := "ubuntu-seed"
	gadgetYaml := fmt.Sprintf(`
volumes:
    volume-id:
        schema: mbr
        bootloader: u-boot
        structure:
          - name: %s
            filesystem: vfat
            type: 0C
            size: 1200M
            content:
              - source: boot-assets/
                target: /`, structureName)

	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), gadgetYaml, nil)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"volume-id": lvol.Volume,
			},
		},
	}

	// don't mock anything we don't get that far in the function

	allLaidOutVolumes := map[string]*gadget.LaidOutVolume{
		"volume-id": lvol,
	}

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	_, err = gadget.BuildNewVolumeToDeviceMapping(uc16Model, old, vols)
	c.Assert(err, Equals, gadget.ErrSkipUpdateProceedRefresh)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingImplicitSystemDataUC16(c *C) {
	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.UC16YAMLImplicitSystemData, uc16Model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: make(map[string]*gadget.Volume),
		},
	}

	for volName, laidOutVol := range allLaidOutVolumes {
		old.Info.Volumes[volName] = laidOutVol.Volume
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/sda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("EFI System")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/sda1"): gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	m, err := gadget.BuildNewVolumeToDeviceMapping(uc16Model, old, vols)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.UC16ImplicitSystemDataDeviceTraits,
	})
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingImplicitSystemBootSingleVolume(c *C) {
	// not there is no role or filesystem-label or name referencing system-boot
	// here so there are no implicit roles set for this yaml, but it is valid as
	// we used to allow installation of such gadget.yaml and as such need to
	// continue to support updates for this
	const implicitSystemBootVolumeYAML = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        size: 50M
`

	laidOutVolume, err := gadgettest.LayoutFromYaml(c.MkDir(), implicitSystemBootVolumeYAML, uc16Model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"pc": laidOutVolume.Volume,
			},
		},
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/sda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("EFI System")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/sda1"): gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	allLaidOutVolumes := map[string]*gadget.LaidOutVolume{
		"pc": laidOutVolume,
	}

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	m, err := gadget.BuildNewVolumeToDeviceMapping(uc16Model, old, vols)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.UC16ImplicitSystemDataDeviceTraits,
	})
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingImplicitSystemBootMultiVolumeNotSupported(c *C) {
	// not there is no role or filesystem-label or name referencing system-boot
	// here so there are no implicit roles set for this yaml, but it is valid as
	// we used to allow installation of such gadget.yaml, but since it has
	// multiple volumes, we need to make sure we fail with a specific error that
	// allows the overall gadget refresh to proceed but skips the asset update
	const implicitSystemBootVolumeYAML = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        size: 50M
  foo:
    structure:
      - name: some-thing
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        size: 50M
`

	// need to manually lay out this YAML, the helpers don't work for
	// multi-volume non-UC20 setups like this
	gadgetRoot, err := gadgettest.WriteGadgetYaml(c.MkDir(), implicitSystemBootVolumeYAML)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(gadgetRoot, uc16Model)
	c.Assert(err, IsNil)

	allLaidOutVolumes := map[string]*gadget.LaidOutVolume{}

	opts := &gadget.LayoutOptions{GadgetRootDir: gadgetRoot}
	for volName, vol := range info.Volumes {
		lvol, err := gadget.LayoutVolume(vol, gadget.OnDiskStructsFromGadget(vol), opts)
		c.Assert(err, IsNil)
		allLaidOutVolumes[volName] = lvol
	}

	old := gadget.GadgetData{Info: info}

	// don't need to mock anything, we don't get far enough

	// we fail with the error that skips the asset update but proceeds with the
	// rest of the refresh
	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	_, err = gadget.BuildNewVolumeToDeviceMapping(uc16Model, old, vols)
	c.Assert(err, Equals, gadget.ErrSkipUpdateProceedRefresh)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingPreUC20NonFatalError(c *C) {
	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.UC16YAMLImplicitSystemData, uc16Model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: make(map[string]*gadget.Volume),
		},
	}

	for volName, laidOutVol := range allLaidOutVolumes {
		old.Info.Volumes[volName] = laidOutVol.Volume
	}

	// don't mock any symlinks so that it fails to find any disk matching the
	// system-boot volume

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	_, err = gadget.BuildNewVolumeToDeviceMapping(uc16Model, old, vols)
	c.Assert(err, Equals, gadget.ErrSkipUpdateProceedRefresh)

	// it's a fatal error on UC20 though
	_, err = gadget.BuildNewVolumeToDeviceMapping(uc20Model, old, vols)
	c.Assert(err, Not(Equals), gadget.ErrSkipUpdateProceedRefresh)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingPreUC20CannotMap(c *C) {
	fmt.Println("TestBuildNewVolumeToDeviceMappingPreUC20CannotMap")
	defer fmt.Println("TestBuildNewVolumeToDeviceMappingPreUC20CannotMap")
	mockLogBuf, restore := logger.MockLogger()
	defer restore()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.UC16YAMLImplicitSystemData, uc16Model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: make(map[string]*gadget.Volume),
		},
	}

	for volName, laidOutVol := range allLaidOutVolumes {
		old.Info.Volumes[volName] = laidOutVol.Volume
	}

	// setup symlink for the system-boot partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("EFI System")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore = disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}

	// The call will fail as it won't find a match between /dev/vda2 and
	// any partition defined in the gadget. But it is ok for UC16/18.
	_, err = gadget.BuildNewVolumeToDeviceMapping(uc16Model, old, vols)
	c.Assert(err, Equals, gadget.ErrSkipUpdateProceedRefresh)
	c.Assert(mockLogBuf.String(), testutil.Contains, "WARNING: not applying gadget asset updates on main system-boot volume due to error while finding disk traits")

	// it's a fatal error on UC20 though
	_, err = gadget.BuildNewVolumeToDeviceMapping(uc20Model, old, vols)
	c.Assert(err, Not(Equals), gadget.ErrSkipUpdateProceedRefresh)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingUC20MultiVolume(c *C) {
	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.MultiVolumeUC20GadgetYaml, uc20Model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: make(map[string]*gadget.Volume),
		},
	}

	for volName, laidOutVol := range allLaidOutVolumes {
		old.Info.Volumes[volName] = laidOutVol.Volume
	}

	// setup symlink for the ubuntu-seed partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("ubuntu-seed")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/vda1"): gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
	})
	defer restore()

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	m, err := gadget.BuildNewVolumeToDeviceMapping(uc20Model, old, vols)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.VMSystemVolumeDeviceTraits,
	})
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingUC20Encryption(c *C) {
	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", gadgettest.RaspiSimplifiedYaml, uc20Model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: make(map[string]*gadget.Volume),
		},
	}

	for volName, laidOutVol := range allLaidOutVolumes {
		old.Info.Volumes[volName] = laidOutVol.Volume
	}

	// setup symlink for the ubuntu-seed partition
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/mmcblk0p1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("ubuntu-seed")))
	c.Assert(err, IsNil)
	err = os.WriteFile(fakedevicepart, nil, 0644)
	c.Assert(err, IsNil)

	// mock the partition device node to mock disk
	restore := disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		filepath.Join(dirs.GlobalRootDir, "/dev/mmcblk0p1"): gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping,
	})
	defer restore()

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/mmcblk0": gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping,
	})
	defer restore()

	// write an encryption marker
	markerFile := filepath.Join(dirs.SnapFDEDir, "marker")
	err = os.MkdirAll(filepath.Dir(markerFile), 0755)
	c.Assert(err, IsNil)

	err = os.WriteFile(markerFile, nil, 0644)
	c.Assert(err, IsNil)

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	m, err := gadget.BuildNewVolumeToDeviceMapping(uc20Model, old, vols)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pi": gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits,
	})
}

func (s *updateTestSuite) TestBuildVolumeStructureToLocationUC20MultiVolume(c *C) {
	traits := map[string]gadget.DiskVolumeDeviceTraits{
		"pc":  gadgettest.VMSystemVolumeDeviceTraits,
		"foo": gadgettest.VMExtraVolumeDeviceTraits,
	}

	volMappings := map[string]*disks.MockDiskMapping{
		"pc":  gadgettest.VMSystemVolumeDiskMapping,
		"foo": gadgettest.VMExtraVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
		"foo": {
			0: {Device: "/dev/vdb", Offset: quantity.OffsetMiB},                            // barething
			1: {Device: "/dev/vdb", Offset: quantity.OffsetMiB + 4096},                     // nofspart
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/foo/some-filesystem")}, // some-filesystem
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore := osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 525:3 / %[1]s/foo/some-filesystem rw,relatime shared:7 - vfat %[1]s/dev/vdb2 rw
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testBuildVolumeStructureToLocation(c,
		uc20Model,
		gadgettest.MultiVolumeUC20GadgetYaml,
		traits,
		volMappings,
		expMap,
	)
}

func (s *updateTestSuite) TestBuildVolumeStructureToLocationUC20SingleVolume(c *C) {
	traits := map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.VMSystemVolumeDeviceTraits,
	}

	volMappings := map[string]*disks.MockDiskMapping{
		"pc": gadgettest.VMSystemVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore := osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testBuildVolumeStructureToLocation(c,
		uc20Model,
		gadgettest.SingleVolumeUC20GadgetYaml,
		traits,
		volMappings,
		expMap,
	)
}

func (s *updateTestSuite) TestBuildVolumeStructureToLocationUC16ImplicitSystemData(c *C) {
	traits := map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.UC16ImplicitSystemDataDeviceTraits,
	}

	volMappings := map[string]*disks.MockDiskMapping{
		"pc": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/sda", Offset: 0},                  // for mbr
			1: {Device: "/dev/sda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/boot/grub")},

			// note that the implicit data partition is missing - it is not in
			// the YAML and thus cannot be updated via a gadget asset update
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore := osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/boot/grub rw,relatime shared:7 - vfat %[1]s/dev/vda1 rw
27 27 600:3 / %[1]s/writable rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testBuildVolumeStructureToLocation(c,
		uc16Model,
		gadgettest.UC16YAMLImplicitSystemData,
		traits,
		volMappings,
		expMap,
	)
}

func (s *updateTestSuite) TestBuildVolumeStructureToLocationUC20Encryption(c *C) {
	mockLogBuf, restore := logger.MockLogger()
	defer restore()

	traits := map[string]gadget.DiskVolumeDeviceTraits{
		"pi": gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits,
	}

	volMappings := map[string]*disks.MockDiskMapping{
		"pi": gadgettest.ExpectedLUKSEncryptedRaspiMockDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pi": {
			// keys are the YamlIndex in the gadget.yaml

			// partition devices have RootMountPoint set
			0: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			1: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},

			// encrypted partitions are currently treated like they are
			// unmounted
			2: {RootMountPoint: ""},
			3: {RootMountPoint: ""},
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily

	// also note that neither save nor data are present here since they are
	// encrypted the mapper devices would show up here, but we don't currently
	// support anything like that so just ignore that
	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 179:1 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/mmcblk0p1 rw
27 27 179:1 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/mmcblk0p1 rw
28 27 179:2 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/mmcblk0p1 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testBuildVolumeStructureToLocation(c,
		uc20Model,
		gadgettest.RaspiSimplifiedYaml,
		traits,
		volMappings,
		expMap,
	)

	// we logged a message about not supporting asset updates on encrypted
	// partitions
	c.Assert(mockLogBuf.String(), testutil.Contains, "gadget asset update for assets on encrypted partition ubuntu-data unsupported")
	c.Assert(mockLogBuf.String(), testutil.Contains, "gadget asset update for assets on encrypted partition ubuntu-save unsupported")
}

func (s *updateTestSuite) TestBuildVolumeStructureToLocationUC20MultiVolumeNonMountedPartition(c *C) {
	traits := map[string]gadget.DiskVolumeDeviceTraits{
		"pc":  gadgettest.VMSystemVolumeDeviceTraits,
		"foo": gadgettest.VMExtraVolumeDeviceTraits,
	}

	volMappings := map[string]*disks.MockDiskMapping{
		"pc":  gadgettest.VMSystemVolumeDiskMapping,
		"foo": gadgettest.VMExtraVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: ""}, // ubuntu-boot is not mounted for some reason
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
		"foo": {
			0: {Device: "/dev/vdb", Offset: quantity.OffsetMiB},        // barething
			1: {Device: "/dev/vdb", Offset: quantity.OffsetMiB + 4096}, // nofspart
			2: {RootMountPoint: ""},                                    // some-filesystem is not mounted
		},
	}

	mockLogBuf, restore := logger.MockLogger()
	defer restore()

	// setup mountinfo for root mount points of the partitions with some of the filesystems mounted
	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testBuildVolumeStructureToLocation(c,
		uc20Model,
		gadgettest.MultiVolumeUC20GadgetYaml,
		traits,
		volMappings,
		expMap,
	)

	c.Assert(mockLogBuf.String(), testutil.Contains, "structure 2 on volume foo (/dev/vdb2) is not mounted read/write anywhere to be able to update it")
}

func (s *updateTestSuite) testBuildVolumeStructureToLocation(c *C,
	model gadget.Model,
	yaml string,
	traits map[string]gadget.DiskVolumeDeviceTraits,
	volMappings map[string]*disks.MockDiskMapping,
	expMapping map[string]map[int]gadget.StructureLocation,
) {
	old, allLaidOutVolumes := s.setupForVolumeStructureToLocation(c, model,
		yaml,
		traits,
		volMappings,
		expMapping,
	)

	missingInitialMappingNo := false
	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	structureMap, _, err := gadget.BuildVolumeStructureToLocation(model, old, vols, traits, missingInitialMappingNo)
	c.Assert(err, IsNil)
	c.Assert(structureMap, DeepEquals, expMapping)
}

func (s *updateTestSuite) setupForVolumeStructureToLocation(c *C,
	model gadget.Model,
	yaml string,
	traits map[string]gadget.DiskVolumeDeviceTraits,
	volMappings map[string]*disks.MockDiskMapping,
	expMapping map[string]map[int]gadget.StructureLocation,
) (gadget.GadgetData, map[string]*gadget.LaidOutVolume) {
	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), "", yaml, model)
	c.Assert(err, IsNil)

	old := gadget.GadgetData{
		Info: &gadget.Info{
			Volumes: make(map[string]*gadget.Volume),
		},
	}

	for volName, laidOutVol := range allLaidOutVolumes {
		old.Info.Volumes[volName] = laidOutVol.Volume
	}

	devicePathMapping := map[string]*disks.MockDiskMapping{}

	// mock two disks in /sys/block
	blockDir := filepath.Join(dirs.SysfsDir, "block")
	err = os.MkdirAll(blockDir, 0755)
	c.Assert(err, IsNil)
	for volName := range allLaidOutVolumes {
		blockDevSym := filepath.Join(blockDir, volName)
		err := os.Symlink("something", blockDevSym)
		c.Assert(err, IsNil)

		devicePathMapping[blockDevSym] = volMappings[volName]
	}

	restore := disks.MockDevicePathToDiskMapping(devicePathMapping)
	s.AddCleanup(restore)

	// setup symlinks in /dev
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label"), 0755)
	c.Assert(err, IsNil)

	partDeviceNodeMappings := map[string]*disks.MockDiskMapping{}
	diskDeviceNodeMappings := map[string]*disks.MockDiskMapping{}

	for volName := range allLaidOutVolumes {
		// only create udev symlinks for the fist partition, don't need the
		// others
		partlabel := ""
		fslabel := ""
		firstPartDev := ""
		for _, p := range traits[volName].Structure {
			firstPartDev = p.OriginalKernelPath
			partlabel = p.PartitionLabel
			fslabel = p.FilesystemLabel
			break
		}

		switch traits[volName].Schema {
		case "gpt":
			fakedevicepart := filepath.Join(dirs.GlobalRootDir, firstPartDev)
			err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", partlabel))
			c.Assert(err, IsNil)
			err = os.WriteFile(fakedevicepart, nil, 0644)
			c.Assert(err, IsNil)
		case "dos":
			fakedevicepart := filepath.Join(dirs.GlobalRootDir, firstPartDev)
			err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label", fslabel))
			c.Assert(err, IsNil)
			err = os.WriteFile(fakedevicepart, nil, 0644)
			c.Assert(err, IsNil)
		default:
			panic(fmt.Sprintf("unexpected schema %s", traits[volName].Schema))
		}

		partDeviceNodeMappings[filepath.Join(dirs.GlobalRootDir, firstPartDev)] = volMappings[volName]

		diskDeviceNodeMappings[traits[volName].OriginalKernelPath] = volMappings[volName]
	}

	// mock the partition device node to mock disk
	restore = disks.MockPartitionDeviceNodeToDiskMapping(partDeviceNodeMappings)
	s.AddCleanup(restore)

	// and the device name to the disk itself
	restore = disks.MockDeviceNameToDiskMapping(diskDeviceNodeMappings)
	s.AddCleanup(restore)

	return old, allLaidOutVolumes
}

func (s *updateTestSuite) testVolumeStructureToLocationMap(c *C,
	model gadget.Model,
	yaml string,
	traitsJSON string,
	withTraits bool,
	volMappings map[string]*disks.MockDiskMapping,
	expMapping map[string]map[int]gadget.StructureLocation,
) {
	err := os.MkdirAll(dirs.SnapDeviceDir, 0755)
	c.Assert(err, IsNil)
	// write out the provided traits JSON so we can at least load the traits for
	// mocking via setupForVolumeStructureToLocation
	err = os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(traitsJSON),
		0644,
	)
	c.Assert(err, IsNil)

	traits, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)

	// if we aren't meant to have the traits written to disk for the test delete
	// it
	if !withTraits {
		err := os.Remove(filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"))
		c.Assert(err, IsNil)
	}

	old, allLaidOutVolumes := s.setupForVolumeStructureToLocation(c, model,
		yaml,
		traits,
		volMappings,
		expMapping,
	)

	vols := map[string]*gadget.Volume{}
	for name, lov := range allLaidOutVolumes {
		vols[name] = lov.Volume
	}
	structureMap, _, err := gadget.VolumeStructureToLocationMap(old, model, vols)
	c.Assert(err, IsNil)
	c.Assert(structureMap, DeepEquals, expMapping)
}

func (s *updateTestSuite) TestVolumeStructureToLocationMapUC20MultiVolume(c *C) {
	volMappings := map[string]*disks.MockDiskMapping{
		"pc":  gadgettest.VMSystemVolumeDiskMapping,
		"foo": gadgettest.VMExtraVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
		"foo": {
			0: {Device: "/dev/vdb", Offset: quantity.OffsetMiB},                            // barething
			1: {Device: "/dev/vdb", Offset: quantity.OffsetMiB + 4096},                     // nofspart
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/foo/some-filesystem")}, // some-filesystem
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore := osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 525:3 / %[1]s/foo/some-filesystem rw,relatime shared:7 - vfat %[1]s/dev/vdb2 rw
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testVolumeStructureToLocationMap(c,
		uc20Model,
		gadgettest.MultiVolumeUC20GadgetYaml,
		gadgettest.VMMultiVolumeUC20DiskTraitsJSON,
		true,
		volMappings,
		expMap,
	)
}

func (s *updateTestSuite) TestVolumeStructureToLocationMapUC20SingleVolume(c *C) {
	volMappings := map[string]*disks.MockDiskMapping{
		"pc": gadgettest.VMSystemVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore := osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testVolumeStructureToLocationMap(c,
		uc20Model,
		gadgettest.SingleVolumeUC20GadgetYaml,
		gadgettest.VMSingleVolumeUC20DiskTraitsJSON,
		true,
		volMappings,
		expMap,
	)
}

func (s *updateTestSuite) TestVolumeStructureToLocationMapMissingInitialTraitsMapUC20SingleVolume(c *C) {
	volMappings := map[string]*disks.MockDiskMapping{
		"pc": gadgettest.VMSystemVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore := osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testVolumeStructureToLocationMap(c,
		uc20Model,
		gadgettest.SingleVolumeUC20GadgetYaml,
		gadgettest.VMSingleVolumeUC20DiskTraitsJSON,
		false,
		volMappings,
		expMap,
	)
}

func (s *updateTestSuite) TestVolumeStructureToLocationMapMissingInitialTraitsMapUC20MultiVolume(c *C) {
	mockLogBuf, restore := logger.MockLogger()
	defer restore()

	volMappings := map[string]*disks.MockDiskMapping{
		"pc":  gadgettest.VMSystemVolumeDiskMapping,
		"foo": gadgettest.VMExtraVolumeDiskMapping,
	}

	expMap := map[string]map[int]gadget.StructureLocation{
		"pc": {
			// keys are the YamlIndex in the gadget.yaml

			// raw devices have Device + Offset set
			0: {Device: "/dev/vda", Offset: 0},                  // for mbr
			1: {Device: "/dev/vda", Offset: quantity.OffsetMiB}, // for bios-boot

			// partition devices have RootMountPoint set
			2: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-seed")},
			3: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-boot")},
			4: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-save")},
			5: {RootMountPoint: filepath.Join(dirs.GlobalRootDir, "/run/mnt/data")},
		},
		// missing foo volume since because the disk-mapping.json was not
		// written initially, we only handle updates to the pc / system-boot
		// volume
	}

	// setup mountinfo for root mount points of the partitions with filesystems
	// note ubuntu-seed is mounted twice, but the impl always chooses the first
	// mount point arbitrarily
	restore = osutil.MockMountInfo(
		fmt.Sprintf(
			`
27 27 525:3 / %[1]s/foo/some-filesystem rw,relatime shared:7 - vfat %[1]s/dev/vdb2 rw
27 27 600:3 / %[1]s/run/mnt/ubuntu-seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
27 27 600:3 / %[1]s/writable/system-data/var/lib/snapd/seed rw,relatime shared:7 - vfat %[1]s/dev/vda2 rw
28 27 600:4 / %[1]s/run/mnt/ubuntu-boot rw,relatime shared:7 - vfat %[1]s/dev/vda3 rw
29 27 600:5 / %[1]s/run/mnt/ubuntu-save rw,relatime shared:7 - vfat %[1]s/dev/vda4 rw
30 27 600:6 / %[1]s/run/mnt/data rw,relatime shared:7 - vfat %[1]s/dev/vda5 rw`[1:],
			dirs.GlobalRootDir,
		),
	)
	defer restore()

	s.testVolumeStructureToLocationMap(c,
		uc20Model,
		gadgettest.MultiVolumeUC20GadgetYaml,
		gadgettest.VMMultiVolumeUC20DiskTraitsJSON,
		false,
		volMappings,
		expMap,
	)

	c.Assert(mockLogBuf.String(), testutil.Contains, "WARNING: gadget has multiple volumes but updates are only being performed for volume pc")
}
