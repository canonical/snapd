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
	"fmt"
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/testutil"
)

type partitionSuite struct {
	sfdisk *testutil.MockCmd
	dir    string
}

var _ = Suite(&partitionSuite{})

func (s *partitionSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	s.sfdisk = testutil.MockCommand(c, "sfdisk", fmt.Sprintf("cat > %s/input", s.dir))
}

func (s *partitionSuite) TearDownTest(c *C) {
	if s.sfdisk != nil {
		s.sfdisk.Restore()
		s.sfdisk = nil
	}
}

func (s *partitionSuite) input(c *C) string {
	data, err := ioutil.ReadFile(filepath.Join(s.dir, "input"))
	c.Assert(err, IsNil)
	return string(data)
}

func (s *partitionSuite) TestGPTHappy(c *C) {
	pv := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Schema: "gpt",
			ID:     "123-123",
		},
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		StructureLayout: []gadget.LaidOutStructure{
			{
				// does not appear as partition
				VolumeStructure: &gadget.VolumeStructure{
					Size: 128 * gadget.SizeKiB,
					Name: "not-visible",
					Type: "bare",
				},
				StartOffset: 128 * gadget.SizeKiB,
				Index:       0,
			}, {
				VolumeStructure: &gadget.VolumeStructure{
					Size: 4 * gadget.SizeMiB,
					Name: "foo",
					Type: "21686148-6449-6E6F-744E-656564454649",
					Role: "system-boot",
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       1,
			}, {
				VolumeStructure: &gadget.VolumeStructure{
					Size: 12 * gadget.SizeMiB,
					Name: "bar",
					Type: "21686148-6449-6E6F-744E-656564454650",
					Role: "system-data",
				},
				StartOffset: 5 * gadget.SizeMiB,
				Index:       2,
			},
		},
	}
	err := gadget.Partition("foo", pv)
	c.Assert(err, IsNil)
	c.Assert(s.input(c), Equals, `unit: sectors
label: gpt
first-lba: 34
label-id: 123-123

start=2048, size=8192, type=21686148-6449-6E6F-744E-656564454649, name="foo"
start=10240, size=24576, type=21686148-6449-6E6F-744E-656564454650, name="bar"
`)
	c.Assert(s.sfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "foo"},
	})
}

func (s *partitionSuite) TestMBRHappy(c *C) {
	pv := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Schema: "mbr",
			ID:     "0x123",
		},
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		StructureLayout: []gadget.LaidOutStructure{
			{
				// does not appear as partition
				VolumeStructure: &gadget.VolumeStructure{
					Size: 446,
					Name: "not-visible",
					Type: "mbr",
				},
				StartOffset: 0,
				Index:       0,
			}, {
				// does not appear as partition
				VolumeStructure: &gadget.VolumeStructure{
					Size: 128 * gadget.SizeKiB,
					Name: "not-visible",
					Type: "bare",
				},
				StartOffset: 128 * gadget.SizeKiB,
				Index:       1,
			}, {
				VolumeStructure: &gadget.VolumeStructure{
					Size:       128 * gadget.SizeMiB,
					Name:       "foo",
					Type:       "0C",
					Role:       "system-boot",
					Filesystem: "vfat",
				},
				StartOffset: 1 * gadget.SizeMiB,
				Index:       2,
			}, {
				VolumeStructure: &gadget.VolumeStructure{
					Size:       12 * gadget.SizeMiB,
					Name:       "bar",
					Role:       "system-data",
					Filesystem: "ext4",
					Label:      "writable",
				},
				StartOffset: 129 * gadget.SizeMiB,
				Index:       3,
			},
		},
	}
	err := gadget.Partition("foo", pv)
	c.Assert(err, IsNil)
	c.Assert(s.input(c), Equals, `unit: sectors
label: dos
label-id: 0x123

start=2048, size=262144, type=0C, bootable
start=264192, size=24576
`)
	c.Assert(s.sfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "foo"},
	})
}

func (s *partitionSuite) TestHybridType(c *C) {
	ps := gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 2 * gadget.SizeMiB,
			Type: "0C,21686148-6449-6E6F-744E-656564454649",
		},
		StartOffset: 1 * gadget.SizeMiB,
	}
	pvGPT := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Schema: "gpt",
		},
		Size:            3 * gadget.SizeMiB,
		SectorSize:      512,
		StructureLayout: []gadget.LaidOutStructure{ps},
	}

	err := gadget.Partition("foo", pvGPT)
	c.Assert(err, IsNil)
	c.Assert(s.input(c), Equals, `unit: sectors
label: gpt
first-lba: 34

start=2048, size=4096, type=21686148-6449-6E6F-744E-656564454649
`)

	pvMBR := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Schema: "mbr",
		},
		Size:            3 * gadget.SizeMiB,
		SectorSize:      512,
		StructureLayout: []gadget.LaidOutStructure{ps},
	}
	err = gadget.Partition("foo", pvMBR)
	c.Assert(err, IsNil)
	c.Assert(s.input(c), Equals, `unit: sectors
label: dos

start=2048, size=4096, type=0C
`)
}

func (s *partitionSuite) TestInputErrors(c *C) {
	pv := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Schema: "gpt",
		},
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		StructureLayout: []gadget.LaidOutStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 2 * gadget.SizeMiB,
					Type: "0C,21686148-6449-6E6F-744E-656564454649",
				},
				StartOffset: 1 * gadget.SizeMiB,
			},
		},
	}

	err := gadget.Partition("", pv)
	c.Assert(err, ErrorMatches, "internal error: image path is unset")
	c.Assert(s.sfdisk.Calls(), HasLen, 0)

	// unsupported sector size
	pv.SectorSize = 384

	err = gadget.Partition("foo", pv)
	c.Assert(err, ErrorMatches, "cannot use sector size 384")
	c.Assert(s.sfdisk.Calls(), HasLen, 0)
}

func (s *partitionSuite) TestCommandError(c *C) {
	pv := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Schema: "gpt",
		},
		Size:       3 * gadget.SizeMiB,
		SectorSize: 512,
		StructureLayout: []gadget.LaidOutStructure{
			{
				VolumeStructure: &gadget.VolumeStructure{
					Size: 2 * gadget.SizeMiB,
					Type: "0C,21686148-6449-6E6F-744E-656564454649",
				},
				StartOffset: 1 * gadget.SizeMiB,
			},
		},
	}

	sfdiskBad := testutil.MockCommand(c, "sfdisk", "echo 'failed'; false")
	defer sfdiskBad.Restore()

	err := gadget.Partition("foo", pv)
	c.Assert(err, ErrorMatches, "cannot partition image using sfdisk: failed")
	c.Assert(s.sfdisk.Calls(), HasLen, 0)
}
