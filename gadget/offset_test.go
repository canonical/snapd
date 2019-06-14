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
	"io"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
)

type offsetSuite struct{}

var _ = Suite(&offsetSuite{})

func (m *offsetSuite) TestOffsetWriterOnlyStructure(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:        1 * gadget.SizeMiB,
			OffsetWrite: &gadget.RelativeOffset{Offset: 512},
		},
		StartOffset: 1024,
		// start offset written at this location
		PositionedOffsetWrite: asSizePtr(512),

		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
				},
				Size:        128,
				StartOffset: 2048,
			},
		},
	}

	const sectorSize = 512
	ow := gadget.NewOffsetWriter(ps, sectorSize)

	mw := &mockWriteSeeker{
		seek: func(offs int64, whence int) (int64, error) {
			c.Assert(offs, Equals, int64(512))
			c.Assert(whence, Equals, io.SeekStart)
			return offs, nil
		},
		write: func(what []byte) (int, error) {
			// start-offset / sector-size -> 1024 / 512 -> 2
			// 0x2 -> little endian 0x02 0x00 0x00 0x00
			c.Assert(what, DeepEquals, []byte{0x02, 0x00, 0x00, 0x00})
			return len(what), nil
		},
	}
	err := ow.Write(mw)
	c.Assert(err, IsNil)
}

func (m *offsetSuite) TestOffsetWriterOnlyRawContent(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 1 * gadget.SizeMiB,
		},
		StartOffset: gadget.Size(1024),

		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
					// absolute within the volume
					OffsetWrite: &gadget.RelativeOffset{Offset: 4096},
				},
				Size:        128,
				StartOffset: 2048,
				// start offset written here
				PositionedOffsetWrite: asSizePtr(4096),
			},
		},
	}

	const sectorSize = 512
	ow := gadget.NewOffsetWriter(ps, sectorSize)

	mw := &mockWriteSeeker{
		seek: func(offs int64, whence int) (int64, error) {
			c.Assert(offs, Equals, int64(4096))
			c.Assert(whence, Equals, io.SeekStart)
			return offs, nil
		},
		write: func(what []byte) (int, error) {
			// start-offset / sector-size -> 2048 / 512 -> 4
			// 0x4 -> little endian 0x04 0x00 0x00 0x00
			c.Assert(what, DeepEquals, []byte{0x04, 0x00, 0x00, 0x00})
			return 0, nil
		},
	}
	err := ow.Write(mw)
	c.Assert(err, IsNil)
}

func (m *offsetSuite) TestOffsetWriterOnlyFsStructure(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       1 * gadget.SizeMiB,
			Filesystem: "ext4",
			// same as in pc gadget
			OffsetWrite: &gadget.RelativeOffset{Offset: 92},
		},
		StartOffset:           gadget.Size(348 * gadget.SizeKiB),
		PositionedOffsetWrite: asSizePtr(92),
	}

	const sectorSize = 512
	ow := gadget.NewOffsetWriter(ps, sectorSize)

	mw := &mockWriteSeeker{
		seek: func(offs int64, whence int) (int64, error) {
			c.Assert(offs, Equals, int64(92))
			c.Assert(whence, Equals, io.SeekStart)
			return offs, nil
		},
		write: func(what []byte) (int, error) {
			// start-offset / sector-size -> 356352 / 512 -> 696
			// 0x2b8 -> little endian 0xb8 0x02 0x00 0x00
			c.Assert(what, DeepEquals, []byte{0xb8, 0x02, 0x00, 0x00})
			return 0, nil
		},
	}
	err := ow.Write(mw)
	c.Assert(err, IsNil)
}

func (m *offsetSuite) TestOffsetWriterErrors(c *C) {
	ps := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size:       1 * gadget.SizeMiB,
			Filesystem: "ext4",
			// same as in pc gadget
			OffsetWrite: &gadget.RelativeOffset{Offset: 92},
		},
		StartOffset:           gadget.Size(348 * gadget.SizeKiB),
		PositionedOffsetWrite: asSizePtr(92),
	}

	const sectorSize = 512
	ow := gadget.NewOffsetWriter(ps, sectorSize)

	mwBadSeeker := &mockWriteSeeker{
		seek: func(offs int64, whence int) (int64, error) {
			return 0, errors.New("bad seeker")
		},
		write: func(what []byte) (int, error) {
			return 0, errors.New("unexpected call")
		},
	}
	err := ow.Write(mwBadSeeker)
	c.Assert(err, ErrorMatches, "cannot seek to offset 92: bad seeker")

	mwBadWriter := &mockWriteSeeker{
		seek: func(offs int64, whence int) (int64, error) {
			return offs, nil
		},
		write: func(what []byte) (int, error) {
			return 0, errors.New("bad writer")
		},
	}
	err = ow.Write(mwBadWriter)
	c.Assert(err, ErrorMatches, "cannot write LBA value 0x2b8 at offset 92: bad writer")

	psOnlyContent := &gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Size: 1 * gadget.SizeMiB,
		},
		StartOffset: gadget.Size(348 * gadget.SizeKiB),
		PositionedContent: []gadget.PositionedContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "foo.img",
					// absolute within the volume
					OffsetWrite: &gadget.RelativeOffset{Offset: 4096},
				},
				Size:        128,
				StartOffset: 2048,
				// start offset written here
				PositionedOffsetWrite: asSizePtr(4096),
			},
		},
	}

	ow = gadget.NewOffsetWriter(psOnlyContent, sectorSize)
	err = ow.Write(mwBadWriter)
	c.Assert(err, ErrorMatches, "cannot write LBA value 0x4 at offset 4096: bad writer")
}
