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
