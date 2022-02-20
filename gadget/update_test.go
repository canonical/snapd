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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

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

type updateTestSuite struct{}

var _ = Suite(&updateTestSuite{})

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
	from   gadget.LaidOutStructure
	to     gadget.LaidOutStructure
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
		err := gadget.CanUpdateStructure(&tc.from, &tc.to, schema)
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
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1*quantity.SizeMiB + 1*quantity.SizeKiB},
			},
			err: "cannot change structure size from [0-9]+ to [0-9]+",
		}, {
			// size change
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB},
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
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 1024},
				},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 2048},
				},
			},
			err: "cannot change structure offset-write from [0-9]+ to [0-9]+",
		}, {
			// offset-write, change in relative-to structure name
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "bar", Offset: 1024},
				},
			},
			err: `cannot change structure offset-write from foo\+[0-9]+ to bar\+[0-9]+`,
		}, {
			// offset-write, unspecified in old
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "bar", Offset: 1024},
				},
			},
			err: `cannot change structure offset-write from unspecified to bar\+[0-9]+`,
		}, {
			// offset-write, unspecified in new
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			err: `cannot change structure offset-write from foo\+[0-9]+ to unspecified`,
		}, {
			// all ok, both nils
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: nil,
				},
			},
			err: ``,
		}, {
			// all ok, both fully specified
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024},
				},
			},
			err: ``,
		}, {
			// all ok, both fully specified
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					OffsetWrite: &gadget.RelativeOffset{Offset: 1024},
				},
			},
			to: gadget.LaidOutStructure{
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
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(1024)},
				StartOffset:     1024,
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(2048)},
				StartOffset:     2048,
			},
			err: "cannot change structure offset from [0-9]+ to [0-9]+",
		}, {
			// explicitly declared start offset in new structure
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: nil},
				StartOffset:     1024,
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(2048)},
				StartOffset:     2048,
			},
			err: "cannot change structure offset from unspecified to [0-9]+",
		}, {
			// explicitly declared start offset in old structure,
			// missing from new
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: asOffsetPtr(1024)},
				StartOffset:     1024,
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB, Offset: nil},
				StartOffset:     2048,
			},
			err: "cannot change structure offset from [0-9]+ to unspecified",
		}, {
			// start offset changed due to layout
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB},
				StartOffset:     1 * quantity.OffsetMiB,
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Size: 1 * quantity.SizeMiB},
				StartOffset:     2 * quantity.OffsetMiB,
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
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: ""},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: "system-data"},
			},
			err: `cannot change structure role from "" to "system-data"`,
		}, {
			// explicitly set tole
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: "mbr"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: "system-data"},
			},
			err: `cannot change structure role from "mbr" to "system-data"`,
		}, {
			// implicit legacy role to proper explicit role
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "mbr", Role: "mbr"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare", Role: "mbr"},
			},
			err: "",
		}, {
			// but not in the opposite direction
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare", Role: "mbr"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "mbr", Role: "mbr"},
			},
			err: `cannot change structure type from "bare" to "mbr"`,
		}, {
			// start offset changed due to layout
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Role: ""},
			},
			to: gadget.LaidOutStructure{
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
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err: `cannot change structure type from "0C,00000000-0000-0000-0000-dd00deadbeef" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			// from MBR type to GUID (would be stopped at volume update checks)
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err: `cannot change structure type from "0C" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			// from one MBR type to another
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0A"},
			},
			err: `cannot change structure type from "0C" to "0A"`,
		}, {
			// from one MBR type to another
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare"},
			},
			err: `cannot change structure type from "0C" to "bare"`,
		}, {
			// from one GUID to another
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadcafe"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err: `cannot change structure type from "00000000-0000-0000-0000-dd00deadcafe" to "00000000-0000-0000-0000-dd00deadbeef"`,
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "bare"},
			},
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C"},
			},
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C,00000000-0000-0000-0000-dd00deadbeef"},
			},
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateID(c *C) {

	cases := []canUpdateTestCase{
		{
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{ID: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.LaidOutStructure{
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
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: ""},
			},
			err: `cannot change a filesystem structure to a bare one`,
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: ""},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			err: `cannot change a bare structure to filesystem one`,
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "vfat"},
			},
			err: `cannot change filesystem from "ext4" to "vfat"`,
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "writable"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4"},
			},
			err: `cannot change filesystem label from "writable" to ""`,
		}, {
			// all ok
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "do-not-touch"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Type: "0C", Filesystem: "ext4", Label: "do-not-touch"},
			},
			err: ``,
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateName(c *C) {

	cases := []canUpdateTestCase{
		{
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Name: "foo", Type: "0C"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Name: "mbr-ok", Type: "0C"},
			},
			err:    ``,
			schema: "mbr",
		}, {
			from: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Name: "foo", Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			to: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{Name: "gpt-unhappy", Type: "00000000-0000-0000-0000-dd00deadbeef"},
			},
			err:    `cannot change structure name from "foo" to "gpt-unhappy"`,
			schema: "gpt",
		},
	}
	u.testCanUpdate(c, cases)
}

func (u *updateTestSuite) TestCanUpdateVolume(c *C) {

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
				Volume: &gadget.Volume{Schema: "mbr"},
			},
			err: `cannot change volume schema from "gpt" to "mbr"`,
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
				Volume: &gadget.Volume{Schema: "mbr"},
				LaidOutStructure: []gadget.LaidOutStructure{
					{}, {},
				},
			},
			to: gadget.LaidOutVolume{
				Volume: &gadget.Volume{Schema: "mbr"},
				LaidOutStructure: []gadget.LaidOutStructure{
					{}, {},
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

func updateDataSet(c *C) (oldData gadget.GadgetData, newData gadget.GadgetData, rollbackDir string) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name: "first",
		Size: 5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	fsStruct := gadget.VolumeStructure{
		Name:       "second",
		Size:       10 * quantity.SizeMiB,
		Filesystem: "ext4",
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/second-content", Target: "/"},
		},
	}
	lastStruct := gadget.VolumeStructure{
		Name:       "third",
		Size:       5 * quantity.SizeMiB,
		Filesystem: "vfat",
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/third-content", Target: "/"},
		},
	}
	// start with identical data for new and old infos, they get updated by
	// the caller as needed
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct, fsStruct, lastStruct},
			},
		},
	}
	newInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{bareStruct, fsStruct, lastStruct},
			},
		},
	}

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

	rollbackDir = c.MkDir()
	return oldData, newData, rollbackDir
}

type mockUpdateProcessObserver struct {
	beforeWriteCalled int
	canceledCalled    int
	beforeWriteErr    error
	canceledErr       error
}

func (m *mockUpdateProcessObserver) Observe(op gadget.ContentOperation, sourceStruct *gadget.LaidOutStructure,
	targetRootDir, relativeTargetPath string, data *gadget.ContentChange) (gadget.ContentChangeAction, error) {
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
	oldData, newData, rollbackDir := updateDataSet(c)
	// update two structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)
		c.Assert(observer, Equals, muo)
		// TODO:UC20 verify observer

		switch updaterForStructureCalls {
		case 0:
			c.Check(ps.Name, Equals, "first")
			c.Check(ps.HasFilesystem(), Equals, false)
			c.Check(ps.Size, Equals, 5*quantity.SizeMiB)
			c.Check(ps.IsPartition(), Equals, true)
			// non MBR start offset defaults to 1MiB
			c.Check(ps.StartOffset, Equals, 1*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 1)
			c.Check(ps.LaidOutContent[0].Image, Equals, "first.img")
			c.Check(ps.LaidOutContent[0].Size, Equals, 900*quantity.SizeKiB)
		case 1:
			c.Check(ps.Name, Equals, "second")
			c.Check(ps.HasFilesystem(), Equals, true)
			c.Check(ps.Filesystem, Equals, "ext4")
			c.Check(ps.IsPartition(), Equals, true)
			c.Check(ps.Size, Equals, 10*quantity.SizeMiB)
			// foo's start offset + foo's size
			c.Check(ps.StartOffset, Equals, (1+5)*quantity.OffsetMiB)
			c.Assert(ps.LaidOutContent, HasLen, 0)
			c.Assert(ps.Content, HasLen, 1)
			c.Check(ps.Content[0].UnresolvedSource, Equals, "/second-content")
			c.Check(ps.Content[0].Target, Equals, "/")
		default:
			c.Fatalf("unexpected call")
		}
		updaterForStructureCalls++
		mu := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name] = true
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
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

func (u *updateTestSuite) TestUpdateApplyOnlyWhenNeeded(c *C) {
	oldData, newData, rollbackDir := updateDataSet(c)
	// first structure is updated
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 0
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	// second one is not, lower edition
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	// third one is not, same edition
	oldData.Info.Volumes["foo"].Structure[2].Update.Edition = 3
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Assert(psRootDir, Equals, newData.RootDir)
		c.Assert(psRollbackDir, Equals, rollbackDir)

		switch updaterForStructureCalls {
		case 0:
			// only called for the first structure
			c.Assert(ps.Name, Equals, "first")
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)

	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyErrorLayout(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name: "foo",
		Size: 5 * quantity.SizeMiB,
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

	newRootDir := c.MkDir()
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}

	rollbackDir := c.MkDir()

	// both old and new bare struct data is missing

	// cannot lay out the new volume when bare struct data is missing
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot lay out the new volume: cannot lay out structure #0 \("foo"\): content "first.img": .* no such file or directory`)

	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), quantity.SizeMiB, nil)

	// Update does not error out when when the bare struct data of the old volume is missing
	err = gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, Equals, gadget.ErrNoUpdate)
}

func (u *updateTestSuite) TestUpdateApplyErrorIllegalVolumeUpdate(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name: "foo",
		Size: 5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	bareStructUpdate := bareStruct
	bareStructUpdate.Name = "foo update"
	bareStructUpdate.Update.Edition = 1
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

	newRootDir := c.MkDir()
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}

	rollbackDir := c.MkDir()

	makeSizedFile(c, filepath.Join(oldRootDir, "first.img"), quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), 900*quantity.SizeKiB, nil)

	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot apply update to volume: cannot change the number of structures within volume from 1 to 2`)
}

func (u *updateTestSuite) TestUpdateApplyErrorIllegalStructureUpdate(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name: "foo",
		Size: 5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	fsStruct := gadget.VolumeStructure{
		Name:       "foo",
		Filesystem: "ext4",
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

	newRootDir := c.MkDir()
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}

	oldRootDir := c.MkDir()
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}

	rollbackDir := c.MkDir()

	makeSizedFile(c, filepath.Join(oldRootDir, "first.img"), quantity.SizeMiB, nil)

	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot update volume structure #0 \("foo"\): cannot change a bare structure to filesystem one`)
}

func (u *updateTestSuite) TestUpdateApplyErrorDifferentVolume(c *C) {
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
			// same volume info but using a different name
			"foo-new": oldInfo.Volumes["foo"],
		},
	}

	oldData := gadget.GadgetData{Info: oldInfo, RootDir: c.MkDir()}
	newData := gadget.GadgetData{Info: newInfo, RootDir: c.MkDir()}
	rollbackDir := c.MkDir()

	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Fatalf("unexpected call")
		return &mockUpdater{}, nil
	})
	defer restore()

	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot find entry for volume "foo" in updated gadget info`)
}

func (u *updateTestSuite) TestUpdateApplyUpdatesAreOptInWithDefaultPolicy(c *C) {
	// prepare the stage
	bareStruct := gadget.VolumeStructure{
		Name: "foo",
		Size: 5 * quantity.SizeMiB,
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

	newRootDir := c.MkDir()
	// same volume description
	newData := gadget.GadgetData{Info: oldInfo, RootDir: newRootDir}
	// different content, but updates are opt in
	makeSizedFile(c, filepath.Join(newRootDir, "first.img"), 900*quantity.SizeKiB, nil)

	rollbackDir := c.MkDir()

	muo := &mockUpdateProcessObserver{}

	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		c.Fatalf("unexpected call")
		return &mockUpdater{}, nil
	})
	defer restore()

	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, Equals, gadget.ErrNoUpdate)

	// nothing was updated
	c.Assert(muo.beforeWriteCalled, Equals, 0)
}

func policyDataSet(c *C) (oldData gadget.GadgetData, newData gadget.GadgetData, rollbackDir string) {
	oldData, newData, rollbackDir = updateDataSet(c)
	noPartitionStruct := gadget.VolumeStructure{
		Name: "no-partition",
		Type: "bare",
		Size: 5 * quantity.SizeMiB,
		Content: []gadget.VolumeContent{
			{Image: "first.img"},
		},
	}
	mbrStruct := gadget.VolumeStructure{
		Name:   "mbr",
		Role:   "mbr",
		Size:   446,
		Offset: asOffsetPtr(0),
	}

	oldVol := oldData.Info.Volumes["foo"]
	oldVol.Structure = append(oldVol.Structure, noPartitionStruct, mbrStruct)
	oldData.Info.Volumes["foo"] = oldVol

	newVol := newData.Info.Volumes["foo"]
	newVol.Structure = append(newVol.Structure, noPartitionStruct, mbrStruct)
	newData.Info.Volumes["foo"] = newVol

	c.Assert(oldData.Info.Volumes["foo"].Structure, HasLen, 5)
	c.Assert(newData.Info.Volumes["foo"].Structure, HasLen, 5)
	return oldData, newData, rollbackDir
}

func (u *updateTestSuite) TestUpdateApplyUpdatesArePolicyControlled(c *C) {
	oldData, newData, rollbackDir := policyDataSet(c)
	c.Assert(oldData.Info.Volumes["foo"].Structure, HasLen, 5)
	c.Assert(newData.Info.Volumes["foo"].Structure, HasLen, 5)
	// all structures have higher Edition, thus all would be updated under
	// the default policy
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3
	newData.Info.Volumes["foo"].Structure[3].Update.Edition = 4
	newData.Info.Volumes["foo"].Structure[4].Update.Edition = 5

	toUpdate := map[string]int{}
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		toUpdate[ps.Name]++
		return &mockUpdater{}, nil
	})
	defer restore()

	policySeen := map[string]int{}
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, func(_, to *gadget.LaidOutStructure) (bool, gadget.ResolvedContentFilterFunc) {
		policySeen[to.Name]++
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
	err = gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, func(_, to *gadget.LaidOutStructure) (bool, gadget.ResolvedContentFilterFunc) {
		policySeen[to.Name]++
		return to.Name == "second", nil
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

func (u *updateTestSuite) TestUpdateApplyUpdatesRemodelPolicy(c *C) {
	oldData, newData, rollbackDir := policyDataSet(c)

	// old structures have higher Edition, no update would occur under the default policy
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	oldData.Info.Volumes["foo"].Structure[2].Update.Edition = 3
	oldData.Info.Volumes["foo"].Structure[3].Update.Edition = 4
	oldData.Info.Volumes["foo"].Structure[4].Update.Edition = 5

	toUpdate := map[string]int{}
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		toUpdate[ps.Name] = toUpdate[ps.Name] + 1
		return &mockUpdater{}, nil
	})
	defer restore()

	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, gadget.RemodelUpdatePolicy, nil)
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
	oldData, newData, rollbackDir := updateDataSet(c)
	// update both structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
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
			c.Assert(ps.Name, Equals, "second")
			updater.backupCb = func() error {
				return errors.New("failed")
			}
		}
		updaterForStructureCalls++
		return updater, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot backup volume structure #1 \("second"\): failed`)

	// update was canceled before backup pass completed
	c.Check(muo.canceledCalled, Equals, 1)
	c.Check(muo.beforeWriteCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyUpdateFailsThenRollback(c *C) {
	oldData, newData, rollbackDir := updateDataSet(c)
	// update all structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	muo := &mockUpdateProcessObserver{}
	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	rollbackCalls := make(map[string]bool)
	updaterForStructureCalls := 0
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name] = true
				return nil
			},
			rollbackCb: func() error {
				rollbackCalls[ps.Name] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name] = true
				return nil
			},
		}
		if updaterForStructureCalls == 1 {
			c.Assert(ps.Name, Equals, "second")
			// fail update of 2nd structure
			updater.updateCb = func() error {
				updateCalls[ps.Name] = true
				return errors.New("failed")
			}
		}
		updaterForStructureCalls++
		return updater, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot update volume structure #1 \("second"\): failed`)
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

	oldData, newData, rollbackDir := updateDataSet(c)
	// update all structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	updateCalls := make(map[string]bool)
	backupCalls := make(map[string]bool)
	rollbackCalls := make(map[string]bool)
	updaterForStructureCalls := 0
	restore = gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updater := &mockUpdater{
			backupCb: func() error {
				backupCalls[ps.Name] = true
				return nil
			},
			rollbackCb: func() error {
				rollbackCalls[ps.Name] = true
				return nil
			},
			updateCb: func() error {
				updateCalls[ps.Name] = true
				return nil
			},
		}
		switch updaterForStructureCalls {
		case 1:
			c.Assert(ps.Name, Equals, "second")
			// rollback fails on 2nd structure
			updater.rollbackCb = func() error {
				rollbackCalls[ps.Name] = true
				return errors.New("rollback failed with different error")
			}
		case 2:
			c.Assert(ps.Name, Equals, "third")
			// fail update of 3rd structure
			updater.updateCb = func() error {
				updateCalls[ps.Name] = true
				return errors.New("update error")
			}
		}
		updaterForStructureCalls++
		return updater, nil
	})
	defer restore()

	// go go go
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	// preserves update error
	c.Assert(err, ErrorMatches, `cannot update volume structure #2 \("third"\): update error`)
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

	c.Check(logbuf.String(), testutil.Contains, `cannot update gadget: cannot update volume structure #2 ("third"): update error`)
	c.Check(logbuf.String(), testutil.Contains, `cannot rollback volume structure #1 ("second") update: rollback failed with different error`)
}

func (u *updateTestSuite) TestUpdateApplyBadUpdater(c *C) {
	oldData, newData, rollbackDir := updateDataSet(c)
	// update all structs
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2
	newData.Info.Volumes["foo"].Structure[2].Update.Edition = 3

	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		return nil, errors.New("bad updater for structure")
	})
	defer restore()

	// go go go
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, nil)
	c.Assert(err, ErrorMatches, `cannot prepare update for volume structure #0 \("first"\): bad updater for structure`)
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
	err = ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(rootDir, "/dev/disk/by-label/writable"))
	c.Assert(err, IsNil)
	mountInfo := `170 27 8:2 / /some/mount/point rw,relatime shared:58 - ext4 %s/dev/sdxxx2 rw
`
	restore := osutil.MockMountInfo(fmt.Sprintf(mountInfo, rootDir))
	defer restore()

	psBare := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "none",
			Size:       10 * quantity.SizeMiB,
		},
		StartOffset: 1 * quantity.OffsetMiB,
	}
	updater, err := gadget.UpdaterForStructure(psBare, gadgetRootDir, rollbackDir, nil)
	c.Assert(err, IsNil)
	c.Assert(updater, FitsTypeOf, &gadget.RawStructureUpdater{})

	psFs := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
			Size:       10 * quantity.SizeMiB,
			Label:      "writable",
		},
		StartOffset: 1 * quantity.OffsetMiB,
	}
	updater, err = gadget.UpdaterForStructure(psFs, gadgetRootDir, rollbackDir, nil)
	c.Assert(err, IsNil)
	c.Assert(updater, FitsTypeOf, &gadget.MountedFilesystemUpdater{})

	// trigger errors
	updater, err = gadget.UpdaterForStructure(psBare, gadgetRootDir, "", nil)
	c.Assert(err, ErrorMatches, "internal error: backup directory cannot be unset")
	c.Assert(updater, IsNil)
}

func (u *updateTestSuite) TestUpdaterMultiVolumesDoesNotError(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

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

	// a new multi volume gadget update gives no error
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, singleVolume, multiVolume, "some-rollback-dir", nil, nil)
	c.Assert(err, IsNil)
	// but it warns that nothing happens either
	c.Assert(logbuf.String(), testutil.Contains, "WARNING: gadget assests cannot be updated yet when multiple volumes are used")

	// same for old
	err = gadget.Update(&gadgettest.ModelCharacteristics{}, multiVolume, singleVolume, "some-rollback-dir", nil, nil)
	c.Assert(err, IsNil)
	c.Assert(strings.Count(logbuf.String(), "WARNING: gadget assests cannot be updated yet when multiple volumes are used"), Equals, 2)
}

func (u *updateTestSuite) TestUpdateApplyNoChangedContentInAll(c *C) {
	oldData, newData, rollbackDir := updateDataSet(c)
	// first structure is updated
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 0
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	// so is the second structure
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2

	muo := &mockUpdateProcessObserver{}
	expectedStructs := []string{"first", "second"}
	updateCalls := 0
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		mu := &mockUpdater{
			updateCb: func() error {
				c.Assert(expectedStructs, testutil.Contains, ps.Name)
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, Equals, gadget.ErrNoUpdate)
	// update called for 2 structures
	c.Assert(updateCalls, Equals, 2)
	// nothing was updated, but the backup pass still executed
	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyNoChangedContentInSome(c *C) {
	oldData, newData, rollbackDir := updateDataSet(c)
	// first structure is updated
	oldData.Info.Volumes["foo"].Structure[0].Update.Edition = 0
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1
	// so is the second structure
	oldData.Info.Volumes["foo"].Structure[1].Update.Edition = 1
	newData.Info.Volumes["foo"].Structure[1].Update.Edition = 2

	muo := &mockUpdateProcessObserver{}
	expectedStructs := []string{"first", "second"}
	updateCalls := 0
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		mu := &mockUpdater{
			updateCb: func() error {
				c.Assert(expectedStructs, testutil.Contains, ps.Name)
				updateCalls++
				if ps.Name == "first" {
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	// update called for 2 structures
	c.Assert(updateCalls, Equals, 2)
	// at least one structure had an update
	c.Assert(muo.beforeWriteCalled, Equals, 1)
	c.Assert(muo.canceledCalled, Equals, 0)
}

func (u *updateTestSuite) TestUpdateApplyObserverBeforeWriteErrs(c *C) {
	oldData, newData, rollbackDir := updateDataSet(c)
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1

	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot observe prepared update: before write fail`)
	// update was canceled before backup pass completed
	c.Check(muo.canceledCalled, Equals, 0)
	c.Check(muo.beforeWriteCalled, Equals, 1)
}

func (u *updateTestSuite) TestUpdateApplyObserverCanceledErrs(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	oldData, newData, rollbackDir := updateDataSet(c)
	newData.Info.Volumes["foo"].Structure[0].Update.Edition = 1

	backupErr := errors.New("backup fails")
	updateErr := errors.New("update fails")
	restore = gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, ErrorMatches, `cannot backup volume structure #0 .*: backup fails`)
	// canceled called after backup pass
	c.Check(muo.canceledCalled, Equals, 1)
	c.Check(muo.beforeWriteCalled, Equals, 0)

	c.Check(logbuf.String(), testutil.Contains, `cannot observe canceled prepare update: canceled fail`)

	// backup works, update fails, triggers another canceled call
	backupErr = nil
	err = gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, nil, muo)
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
		Name:       "foo",
		Size:       5 * quantity.SizeMiB,
		Filesystem: "ext4",
		Content: []gadget.VolumeContent{
			{UnresolvedSource: "/second-content", Target: "/"},
			{UnresolvedSource: "$kernel:ref/kernel-content", Target: "/"},
		},
	}
	oldInfo := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"foo": {
				Bootloader: "grub",
				Schema:     "gpt",
				Structure:  []gadget.VolumeStructure{fsStruct},
			},
		},
	}

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
	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
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
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, gadget.KernelUpdatePolicy, muo)
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

	restore := gadget.MockUpdaterForStructure(func(ps *gadget.LaidOutStructure, psRootDir, psRollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		panic("should not get called")
	})
	defer restore()

	// exercise KernelUpdatePolicy here
	err := gadget.Update(&gadgettest.ModelCharacteristics{}, oldData, newData, rollbackDir, gadget.KernelUpdatePolicy, muo)
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

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
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

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
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

	mod := &gadgettest.ModelCharacteristics{
		SystemSeed: true,
	}
	vols, err := gadgettest.LayoutMultiVolumeFromYaml(
		c.MkDir(),
		gadgettest.MultiVolumeUC20GadgetYaml,
		mod,
	)
	c.Assert(err, IsNil)

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(vols["pc"], "/dev/vda", nil)
	c.Assert(err, IsNil)
	c.Assert(traits, DeepEquals, gadgettest.VMSystemVolumeDeviceTraits)

	traitsExtra, err := gadget.DiskTraitsFromDeviceAndValidate(vols["foo"], "/dev/vdb", nil)
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

	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
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
	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
	c.Assert(err, ErrorMatches, `volume foo is not compatible with disk /dev/foo: cannot find gadget structure #1 \("ubuntu-data"\) on disk`)

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

	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
	c.Assert(err, ErrorMatches, `volume foo is not compatible with disk /dev/foo: cannot find disk partition /dev/foo2 \(starting at 1052672\) in gadget: on disk size 4096 \(4 KiB\) is smaller than gadget size 1572864000 \(1.46 GiB\)`)

	// same size is okay though
	mockDisk.Structure[1].SizeInBytes = 1500 * 1024 * 1024
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": mockDisk,
	})
	defer restore()

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
	c.Assert(err, IsNil)

	// it has the right size
	c.Assert(traits.Structure[1].Size, Equals, 1500*quantity.SizeMiB)

	// bigger is okay too
	mockDisk.Structure[1].SizeInBytes = 3200 * 1024 * 1024
	restore = disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": mockDisk,
	})
	defer restore()

	traits, err = gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/foo", nil)
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

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/mmcblk0", nil)
	c.Assert(err, IsNil)
	// we normally get nil returned, but for reasons, the traits object in
	// gadgettest is not nil
	c.Assert(traits.StructureEncryption, IsNil)
	traits.StructureEncryption = map[string]gadget.StructureEncryptionParameters{}
	c.Assert(traits, DeepEquals, gadgettest.ExpectedRaspiDiskVolumeDeviceTraits)
}

func (s *gadgetYamlTestSuite) TestDiskTraitsFromDeviceAndValidateImplicitSystemDataHappy(c *C) {
	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	lvol, err := gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, nil)
	c.Assert(err, IsNil)

	// the volume cannot be found with no opts set
	_, err = gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/sda", nil)
	c.Assert(err, ErrorMatches, `volume pc is not compatible with disk /dev/sda: cannot find disk partition /dev/sda3 \(starting at 54525952\) in gadget`)

	// with opts for pc then it can be found
	opts := &gadget.DiskVolumeValidationOptions{
		AllowImplicitSystemData: true,
	}

	traits, err := gadget.DiskTraitsFromDeviceAndValidate(lvol, "/dev/sda", opts)
	c.Assert(err, IsNil)

	c.Assert(traits, DeepEquals, gadgettest.UC16ImplicitSystemDataDeviceTraits)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingInvalidYAMLDoesNotBlockOverallRefresh(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

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

	preUC20 := true
	_, err = gadget.BuildNewVolumeToDeviceMapping(old, allLaidOutVolumes, preUC20)
	c.Assert(err, Equals, gadget.ErrSkipUpdateProceedRefresh)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingImplicitSystemDataUC16(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, &gadgettest.ModelCharacteristics{})
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
	err = ioutil.WriteFile(fakedevicepart, nil, 0644)
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

	preUC20 := true
	m, err := gadget.BuildNewVolumeToDeviceMapping(old, allLaidOutVolumes, preUC20)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.UC16ImplicitSystemDataDeviceTraits,
	})
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingPreUC20NonFatalError(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, &gadgettest.ModelCharacteristics{})
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

	preUC20 := true
	_, err = gadget.BuildNewVolumeToDeviceMapping(old, allLaidOutVolumes, preUC20)
	c.Assert(err, Equals, gadget.ErrSkipUpdateProceedRefresh)

	// it's a fatal error on UC20 though
	postUC20 := false
	_, err = gadget.BuildNewVolumeToDeviceMapping(old, allLaidOutVolumes, postUC20)
	c.Assert(err, Not(Equals), gadget.ErrSkipUpdateProceedRefresh)
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingUC20MultiVolume(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), gadgettest.MultiVolumeUC20GadgetYaml, &gadgettest.ModelCharacteristics{SystemSeed: true})
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
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("ubuntu-seed")))
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(fakedevicepart, nil, 0644)
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

	postUC20 := false
	m, err := gadget.BuildNewVolumeToDeviceMapping(old, allLaidOutVolumes, postUC20)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.VMSystemVolumeDeviceTraits,
	})
}

func (u *updateTestSuite) TestBuildNewVolumeToDeviceMappingUC20Encryption(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	allLaidOutVolumes, err := gadgettest.LayoutMultiVolumeFromYaml(c.MkDir(), gadgettest.RaspiSimplifiedYaml, &gadgettest.ModelCharacteristics{SystemSeed: true})
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
	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/mmcblk0p1")
	err = os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel", disks.BlkIDEncodeLabel("ubuntu-seed")))
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(fakedevicepart, nil, 0644)
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

	// mock key files such that the function realizes this device was installed
	// with encryption
	recoveryKey := filepath.Join(dirs.SnapFDEDir, "recovery.key")
	err = os.MkdirAll(filepath.Dir(recoveryKey), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(recoveryKey, nil, 0644)
	c.Assert(err, IsNil)

	postUC20 := false
	m, err := gadget.BuildNewVolumeToDeviceMapping(old, allLaidOutVolumes, postUC20)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pi": gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits,
	})
}
