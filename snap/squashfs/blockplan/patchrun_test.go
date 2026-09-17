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
	"crypto/sha256"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type patchrunSuite struct{}

var _ = Suite(&patchrunSuite{})

// TestSrcWindowPicker holds the two rules the applier depends on: a window is
// whole blocks, and every block in it agrees on being compressed or raw. Both
// exist because the window is decompressed as a unit by a single `xz -dc`, which
// cannot walk past a block that is not an xz stream.
func (s *patchrunSuite) TestSrcWindowPicker(c *C) {
	requireTools(c, "mksquashfs", "xz")
	ctx := context.Background()
	// populateMixed has both compressed blocks and a raw one, so both kinds of
	// window occur.
	im, err := blockplan.OpenImage(buildImage(c, "windows.snap", populateMixed))
	c.Assert(err, IsNil)
	ext, gaps, overlaps, err := im.CheckCoverage(ctx)
	c.Assert(err, IsNil)
	c.Assert(gaps, HasLen, 0)
	c.Assert(overlaps, HasLen, 0)
	pick := blockplan.NewSrcWindowPicker(im, ext)

	sawPlain, sawCompressed := false, false
	for _, from := range ext {
		cmt := Commentf("window from offset %d", from.Offset)
		w, ok := pick.Window(from.Offset, 1<<30)
		c.Assert(ok, Equals, true, Commentf("no window at offset %d, which is a block boundary", from.Offset))
		c.Check(w.Off, Equals, from.Offset, cmt)

		// Walk the window back into extents: it must be exactly the blocks
		// ExtentsIn reports, contiguous, all of one kind, and its declared
		// plaintext must be their total.
		in := pick.ExtentsIn(w)
		c.Assert(len(in) > 0, Equals, true, Commentf("window %+v spans no extents", w))
		var cTotal, uTotal int
		at := w.Off
		for _, e := range in {
			c.Check(e.Offset, Equals, at, Commentf("window %+v is not contiguous at %d", w, e.Offset))
			c.Check(e.Raw, Equals, in[0].Raw, Commentf("window %+v mixes raw and compressed blocks", w))
			cTotal += e.CSize
			uTotal += e.USize
			at += int64(e.CSize)
		}
		c.Check(cTotal, Equals, w.Len, cmt)
		c.Check(uTotal, Equals, w.ULen, cmt)
		// The invariant the applier reads the window through: already-plaintext
		// exactly when the blocks are stored raw.
		c.Check(w.Plain(), Equals, in[0].Raw,
			Commentf("window %+v reports Plain()=%v over raw=%v blocks", w, w.Plain(), in[0].Raw))
		if in[0].Raw {
			sawPlain = true
		} else {
			sawCompressed = true
		}

		// A window is at least one whole block even when that overshoots the
		// budget, because half a block cannot be decompressed.
		one, ok := pick.Window(from.Offset, 1)
		c.Assert(ok, Equals, true, Commentf("no minimal window at %d", from.Offset))
		c.Check(one.Len, Equals, from.CSize, cmt)
		c.Check(one.ULen, Equals, from.USize, cmt)
	}
	c.Check(sawPlain, Equals, true, Commentf("the fixture exercised no plain window"))
	c.Check(sawCompressed, Equals, true, Commentf("the fixture exercised no compressed window"))

	// Landing mid-block rounds forward, since a partial block is not
	// decompressible; and there is nothing to be had past the region's end.
	mid := ext[0].Offset + 1
	w, ok := pick.Window(mid, 1<<30)
	c.Assert(ok, Equals, true)
	c.Check(w.Off > mid, Equals, true,
		Commentf("a window from mid-block %d did not round forward: %+v", mid, w))

	last := ext[len(ext)-1]
	w, ok = pick.Window(last.Offset+int64(last.CSize), 1<<30)
	c.Check(ok, Equals, false, Commentf("a window was found past the end of the data region: %+v", w))

	_, ok = pick.Window(0, 0)
	c.Check(ok, Equals, false, Commentf("a window was found with no plaintext budget"))
}

// TestSrcWindowsSpanRawBoundaries covers what a window may not do: stop.
//
// A window holds blocks of one kind, because the applier decompresses it in one
// pass, so a run of raw blocks inside an otherwise compressed region ends one.
// Ending the whole budget there starves the patch -- measured on imx-kernel,
// where a FIT image's raw stretches cut three 8 MiB runs down to windows of
// 917 KiB, 1.5 MiB and 655 KiB and cost 18.5 MiB of patch. windowsFrom must
// instead continue into a fresh window and keep spending the budget.
func (s *patchrunSuite) TestSrcWindowsSpanRawBoundaries(c *C) {
	requireTools(c, "mksquashfs", "xz")
	ctx := context.Background()
	// populateMixed holds both compressed blocks and a raw one, so at least one
	// boundary exists to be crossed.
	im, err := blockplan.OpenImage(buildImage(c, "segments.snap", populateMixed))
	c.Assert(err, IsNil)
	ext, _, _, err := im.CheckCoverage(ctx)
	c.Assert(err, IsNil)
	pick := blockplan.NewSrcWindowPicker(im, ext)

	wins, ok := pick.WindowsFrom(ext[0].Offset, 1<<30)
	c.Assert(ok, Equals, true, Commentf("no windows at the start of the data region"))
	c.Assert(len(wins) >= 2, Equals, true,
		Commentf("the fixture produced %d window(s), so no raw boundary was crossed", len(wins)))

	// With a budget past the region's size the windows must together be the whole
	// region: same extents, in order, nothing dropped at a boundary.
	var got []blockplan.Extent
	at := ext[0].Offset
	for i, w := range wins {
		cmt := Commentf("window %d", i)
		c.Check(w.Off, Equals, at, Commentf("window %d starts at %d, leaving a hole after %d", i, w.Off, at))
		in := pick.ExtentsIn(w)
		c.Assert(len(in) > 0, Equals, true, cmt)
		for _, e := range in {
			c.Check(e.Raw, Equals, in[0].Raw, Commentf("window %d mixes raw and compressed blocks", i))
		}
		c.Check(w.Plain(), Equals, in[0].Raw,
			Commentf("window %d reports Plain()=%v over raw=%v blocks", i, w.Plain(), in[0].Raw))
		got = append(got, in...)
		at = w.Off + int64(w.Len)
	}
	c.Assert(got, HasLen, len(ext), Commentf("the windows span %d of the region's %d blocks", len(got), len(ext)))
	for i := range got {
		c.Assert(got[i], DeepEquals, ext[i], Commentf("window block %d", i))
	}

	// The plaintext the applier holds at once is the total across windows, so
	// that is what the budget bounds -- give or take the first block, which is
	// indivisible.
	const budget = 200000
	wins, ok = pick.WindowsFrom(ext[0].Offset, budget)
	c.Assert(ok, Equals, true, Commentf("no windows within a bounded budget"))
	u := 0
	for _, w := range wins {
		u += w.ULen
	}
	c.Check(u <= budget+ext[0].USize, Equals, true,
		Commentf("windows hold %d bytes of plaintext against a %d budget", u, budget))
	// Not a correctness bound, just a check that the budget bites at all --
	// otherwise the assertion above passes vacuously.
	c.Assert(len(ext) >= 5, Equals, true)
	whole := 0
	for _, e := range ext[:5] {
		whole += e.USize
	}
	c.Check(u < whole, Equals, true,
		Commentf("a %d-byte budget yielded %d bytes of plaintext, so it was not enforced", budget, u))
}

// TestRunWorthCompressing walks each branch of the cost model, which is the only
// place the delta-size-against-device-CPU trade is decided.
func (s *patchrunSuite) TestRunWorthCompressing(c *C) {
	tune := blockplan.DefaultPatchRunTuning(8 << 20)
	tests := []struct {
		name                   string
		patch, literal, uTotal int
		// floor overrides MinSaving, which the default leaves at 0 because the
		// apply timings said the process overhead it was guarding against does
		// not show up. The dial still has to work for the caller who wants it.
		floor int
		want  bool
	}{
		{name: "a small patch replacing large literals", patch: 2000, literal: 300000, uTotal: 600000, want: true},
		// Its rate and ratio are both fine, so only an explicit floor refuses
		// it -- and by default nothing does.
		{name: "a small saving, floor set", patch: 1000, literal: 9000, uTotal: 20000, floor: 16 << 10},
		{name: "a small saving, no floor", patch: 1000, literal: 9000, uTotal: 20000, want: true},
		// What keeps the floorless default from accepting anything at all: a
		// whole block of plaintext recompressed to save 100 bytes fails the
		// rate, which unlike a byte floor scales with the work asked.
		{name: "a trivial saving on a full block", patch: 1000, literal: 1100, uTotal: 128 << 10},
		// Above the floor but the device would compress 2 MiB to save 20 KB of
		// delta, which is the trade MinSavingRate exists to refuse.
		{name: "a good ratio at a bad rate", patch: 80000, literal: 100000, uTotal: 2000000},
		// A fine rate, but the patch is barely under the literals, so there is
		// nothing here worth having.
		{name: "too close to the literals", patch: 950000, literal: 1000000, uTotal: 1000000},
		{name: "a patch larger than the literals", patch: 200000, literal: 100000, uTotal: 400000},
	}
	for _, t := range tests {
		tc := tune
		if t.floor != 0 {
			tc.MinSaving = t.floor
		}
		c.Check(blockplan.RunWorthCompressing(t.patch, t.literal, t.uTotal, tc), Equals, t.want,
			Commentf("%s: patch=%d literal=%d uTotal=%d", t.name, t.patch, t.literal, t.uTotal))
	}

	// buildPatchRun screens runs with a zero-byte patch before it does any work,
	// which is only sound if the model is monotonic in the patch size: whatever
	// passes at some size must pass at every smaller one. Check that over a grid
	// rather than trusting the arithmetic, since the pre-check and the real check
	// are the same function and a non-monotonic model would silently drop runs
	// that deserved to be emitted.
	for literal := 1; literal <= 1<<20; literal *= 4 {
		for uTotal := 1; uTotal <= 1<<24; uTotal *= 8 {
			best := blockplan.RunWorthCompressing(0, literal, uTotal, tune)
			for patch := 1; patch <= 2*literal; patch = patch*3 + 1 {
				if best {
					continue
				}
				c.Assert(blockplan.RunWorthCompressing(patch, literal, uTotal, tune), Equals, false,
					Commentf("a %d-byte patch passes where a zero-byte one does not "+
						"(literal=%d, uTotal=%d), so the pre-check would drop it", patch, literal, uTotal))
			}
		}
	}
}

// --- end to end ---
//
// Everything above drives the picker and the cost model directly. What follows
// generates real deltas over real images, because the two properties that matter
// most about a patch run are only visible there: that it is worth having, and
// that a compressor which does not reproduce the target's bytes cannot turn one
// into a wrong image.

// TestBeatsLiterals is the whole point of OP_PATCHRUN: the same pair is encoded
// twice, once with runs disabled, and the run version has to be substantially
// smaller while still reconstructing the target exactly.
func (s *patchrunSuite) TestBeatsLiterals(c *C) {
	ctx := context.Background()
	source, target := churnPair(c)
	dir := c.MkDir()

	litPath := filepath.Join(dir, "literals.delta")
	lit, err := blockplan.Generate(ctx, source, target, litPath, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true, NoPatchRuns: true,
	})
	c.Assert(err, IsNil, Commentf("generating a literals-only delta"))
	c.Check(lit.PatchRuns, Equals, 0, Commentf("NoPatchRuns still emitted patch runs"))
	c.Check(lit.PatchedUBytes, Equals, int64(0),
		Commentf("a literals-only delta asks the device to compress %d bytes of data plaintext",
			lit.PatchedUBytes))

	runPath := filepath.Join(dir, "runs.delta")
	run, err := blockplan.Generate(ctx, source, target, runPath, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true,
	})
	c.Assert(err, IsNil, Commentf("generating a delta with patch runs"))
	c.Assert(run.PatchRuns > 0, Equals, true,
		Commentf("no patch run was emitted for a wholly shifted file; runs went to literals instead "+
			"(%d no window, %d not worth it, %d failed verify)",
			run.RunsNoWindow, run.RunsTooExpensive, run.RunsVerifyFailed))
	// The insertion shifts every block of big.bin, so the source holds the
	// plaintext but none of the compressed bytes. A patch against it should be a
	// rounding error next to shipping those blocks whole.
	c.Check(run.DeltaSize <= lit.DeltaSize/2, Equals, true,
		Commentf("patch runs saved too little: %d bytes against %d for literals",
			run.DeltaSize, lit.DeltaSize))
	c.Check(run.PatchBytes < int64(4*4000), Equals, true,
		Commentf("a patch for a 4000-byte insertion came to %d bytes", run.PatchBytes))

	// What the device pays for that: it compresses the run's plaintext and
	// decompresses a window to feed it, and both must be far below the whole
	// image -- otherwise this is just snap-1-1-xdelta3 again.
	st := applyAndCompare(c, source, runPath, target, newXZ(c))
	c.Check(st.PatchRuns, Equals, run.PatchRuns,
		Commentf("applied %d patch runs, the delta declares %d", st.PatchRuns, run.PatchRuns))
	c.Check(st.WindowUBytes > 0, Equals, true,
		Commentf("a patch run ran without reading any source plaintext"))
	c.Check(st.UCompressedBytes > st.MetaUBytes, Equals, true,
		Commentf("a patch run compressed no data plaintext at all"))
	// steady.bin did not change, so the compressor must never have seen it. This
	// is the CPU saving itself, and the assertion snap-1-1-xdelta3 cannot make at
	// any delta size.
	c.Check(st.CopiedBytes > 0, Equals, true,
		Commentf("nothing was copied verbatim, so the unchanged half of the image was rebuilt"))
	c.Check(st.UCompressedBytes < run.TargetUBytes, Equals, true,
		Commentf("the apply compressed %d bytes of plaintext out of a %d-byte target -- no CPU was saved",
			st.UCompressedBytes, run.TargetUBytes))
}

// TestSplitsAtRunCap holds the applier's memory bound: a changed region larger
// than the cap has to become several runs, not one oversized one.
func (s *patchrunSuite) TestSplitsAtRunCap(c *C) {
	ctx := context.Background()
	source, target := churnPair(c)
	delta := filepath.Join(c.MkDir(), "capped.delta")

	// Two blocks' worth. big.bin spans five, so the run cannot be emitted whole.
	const cap2 = 2 * testBlockSize
	stats, err := blockplan.Generate(ctx, source, target, delta, &blockplan.GenerateOpts{
		Comp: newXZ(c), Verify: true, MaxRunUSize: cap2,
	})
	c.Assert(err, IsNil, Commentf("generating under a %d-byte run cap", cap2))
	c.Check(stats.PatchRuns >= 2, Equals, true,
		Commentf("a five-block change under a two-block cap produced %d patch runs", stats.PatchRuns))

	// The header's cap is what the applier enforces, so it has to travel.
	df, err := os.Open(delta)
	c.Assert(err, IsNil)
	defer df.Close()
	pr, err := blockplan.OpenPlan(df)
	c.Assert(err, IsNil)
	c.Check(pr.Header.MaxRunUSize, Equals, uint32(cap2))

	applyAndCompare(c, source, delta, target, newXZ(c))
}

// sabotagingComp is a compressor that produces wrong bytes for data blocks and
// correct ones for metadata. It stands in for the real hazard behind the
// generator's per-block verification: a compressor that does not reproduce what
// mksquashfs produced, whether that is a different liblzma, a different preset,
// or a squashfs-tools that has moved on.
//
// Only data blocks are perturbed, so the metadata patch still applies and the
// only thing under test is the data-block path.
type sabotagingComp struct {
	// Everything but CompressBlocks is the real compressor's: the method
	// declared below shadows the promoted one.
	blockplan.Compressor
	metaDict int
	// blocks counts the data blocks perturbed, so a test can tell the sabotage
	// happened rather than the run being declined for some other reason.
	blocks int
}

func (s *sabotagingComp) CompressBlocks(ctx context.Context, plain blockplan.BlockPlain, uSizes []int,
	dictSize int, fn func(idx int, blk blockplan.CompressedBlock) error) error {

	return s.Compressor.CompressBlocks(ctx, plain, uSizes, dictSize,
		func(idx int, blk blockplan.CompressedBlock) error {
			// A raw block's on-disk bytes are its plaintext, so there is nothing
			// in the compressor's output to corrupt.
			if dictSize != s.metaDict && !blk.Raw && len(blk.OnDisk) > 8 {
				// Flip a bit deep inside the LZMA2 payload rather than
				// truncating, so the block stays a well-formed stream of exactly
				// the right length. The applier's own check is on the length, so
				// only the generator's byte comparison can catch this -- which is
				// the point.
				bad := append([]byte(nil), blk.OnDisk...)
				bad[len(bad)/2] ^= 0x01
				blk.OnDisk = bad
				s.blocks++
			}
			return fn(idx, blk)
		})
}

// TestDowngradesOnVerifyFailure is the safety property that lets the cost model
// be a knob rather than a correctness risk. Given a compressor whose data blocks
// do not match the target's, every candidate run must fall back to OP_LITERAL and
// the delta must still reconstruct the target exactly.
//
// Both sides use the sabotaging compressor, because it stands for the machine's
// xz rather than a fault in one process: the generator discovers it cannot
// reproduce a data block, ships literals, and the applier -- now never asked to
// compress a data block -- produces the right image regardless.
func (s *patchrunSuite) TestDowngradesOnVerifyFailure(c *C) {
	ctx := context.Background()
	source, target := churnPair(c)
	delta := filepath.Join(c.MkDir(), "sabotaged.delta")

	im, err := blockplan.OpenImage(target)
	c.Assert(err, IsNil)
	c.Assert(int(im.SB.BlockSize) != blockplan.SquashfsMetadataSize, Equals, true,
		Commentf("the fixture's block size equals the metadata dictionary, so the saboteur cannot tell them apart"))
	comp := &sabotagingComp{Compressor: newXZ(c), metaDict: blockplan.SquashfsMetadataSize}

	// Verify is what makes this a proof rather than a hope: had any run survived
	// with a corrupt block, the gate would have caught the bad image here and
	// generation would fail.
	stats, err := blockplan.Generate(ctx, source, target, delta, &blockplan.GenerateOpts{
		Comp: comp, Verify: true,
	})
	c.Assert(err, IsNil, Commentf("generation did not survive a compressor it cannot trust"))
	c.Assert(comp.blocks > 0, Equals, true,
		Commentf("no data block was perturbed, so nothing was under test"))
	c.Check(stats.RunBlockMismatches > 0, Equals, true,
		Commentf("the corruption went unnoticed: %d block mismatches over %d failed runs",
			stats.RunBlockMismatches, stats.RunsVerifyFailed))
	c.Check(stats.RunsVerifyFailed > 0, Equals, true,
		Commentf("%d block mismatches did not fail a single run", stats.RunBlockMismatches))
	c.Check(stats.PatchRuns, Equals, 0,
		Commentf("%d patch runs were emitted from blocks that do not recompress", stats.PatchRuns))
	c.Check(stats.PatchedUBytes, Equals, int64(0),
		Commentf("a fully downgraded delta still asks the device to compress %d bytes", stats.PatchedUBytes))
	c.Check(stats.RunsRejectedBytes > 0, Equals, true,
		Commentf("the downgraded runs are not accounted for in RunsRejectedBytes"))

	st := applyAndCompare(c, source, delta, target, comp)
	c.Check(st.PatchRuns, Equals, 0,
		Commentf("the apply ran %d patch runs over %d bytes, expected none", st.PatchRuns, st.PatchedBytes))
	c.Check(st.PatchedBytes, Equals, int64(0))
	// The only plaintext the compressor saw was metadata, which the saboteur
	// leaves alone -- so a delta this conservative asks for no data-block
	// compression whatsoever, which is what makes it safe.
	c.Check(st.UCompressedBytes, Equals, st.MetaUBytes,
		Commentf("compressed %d bytes of plaintext but only %d were metadata",
			st.UCompressedBytes, st.MetaUBytes))
}

// flakyComp gives the right answer the first time it sees a block's plaintext and
// a wrong one every time after. That is what it takes to slip past the
// generator's per-block verification: the run is checked once, passes, and is
// emitted -- and then the applier compresses the same plaintext again and gets
// different bytes. Nothing but the final gate catches it.
//
// The bit flip keeps the block's length intact, so the applier's own cSize check
// cannot see it either. Metadata is left alone, as in sabotagingComp.
type flakyComp struct {
	// As in sabotagingComp: only CompressBlocks is this type's own.
	blockplan.Compressor
	metaDict int
	seen     map[[32]byte]bool
	spoiled  int
}

func (f *flakyComp) CompressBlocks(ctx context.Context, plain blockplan.BlockPlain, uSizes []int,
	dictSize int, fn func(idx int, blk blockplan.CompressedBlock) error) error {

	if f.seen == nil {
		f.seen = map[[32]byte]bool{}
	}
	at := 0
	offs := make([]int, len(uSizes))
	for i, u := range uSizes {
		offs[i] = at
		at += u
	}
	return f.Compressor.CompressBlocks(ctx, plain, uSizes, dictSize,
		func(idx int, blk blockplan.CompressedBlock) error {
			if dictSize != f.metaDict && !blk.Raw && len(blk.OnDisk) > 8 {
				// Reading the block's plaintext back may reuse the buffer the
				// inner compressor read it into, which is safe here only because
				// this branch excludes raw blocks -- for a compressed one OnDisk
				// is the freshly framed stream, not a view of that buffer.
				src, err := plain.Block(offs[idx], uSizes[idx])
				if err != nil {
					return err
				}
				key := sha256.Sum256(src)
				if f.seen[key] {
					bad := append([]byte(nil), blk.OnDisk...)
					bad[len(bad)/2] ^= 0x01
					blk.OnDisk = bad
					f.spoiled++
				}
				f.seen[key] = true
			}
			return fn(idx, blk)
		})
}

// TestGateRefusesUnverifiableDelta is the last line of defence. A run that passes
// per-block verification and then rebuilds wrongly must be caught by the final
// gate, and the delta must not be left behind on disk -- otherwise a caller that
// drops the error would publish an image that does not apply.
func (s *patchrunSuite) TestGateRefusesUnverifiableDelta(c *C) {
	ctx := context.Background()
	source, target := churnPair(c)
	delta := filepath.Join(c.MkDir(), "unverifiable.delta")

	comp := &flakyComp{Compressor: newXZ(c), metaDict: blockplan.SquashfsMetadataSize}
	_, err := blockplan.Generate(ctx, source, target, delta, &blockplan.GenerateOpts{
		Comp: comp, Verify: true,
	})
	c.Assert(err, NotNil, Commentf("a delta that does not reconstruct the target was accepted"))
	c.Check(comp.spoiled > 0, Equals, true,
		Commentf("no block was spoiled on its second sighting, so the gate was never tested"))
	_, err = os.Stat(delta)
	c.Check(os.IsNotExist(err), Equals, true,
		Commentf("the rejected delta is still on disk (stat: %v)", err))

	// Without the gate the same generation succeeds, which is what makes the gate
	// rather than the per-block verification the thing under test here.
	comp2 := &flakyComp{Compressor: newXZ(c), metaDict: blockplan.SquashfsMetadataSize}
	_, err = blockplan.Generate(ctx, source, target, delta, &blockplan.GenerateOpts{Comp: comp2})
	c.Assert(err, IsNil, Commentf("generating without the gate"))
	_, err = os.Stat(delta)
	c.Check(err, IsNil, Commentf("an ungated delta was not written"))
}

// TestCrossesRawStretch is the window rule end to end: one run whose plaintext
// spans a stretch the source stores raw. Every block of the file changes a
// little, as a rebuilt binary's do, so the run covers the whole file --
// compressible head, incompressible middle, compressible tail -- and a window
// that stops at the head/middle boundary has nothing to offer the rest of it.
func (s *patchrunSuite) TestCrossesRawStretch(c *C) {
	requireTools(c, "mksquashfs", "xz", "hdiffz", "hpatchz")
	ctx := context.Background()

	// The middle is what mksquashfs stores raw and what the window has to get
	// past; the head and tail are compressible, so they are stored as xz streams
	// and cannot share a window with it.
	const part = 1 << 20
	body := append(append(append([]byte{},
		compressibleText(part, "head")...),
		incompressible(part, 9)...),
		semiCompressible(part, 3)...)
	// Flip one byte every 4 KiB, which leaves every block of the target different
	// from the source's and so puts the whole file in one run, while leaving it
	// almost entirely matchable.
	edited := append([]byte{}, body...)
	for i := 0; i < len(edited); i += 4 << 10 {
		edited[i] ^= 0x40
	}
	source := buildImage(c, "raw-source.snap", func(c *C, dir string) {
		writeFile(c, dir, "data/blob.bin", body)
	})
	target := buildImage(c, "raw-target.snap", func(c *C, dir string) {
		writeFile(c, dir, "data/blob.bin", edited)
	})

	stats, err := blockplan.Generate(ctx, source, target, filepath.Join(c.MkDir(), "raw.delta"),
		&blockplan.GenerateOpts{Comp: newXZ(c), Verify: true})
	c.Assert(err, IsNil)
	c.Assert(stats.PatchRuns > 0, Equals, true,
		Commentf("no run was patched (%d no window, %d not worth it, %d failed verify)",
			stats.RunsNoWindow, stats.RunsTooExpensive, stats.RunsVerifyFailed))
	c.Check(stats.LiteralBytes, Equals, int64(0),
		Commentf("%d bytes shipped as literals, so a run did not get the window it needed",
			stats.LiteralBytes))
	// The edit is 1 byte in 4096 against a source that holds all of it, so the
	// patch is a list of small changes. A window truncated at the raw stretch
	// instead leaves two thirds of the run diffed against nothing, and the patch
	// carries that plaintext itself -- hundreds of KiB, not tens.
	c.Check(stats.PatchBytes <= 256<<10, Equals, true,
		Commentf("patching a run across a raw stretch cost %d bytes, and the run only "+
			"reconstructs %d bytes of plaintext", stats.PatchBytes, stats.PatchedUBytes))
}
