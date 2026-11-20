// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package blockplan_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type imageSuite struct{}

var _ = Suite(&imageSuite{})

// TestGeometryAndExtents is the premise the whole format rests on: the extents
// derived from the inode table tile the data region exactly, so every byte
// between the superblock and the inode table is a block some inode describes and
// can therefore be copied verbatim.
func (s *imageSuite) TestGeometryAndExtents(c *C) {
	requireTools(c, "mksquashfs", "xz")
	ctx := context.Background()
	img := buildImage(c, "mixed.snap", populateMixed)

	im, err := blockplan.OpenImage(img)
	c.Assert(err, IsNil)
	c.Assert(im.CheckSupported(), IsNil, Commentf("a snapd-style image was refused"))
	c.Assert(im.SB.BlockSize, Equals, uint32(testBlockSize))

	ext, gaps, overlaps, err := im.CheckCoverage(ctx)
	c.Assert(err, IsNil)
	c.Check(gaps, HasLen, 0, Commentf("data region has gaps: %v", gaps))
	c.Check(overlaps, HasLen, 0, Commentf("data region has overlaps: %v", overlaps))

	var onDisk int64
	sawRaw, sawPartial := false, false
	for _, e := range ext {
		onDisk += int64(e.CSize)
		if e.Raw {
			sawRaw = true
			c.Check(e.CSize, Equals, e.USize,
				Commentf("raw extent at %d occupies %d bytes for %d uncompressed", e.Offset, e.CSize, e.USize))
		} else {
			c.Check(e.CSize < e.USize, Equals, true,
				Commentf("compressed extent at %d is %d bytes for %d uncompressed, so it should have been stored raw",
					e.Offset, e.CSize, e.USize))
		}
		if e.USize < int(im.SB.BlockSize) {
			sawPartial = true
		}
	}
	c.Check(onDisk, Equals, im.DataRegionEnd()-blockplan.SuperblockSize,
		Commentf("the extents do not occupy the whole data region"))
	c.Check(sawRaw, Equals, true, Commentf("no raw block in the fixture, so the raw path is untested"))
	c.Check(sawPartial, Equals, true, Commentf("no partial tail in the fixture, so the partial path is untested"))

	// Both file inode types must appear: the sparse file forces an extended
	// inode, everything else is basic.
	inodes, err := im.FileInodes(ctx)
	c.Assert(err, IsNil)
	types := map[uint16]int{}
	sparseHoles, dupSized := 0, 0
	for _, fi := range inodes {
		types[fi.Type]++
		for _, w := range fi.Sizes {
			if w == 0 {
				sparseHoles++
			}
		}
		if fi.FileSize == 400000 {
			dupSized++
		}
	}
	c.Check(types[blockplan.InodeTypeFile] > 0, Equals, true,
		Commentf("no basic file inode (type 2) in the fixture, got types %v", types))
	c.Check(types[blockplan.InodeTypeExtFile] > 0, Equals, true,
		Commentf("no extended file inode (type 9) in the fixture, got types %v", types))
	c.Check(sparseHoles > 0, Equals, true,
		Commentf("no sparse hole in the fixture, so the zero-size-word path is untested"))

	// Dedup: two inodes describe the same 400000 bytes, and the extents must
	// report those blocks once, not twice -- which is how the delta ships
	// shared bytes once and keeps both inodes working.
	c.Assert(dupSized >= 2, Equals, true,
		Commentf("expected two inodes of 400000 bytes for the dedup case, found %d", dupSized))
	seen := map[int64]bool{}
	for _, e := range ext {
		c.Check(seen[e.Offset], Equals, false, Commentf("extent at %d reported twice", e.Offset))
		seen[e.Offset] = true
	}
}

// TestRefusals covers the layouts mksquashfs can be asked for that this format
// cannot replay. Each has to be refused rather than mis-described: snapd answers
// a refusal with a full download, so it costs bandwidth and nothing else, while
// an image that is mis-described costs correctness.
func (s *imageSuite) TestRefusals(c *C) {
	requireTools(c, "mksquashfs", "xz")
	tests := []struct {
		name string
		// args replaces snapd's whole argument list; extra appends to it.
		args  []string
		extra []string
	}{
		// Fragments pack several small files into a shared block the extent
		// walk does not describe. -no-fragments has to go: mksquashfs keeps
		// it whatever follows, so appending -always-use-fragments only sets
		// the flag and still writes no fragments.
		{name: "fragments", args: []string{
			"-noappend", "-comp", "xz", "-always-use-fragments", "-no-progress", "-all-root", "-no-xattrs",
		}},
		// The default: fragments on, which is what an image not built by
		// snapd looks like.
		{name: "default fragments", args: []string{
			"-noappend", "-comp", "xz", "-no-progress", "-all-root", "-no-xattrs",
		}},
		// A BCJ filter is recorded as COMPRESSOR_OPTIONS, and its filter
		// chain is not what the applier reproduces.
		{name: "bcj filter", extra: []string{"-Xbcj", "x86"}},
		// So is a dictionary that differs from the block size.
		{name: "xz dict size", extra: []string{"-Xdict-size", "32K"}},
		// gzip is a different compressor id entirely, and mksquashfs refuses
		// a second -comp, so this one also needs the full list.
		{name: "gzip", args: []string{
			"-noappend", "-comp", "gzip", "-no-fragments", "-no-progress", "-all-root", "-no-xattrs",
		}},
		// No export table means inodes cannot be enumerated without walking
		// directories, which the generator deliberately does not do.
		{name: "no exports", extra: []string{"-no-exports"}},
	}
	for _, tc := range tests {
		var img string
		if tc.args != nil {
			img = buildImageArgs(c, "bad.snap", populateMixed, tc.args...)
		} else {
			img = buildImage(c, "bad.snap", populateMixed, tc.extra...)
		}
		im, err := blockplan.OpenImage(img)
		if err != nil {
			// Refusing to parse at all is also a refusal.
			continue
		}
		c.Check(im.CheckSupported(), NotNil,
			Commentf("%s: an image built with %v%v was accepted:\n%s", tc.name, tc.args, tc.extra, im.SB))
	}
}

// testBytesUsed is the bytes_used of the synthetic superblock below, small
// enough to read at a glance and not a multiple of the padding, so the padding
// rule has something to compute.
const testBytesUsed = 200000

// validSuperblockBytes returns the 96 bytes of a superblock the geometry check
// accepts, for a test to change one field of and see which rule fires.
//
// It is synthesized rather than copied out of a real image because most of the
// rules guard against superblocks mksquashfs would never write -- and because
// this is the shape the applier vets: the target's superblock arrives in SEC_SB
// as bytes, long before the image it describes exists.
func validSuperblockBytes() []byte {
	raw := make([]byte, blockplan.SuperblockSize)
	le := binary.LittleEndian
	le.PutUint32(raw[0:], blockplan.SquashfsMagic)
	le.PutUint32(raw[4:], 10)             // inode count
	le.PutUint32(raw[8:], 1700000000)     // modification time
	le.PutUint32(raw[12:], testBlockSize) // block size
	le.PutUint32(raw[16:], 0)             // fragment entries
	le.PutUint16(raw[20:], blockplan.CompressorXz)
	le.PutUint16(raw[22:], 17)                // block log
	le.PutUint16(raw[24:], 0)                 // flags
	le.PutUint16(raw[26:], 1)                 // id count
	le.PutUint16(raw[28:], 4)                 // major version
	le.PutUint16(raw[30:], 0)                 // minor version
	le.PutUint64(raw[32:], 0)                 // root inode reference
	le.PutUint64(raw[40:], testBytesUsed)     // bytes used
	le.PutUint64(raw[48:], 195000)            // id table
	le.PutUint64(raw[56:], blockplan.NoTable) // xattr table: absent
	le.PutUint64(raw[64:], 150000)            // inode table
	le.PutUint64(raw[72:], 160000)            // directory table
	le.PutUint64(raw[80:], 12345)             // fragment table: a stale write position
	le.PutUint64(raw[88:], 170000)            // export table
	return raw
}

// TestSuperblockGeometryRefusals drives the geometry check from the bytes alone,
// which is how the applier reaches it. No tools and no image involved, so these
// rules are covered on every machine.
func (s *imageSuite) TestSuperblockGeometryRefusals(c *C) {
	padded := blockplan.PaddedImageSize(testBytesUsed)

	sb, err := blockplan.ParseSuperblock(validSuperblockBytes())
	c.Assert(err, IsNil)
	c.Assert(sb.CheckSupportedGeometry(padded), IsNil,
		Commentf("the fixture superblock is not itself acceptable:\n%s", sb))

	le := binary.LittleEndian
	tests := []struct {
		name   string
		mutate func(raw []byte)
		// imageSize overrides the padded length of the fixture, for the two
		// rules that compare the superblock against the file it came from.
		imageSize int64
		err       string
	}{{
		name:   "a later minor version",
		mutate: func(raw []byte) { le.PutUint16(raw[30:], 1) },
		err:    `unsupported squashfs version 4\.1`,
	}, {
		name:   "a compressor this build has no codec for",
		mutate: func(raw []byte) { le.PutUint16(raw[20:], 1) },
		err:    `unsupported compressor gzip, this build implements .*`,
	}, {
		name:   "a compressor options record",
		mutate: func(raw []byte) { le.PutUint16(raw[24:], blockplan.FlagCompressorOptions) },
		err:    `image carries COMPRESSOR_OPTIONS, whose filter chain is not reproduced`,
	}, {
		name:   "fragments",
		mutate: func(raw []byte) { le.PutUint32(raw[16:], 3) },
		err:    `image has 3 fragments, which are not described by the extent walk`,
	}, {
		// The xattr tables ride along in the verbatim tail, so they are only
		// carried at all if they really are above the export table.
		name:   "an xattr table below the export table",
		mutate: func(raw []byte) { le.PutUint64(raw[56:], 169000) },
		err:    `image has an xattr table at 169000, below the export table at 170000, .*`,
	}, {
		name:   "no export table",
		mutate: func(raw []byte) { le.PutUint64(raw[88:], blockplan.NoTable) },
		err:    `image has no export table, .*`,
	}, {
		name:   "a block log that disagrees with the block size",
		mutate: func(raw []byte) { le.PutUint16(raw[22:], 16) },
		err:    `block size 131072 disagrees with block log 16`,
	}, {
		// The applier names the block size as an LZMA2 dictionary in a single
		// property byte, so a block size LZMA2 cannot express is unusable
		// even though squashfs itself would allow it.
		name: "a block size no LZMA2 dictionary can express",
		mutate: func(raw []byte) {
			le.PutUint32(raw[12:], 2048)
			le.PutUint16(raw[22:], 11)
		},
		err: `block size 2048 cannot be an LZMA2 dictionary size: .*`,
	}, {
		name:   "more bytes used than the file holds",
		mutate: func(raw []byte) { le.PutUint64(raw[40:], 300000) },
		err:    `superblock claims 300000 bytes used but the file is 200704`,
	}, {
		name:   "an inode table above the export table",
		mutate: func(raw []byte) { le.PutUint64(raw[64:], 180000) },
		err:    `table pointers are not ordered: inode=180000 export=170000 bytes_used=200000`,
	}, {
		// The image is zero-padded to the next 4 KiB boundary and no further,
		// which is what lets the applier know when it is finished.
		name:      "a length that is not the padded bytes used",
		mutate:    func(raw []byte) {},
		imageSize: testBytesUsed,
		err:       `image is 200000 bytes, but 200000 bytes used pads to 200704`,
	}}

	for _, tc := range tests {
		comment := Commentf("%s", tc.name)
		raw := validSuperblockBytes()
		tc.mutate(raw)
		size := tc.imageSize
		if size == 0 {
			size = padded
		}
		sb, err := blockplan.ParseSuperblock(raw)
		c.Assert(err, IsNil, comment)
		c.Check(sb.CheckSupportedGeometry(size), ErrorMatches, tc.err, comment)
	}
}

func (s *imageSuite) TestParseSuperblockRefusesNonImages(c *C) {
	_, err := blockplan.ParseSuperblock(make([]byte, 32))
	c.Check(err, ErrorMatches, `short superblock: 32 bytes`)

	raw := validSuperblockBytes()
	binary.LittleEndian.PutUint32(raw[0:], 0xdeadbeef)
	_, err = blockplan.ParseSuperblock(raw)
	c.Check(err, ErrorMatches, `not a squashfs image \(magic 0xdeadbeef\)`)
}

// populateXattr is populateMixed plus real extended attributes, which is what
// makes mksquashfs emit an xattr table. Note it takes a file that really has
// one: -xattrs on a tree without any produces an image with NO_XATTRS clear and
// no xattr table at all, which is exactly the core26 shape and why the geometry
// check gates on the table pointer rather than the flag.
func populateXattr(c *C, dir string, value string) {
	populateMixed(c, dir)
	for _, name := range []string{"sub/small.txt", "multi.txt"} {
		if err := unix.Setxattr(filepath.Join(dir, name), "user.test", []byte(value), 0); err != nil {
			c.Skip(fmt.Sprintf("cannot set an xattr under %s: %v", dir, err))
		}
	}
}

// TestXattrTablesRideInTheVerbatimTail pins down that an xattr table needs no
// work of its own. It costs nothing because mksquashfs writes the xattr value
// blocks and id table after every other table, so they land above
// export_table_start and travel verbatim in SEC_MDTAIL -- and because the inode
// walk is driven by the export table and both extended inode layouts carry their
// xattr word after the fields it reads. The test asserts that premise, not just
// the outcome, since the outcome would also hold if mksquashfs stopped putting
// the table up there and the delta silently dropped it. The round trip over such
// an image arrives with the generator.
func (s *imageSuite) TestXattrTablesRideInTheVerbatimTail(c *C) {
	requireTools(c, "mksquashfs", "xz")
	img := buildImage(c, "xattr.snap", func(c *C, dir string) {
		populateXattr(c, dir, "hello")
	}, "-xattrs")

	im, err := blockplan.OpenImage(img)
	c.Assert(err, IsNil)
	c.Assert(im.SB.XattrTableStart != blockplan.NoTable, Equals, true,
		Commentf("the image has no xattr table, so the fixture proves nothing"))
	c.Assert(im.CheckSupported(), IsNil)

	// The value blocks are the lowest xattr byte -- the superblock points at
	// the id table header, which is written after them -- and the whole point
	// is that even they sit inside [export_table_start, bytes_used).
	values := binary.LittleEndian.Uint64(im.Data[im.SB.XattrTableStart:])
	c.Check(values >= im.SB.ExportTableStart, Equals, true,
		Commentf("xattr value blocks at %d, below the export table at %d", values, im.SB.ExportTableStart))
	c.Check(values < im.SB.BytesUsed, Equals, true,
		Commentf("xattr value blocks at %d, past bytes_used %d", values, im.SB.BytesUsed))
}

// TestNonDefaultBlockSizeIsAccepted pins down what is not a refusal: a block
// size other than snapd's 128 KiB, since all the format needs from it is that
// LZMA2 can name it as a dictionary size. Only a source and target that disagree
// about it are refused, which the generator's tests cover.
func (s *imageSuite) TestNonDefaultBlockSizeIsAccepted(c *C) {
	requireTools(c, "mksquashfs", "xz")
	img := buildImage(c, "small-blocks.snap", populateMixed, "-b", "64K")

	im, err := blockplan.OpenImage(img)
	c.Assert(err, IsNil)
	c.Assert(im.SB.BlockSize, Equals, uint32(64<<10),
		Commentf("mksquashfs did not produce 64 KiB blocks"))
	c.Check(im.CheckSupported(), IsNil)

	_, gaps, overlaps, err := im.CheckCoverage(context.Background())
	c.Assert(err, IsNil)
	c.Check(gaps, HasLen, 0, Commentf("data region has gaps: %v", gaps))
	c.Check(overlaps, HasLen, 0, Commentf("data region has overlaps: %v", overlaps))
}
