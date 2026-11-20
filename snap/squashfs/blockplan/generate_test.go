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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type generateSuite struct{}

var _ = Suite(&generateSuite{})

// TestIdentity is the simplest possible delta: source and target are the same
// image, so the whole data region is one copy and no metadata patch is needed at
// all.
func (s *generateSuite) TestIdentity(c *C) {
	requireTools(c, "mksquashfs", "xz", "hdiffz", "hpatchz")
	img := buildImage(c, "same.snap", populateMixed)
	delta := filepath.Join(c.MkDir(), "d.delta")

	stats, err := blockplan.Generate(context.Background(), img, img, delta, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true,
	})
	c.Assert(err, IsNil)
	c.Check(stats.Instructions, Equals, 1)
	c.Check(stats.Copies, Equals, 1)
	c.Check(stats.Literals, Equals, 0)
	c.Check(stats.MDPatchBytes, Equals, 0,
		Commentf("identity delta carries a %d-byte metadata patch", stats.MDPatchBytes))
	c.Check(stats.CopiedBytes, Equals, stats.TargetDataBytes)
	// It should also be tiny: a header, a section table and the superblock.
	c.Check(stats.DeltaSize < 4096, Equals, true, Commentf("identity delta is %d bytes", stats.DeltaSize))
}

// TestNilOptsAreTheZeroValue holds what a caller with no opinion gets: the
// compressor from the image, the measured run cap, and a delta that applies.
// Nothing here may need to be told what built the images.
func (s *generateSuite) TestNilOptsAreTheZeroValue(c *C) {
	requireTools(c, "mksquashfs", "xz", "hdiffz", "hpatchz")
	source := buildImage(c, "source.snap", populateMixed)
	target := buildImage(c, "target.snap", func(c *C, dir string) {
		populateMixed(c, dir)
		smallEdit(c, dir)
	})
	delta := filepath.Join(c.MkDir(), "d.delta")

	stats, err := blockplan.Generate(context.Background(), source, target, delta, nil)
	c.Assert(err, IsNil)
	c.Check(stats.CopiedBytes > 0, Equals, true, Commentf("nothing was copied verbatim"))
	applyAndCompare(c, source, delta, target, nil)
}

// TestUnrelatedImages is the negative control: two images with almost nothing in
// common must still produce a correct delta, merely a large one.
func (s *generateSuite) TestUnrelatedImages(c *C) {
	requireTools(c, "mksquashfs", "xz", "hdiffz", "hpatchz")
	source := buildImage(c, "source.snap", populateMixed)
	target := buildImage(c, "target.snap", func(c *C, dir string) {
		writeFile(c, dir, "everything.bin", incompressible(500000, 99))
		writeFile(c, dir, "else.txt", compressibleText(250000, "unrelated"))
	})
	delta := filepath.Join(c.MkDir(), "d.delta")

	stats, err := blockplan.Generate(context.Background(), source, target, delta, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true,
	})
	c.Assert(err, IsNil, Commentf("generating a delta between unrelated images"))
	// Verify already proved it reconstructs; what matters here is that the
	// generator did not pretend to find reuse that is not there.
	c.Check(stats.CopiedBytes <= stats.TargetDataBytes/10, Equals, true,
		Commentf("claimed to copy %d of %d bytes between unrelated images",
			stats.CopiedBytes, stats.TargetDataBytes))
}

// TestRoundTripsXattrImages is the round trip the image tests defer to here: an
// image with xattrs is supported because its tables ride inside the verbatim
// tail, and nothing but a generate and an apply proves that they come back.
func (s *generateSuite) TestRoundTripsXattrImages(c *C) {
	requireTools(c, "mksquashfs", "xz", "hdiffz", "hpatchz")
	source := buildImage(c, "xattr-source.snap", func(c *C, dir string) {
		populateXattr(c, dir, "hello")
	}, "-xattrs")
	target := buildImage(c, "xattr-target.snap", func(c *C, dir string) {
		populateXattr(c, dir, "goodbye")
	}, "-xattrs")

	im, err := blockplan.OpenImage(target)
	c.Assert(err, IsNil)
	c.Assert(im.SB.XattrTableStart != blockplan.NoTable, Equals, true,
		Commentf("the fixture has no xattr table, so it proves nothing"))

	delta := filepath.Join(c.MkDir(), "d.delta")
	_, err = blockplan.Generate(context.Background(), source, target, delta, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true,
	})
	c.Assert(err, IsNil)
	applyAndCompare(c, source, delta, target, newXZ(c))
}

// TestNonDefaultBlockSizeRoundTrips pins down what is not a refusal: a block size
// other than snapd's 128 KiB, since all the format needs from it is that the
// compressor can name it as a dictionary size.
func (s *generateSuite) TestNonDefaultBlockSizeRoundTrips(c *C) {
	requireTools(c, "mksquashfs", "xz", "hdiffz", "hpatchz")
	source := buildImage(c, "small-source.snap", populateMixed, "-b", "64K")
	target := buildImage(c, "small-target.snap", func(c *C, dir string) {
		populateMixed(c, dir)
		smallEdit(c, dir)
	}, "-b", "64K")

	delta := filepath.Join(c.MkDir(), "d.delta")
	_, err := blockplan.Generate(context.Background(), source, target, delta, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true,
	})
	c.Assert(err, IsNil, Commentf("generating a delta between 64 KiB-block images"))
	applyAndCompare(c, source, delta, target, newXZ(c))
}

// TestRefusesImagesThatDisagree covers the two ways a pair cannot be delta'd at
// all. Both are properties of the pair rather than of either image, so neither
// can be caught when an image is opened -- and getting them wrong would not
// produce an error, it would produce blocks that are valid and wrong.
func (s *generateSuite) TestRefusesImagesThatDisagree(c *C) {
	requireTools(c, "mksquashfs", "xz")
	ctx := context.Background()
	delta := filepath.Join(c.MkDir(), "d.delta")

	// A copied block only stays valid under the dictionary it was compressed
	// with, and the block size is that dictionary.
	small := buildImage(c, "small-blocks.snap", populateMixed, "-b", "64K")
	big := buildImage(c, "big-blocks.snap", populateMixed)
	_, err := blockplan.Generate(ctx, small, big, delta, &blockplan.GenerateOpts{Comp: newXZ(c)})
	c.Check(err, ErrorMatches, `source uses 65536-byte blocks and target 131072`)

	// A source compressed by another codec holds no block this one can copy,
	// and the applier only ever has one compressor.
	//
	// Reaching this needs a build where both codecs are implemented, since an
	// image whose codec is not implemented is refused one step earlier, by
	// checkSupportedGeometry -- which is where image_test.go holds that case. A
	// stub registered under gzip's id supplies the missing half: the refusal
	// happens before either compressor is asked to do anything, so a stub that
	// cannot compress is enough to prove the pair is what was rejected.
	restore := blockplan.MockCompressor(1, func(jobs int) (blockplan.Compressor, error) {
		return &stubCompressor{id: 1, codec: 1}, nil
	}, 1, stubBlobDecoder)
	defer restore()
	other := buildImageArgs(c, "gzip.snap", populateMixed,
		"-noappend", "-comp", "gzip", "-no-fragments", "-no-progress", "-all-root", "-no-xattrs")
	_, err = blockplan.Generate(ctx, other, big, delta, &blockplan.GenerateOpts{Comp: newXZ(c)})
	c.Check(err, ErrorMatches, `source uses compressor gzip and target xz`)
}

// TestRefusesARunCapBelowOneBlock is the other end of MaxRunUSize: a cap that
// cannot hold a single block would make every run empty, which is a caller
// mistake rather than something to work around.
func (s *generateSuite) TestRefusesARunCapBelowOneBlock(c *C) {
	requireTools(c, "mksquashfs", "xz")
	img := buildImage(c, "capped.snap", populateMixed)
	_, err := blockplan.Generate(context.Background(), img, img, filepath.Join(c.MkDir(), "d.delta"),
		&blockplan.GenerateOpts{Comp: newXZ(c), MaxRunUSize: 4096})
	c.Check(err, ErrorMatches, `run cap 4096 is below one block \(131072\)`)
}

// TestCompareWriter covers the generator's final gate directly. It compares the
// reconstruction as it arrives rather than buffering it, so its failure cases are
// the only thing standing between a bad delta and a published one -- and unlike
// the gate as a whole, they can be provoked exactly.
func (s *generateSuite) TestCompareWriter(c *C) {
	want := []byte("the reconstruction must match this exactly")

	// Streaming in pieces: the applier writes the image in many small pieces,
	// and none of them is aligned to anything.
	w := blockplan.NewCompareWriter(want)
	for i := 0; i < len(want); i += 7 {
		end := i + 7
		if end > len(want) {
			end = len(want)
		}
		n, err := w.Write(want[i:end])
		c.Assert(err, IsNil, Commentf("writing [%d,%d)", i, end))
		c.Assert(n, Equals, end-i)
	}
	c.Check(w.At(), Equals, len(want))

	// A difference has to be located, not merely reported: on a real image the
	// offset is the only thing that says which instruction went wrong.
	w = blockplan.NewCompareWriter(want)
	bad := append([]byte(nil), want...)
	bad[20] ^= 0x20
	_, err := w.Write(bad)
	c.Check(err, ErrorMatches, `.*offset 20`)

	w = blockplan.NewCompareWriter(want)
	_, err = w.Write(append(append([]byte(nil), want...), '!'))
	c.Check(err, ErrorMatches, `the reconstruction is longer than the 42-byte target`)

	// Nothing fails here: a truncated reconstruction is only detectable once
	// the writing stops, which is why the gate checks At afterwards rather than
	// relying on Write alone.
	w = blockplan.NewCompareWriter(want)
	_, err = w.Write(want[:10])
	c.Assert(err, IsNil)
	c.Check(w.At(), Not(Equals), len(want),
		Commentf("a 10-byte prefix was counted as the whole target"))
}
