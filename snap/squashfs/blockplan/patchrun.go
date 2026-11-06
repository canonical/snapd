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

package blockplan

import (
	"bytes"
	"context"
	"fmt"
	"sort"
)

// A patch run is how the format pays for the blocks OP_COPY cannot carry.
//
// The trade is explicit. OP_LITERAL ships a changed block's final on-disk bytes,
// costing delta size but zero device CPU. OP_PATCHRUN ships a patch against
// source plaintext instead, which is far smaller, but the device must decompress
// a source window and recompress the run. So a run is only worth emitting when
// the bytes it saves justify the compression it adds -- runWorthCompressing
// below is where that judgement lives, and it is a knob rather than a rule.
//
// Everything structural stays here on the build machine: the generator picks the
// windows, proves the patch reconstructs the plaintext, compresses every block
// and byte-compares it against the real target. A run reaches the delta only
// once this machine has watched it work.

// defaultMaxRunUSize sets what an apply demands in RAM, because a run's
// plaintext and its source window are held in scratch at once. With the default
// WindowRatio of 1.5 the scratch high-water mark is maxRun + 1.5*maxRun + the
// patch, which was measured at 20.4-21.6 MiB across the snapcraft and kernel
// revision pairs, on top of 4.3-13.2 MiB resident -- so roughly 33 MiB of total
// demand in the worst case, all of it bounded by this constant rather than by
// the largest file in the image.
//
// That is deliberately above the ~20 MiB first estimated for this work, which
// assumed a window ratio of 1.0; the ratio was later raised to 1.5 because it
// cuts the delta from 16.56 MiB to 5.13 MiB on the pair with the most churn.
// A caller that must hold a tighter ceiling lowers it through
// GenerateOpts.MaxRunUSize and the applier's negotiation enforces it -- at the
// cost of a larger delta.
//
// The other term in that demand is the job count, which this constant does not
// bound: each job the applier runs holds its own compressor state and its own
// block buffers, measured at 17.1 MiB resident for one job against 38.2 MiB for
// eight on an lzo image. A device with a ceiling sets both.
const defaultMaxRunUSize = 8 << 20

// PatchRunTuning is the cost model's parameters.
//
// It is exported so that a caller measuring the format -- the sweeps this
// package's defaults came out of, and `snap delta --generate` -- can start from
// DefaultPatchRunTuning and move one knob. Starting from the defaults rather
// than from a zero value is the point: every field here has a measured setting,
// and a caller that means to change one must not silently flatten the rest.
type PatchRunTuning struct {
	// MaxRunUSize caps the plaintext one run reconstructs, bounding the
	// applier's peak memory.
	MaxRunUSize int
	// WindowRatio sizes a source window relative to the run's plaintext. The
	// window has to be big enough to contain the run's unchanged
	// neighbourhood, since that is what the patch matches against.
	WindowRatio float64
	// MinSaving is the fewest bytes a run must save over the equivalent
	// literals to be worth any device compression at all. It is about process
	// overhead rather than the trade -- a run costs the device one hpatchz
	// spawn, and on an xz image one xz per source window it rebuilds plus one
	// more to recompress the run, where lzo and zstd do both in process -- and
	// the measurements below say that overhead does not materialise, so it
	// defaults to no floor and is kept as a dial for a caller who has a device
	// that says otherwise.
	MinSaving int
	// MinSavingRate is the trade itself: the delta bytes a run must save per
	// byte of plaintext it makes the device compress. A relative
	// patch-to-literal ratio cannot express this -- a run whose patch is 85%
	// of its literals still passes any such ratio while saving 150 KiB for
	// 3 MiB of compression -- so the rate is the primary filter and
	// MaxCostRatio is only a backstop.
	MinSavingRate float64
	// WindowBackFrac is how much of a window sits before its anchor rather
	// than after it, as a fraction of the window. 0 is the forward-only
	// placement; 0.5 centres the window on the anchor. It exists because the
	// anchor is a guess, and it is measured at 0 -- see the default below.
	WindowBackFrac float64
	// MaxCostRatio is the largest patch-to-literal size ratio still worth
	// compressing for: 0.9 means a run must come in at least 10% under the
	// literals it replaces. A run that fails this is not saving anything
	// worth having regardless of its size.
	MaxCostRatio float64
	// Disabled ships every run as literals. That is the largest delta the
	// format can produce, and the only one that asks the device for no
	// data-block compression whatsoever. It is the baseline the patch runs are
	// measured against, and where a generate lands when no patch tool is
	// installed.
	Disabled bool
}

// DefaultPatchRunTuning is what the sweeps over the snapcraft revision pairs
// settled on. The knobs came out with quite different characters, and it is
// worth being straight about which is measured and which is merely safe.
func DefaultPatchRunTuning(maxRunUSize int) PatchRunTuning {
	return PatchRunTuning{
		MaxRunUSize: maxRunUSize,
		// Measured. On 8.14.4.post129 -> post194 the ratio trades delta size
		// against window decompression, and it has a knee: 0.5 gives a 16.56 MiB
		// delta for 27.92 MiB decompressed, 1.0 gives 5.74/63.38, 1.5 gives
		// 5.13/98.96, 2.0 gives 4.97/124.81, and 3.0 is worse on *both* counts
		// at 5.00/144.17 -- past the knee a wider window only gives hdiffz more
		// places to find a mediocre match. 1.5 is within 3% of the best delta
		// the ratio can reach while decompressing a fifth less than 2.0 does.
		WindowRatio: 1.5,
		// Measured, and the answer is that the window should not reach
		// backwards at all. The hypothesis was that a window only reaching
		// forward misses content that moved later within a large file, and the
		// sweep refutes it on all five pairs: 0, 0.1 and 0.25 land within 0.1%
		// of each other -- on imx-kernel 3,486,147 / 3,483,920 / 3,483,009, on
		// post75 -> post77 544,423 / 545,551 / 546,517 -- and 0.4 and 0.5 are
		// then ruinous, taking imx-kernel to 7,238,187 and 13,950,905 bytes and
		// post61 -> post60 from 100,705 bytes to 1,390,666 and 2,743,381.
		//
		// The reason is that the two directions are not symmetric. Budget spent
		// behind the anchor is budget not spent ahead of it, and a file's
		// plaintext runs forward from the offset the anchor names, so the match
		// is nearly always ahead. Backing off trades a certain loss for a
		// speculative gain. The dial stays because it is cheap and it records
		// the negative result; it should be left at 0.
		WindowBackFrac: 0,
		// Measured, and it earns nothing. The floor was 16 KiB on the argument
		// that a small run's three forks cost more than compressing its few
		// blocks, with a gadget revision -- whose entire change is one 10.6 KiB
		// run -- as the case in point. Timing the apply instead of assuming,
		// that pair rebuilds in 0.04 seconds whether the run is patched or
		// shipped whole, while the floor costs it a factor of ten in delta size
		// (1,083 bytes against 11,571).
		//
		// The larger pairs agree. Dropping the floor to 0 takes post75 ->
		// post77 from 1,729,885 bytes to 544,423, post61 -> post60 from
		// 1,147,575 to 100,705 and post77 -> post129 from 5,382,710 to
		// 3,657,582, for 1.9, 1.6 and 3.3 more seconds of apply CPU -- and even
		// that is mostly the 7% more plaintext the extra runs compress, not the
		// forks. It is what makes the format's delta smaller than
		// snap-1-1-xdelta3's on every pair rather than several times larger,
		// while still applying four times cheaper.
		//
		// Nothing trivial gets through, because MinSavingRate is the real floor
		// and unlike this one it scales with the work asked: a run of a single
		// 128 KiB block has to save 2.6 KiB to pass it.
		MinSaving: 0,
		// A floor rather than a tuner, and the sweeps say where the floor
		// belongs. Read the rate's effect as an exchange: bytes of device
		// compression avoided per byte the delta grows. On 8.13.2.post77 ->
		// 8.14.4.post129, the pair with the most churn, that exchange decays
		// steeply -- raising the rate from this default to 0.05 avoids 22 bytes
		// of compression per delta byte it costs, 0.10 avoids 12, and 0.20 only
		// 7.5, by which point it is refusing runs worth having: delta 7,870,356
		// -> 10,753,615 bytes to bring compression 81.99 -> 62.79 MiB. At 0.40
		// the exchange is 3.8 and the delta 29,179,327 bytes. On the quieter
		// pairs the low rates are inert -- imx-kernel produces the identical
		// delta at 0.02, 0.05 and 0.10, and post75 -> post77 at 0.02 and 0.05,
		// because their runs are all comfortably worth compressing.
		//
		// So 0.02 sits where the check only ever removes outliers. It is not the
		// rate that minimises delta size (that is 0) nor the one that minimises
		// device CPU (raise it until runs stop); it is the largest rate that was
		// still purely favourable on every pair measured. A caller who wants to
		// trade delta size for CPU in earnest raises it knowing the exchange
		// rate above.
		MinSavingRate: 0.02,
		MaxCostRatio:  0.9,
	}
}

// srcWindowPicker turns a byte range of the source data region into the windows
// of whole source blocks the applier decompresses to feed a patch run.
type srcWindowPicker struct {
	src *squashfsImage
	// ext is every source block, sorted by offset and tiling the data region
	// exactly, which CheckCoverage has already proved.
	ext []extent
	// winSizes records each window's block lengths, for a compressor that needs
	// them to decode one. See srcWindow.CSizes.
	winSizes bool
}

func newSrcWindowPicker(src *squashfsImage, ext []extent) *srcWindowPicker {
	return &srcWindowPicker{src: src, ext: ext, winSizes: src.Dec.NeedsBlockSizes()}
}

// windowsAround returns windows holding at most maxU bytes of source plaintext
// between them, positioned so that back bytes of it, at most, precede anchor.
//
// The anchor says where the run's plaintext is expected to live, but "expected"
// is doing real work: within a single large file whose contents shifted -- a FIT
// image whose compressed kernel changed size, a tarball, an ext4 blob -- the
// corresponding source bytes can sit either side of the offset that names them.
// A window that only reaches forward finds the match when the content moved
// earlier and misses it entirely when it moved later, and a miss costs the full
// price: the run's patch degenerates to roughly its own plaintext.
//
// Nothing about correctness depends on where the window sits. It is source
// plaintext handed to hdiffz, which reports what it could not match; a badly
// placed window makes a large patch, not a wrong one.
func (p *srcWindowPicker) windowsAround(anchor int64, maxU, back int) ([]srcWindow, bool) {
	w, ok := p.windowsFrom(p.backOff(anchor, back), maxU)
	if ok {
		return w, true
	}
	// Backing off cannot itself lose a window -- rounding forward from a lower
	// offset can only find more blocks -- but the anchor may have sat past the
	// last source block, and then neither offset yields one.
	return p.windowsFrom(anchor, maxU)
}

// backOff returns the offset of the earliest source block that leaves at most
// back bytes of plaintext between it and off.
//
// Walking blocks rather than subtracting is the whole point: the window's budget
// is denominated in plaintext, while an anchor is an on-disk offset, and the two
// differ by whatever the compressor achieved -- around 2:1 on a snap, but not
// uniformly, so no single ratio converts one to the other.
func (p *srcWindowPicker) backOff(off int64, back int) int64 {
	if back <= 0 {
		return off
	}
	i := sort.Search(len(p.ext), func(k int) bool { return p.ext[k].Offset >= off })
	acc := 0
	for i > 0 {
		e := p.ext[i-1]
		if acc+e.USize > back {
			break
		}
		acc += e.USize
		i--
	}
	if i == len(p.ext) {
		return off
	}
	return p.ext[i].Offset
}

// windowsFrom returns windows covering at most maxU bytes of source plaintext,
// starting at the first whole block at or after lo.
//
// Two constraints come from the applier's side of the contract. A window must
// consist of whole blocks, because it is decompressed as a unit; and every block
// in one must agree on being compressed or being raw, because a single `xz -dc`
// cannot walk past a block that is not an xz stream. That second constraint is
// why this returns a list: a window ends where the source stops being uniformly
// one or the other, and the next one picks up immediately after. The applier
// decompresses them in order into a single buffer, so a split costs a handful of
// instruction bytes and nothing else.
//
// Ending the whole window at the first such boundary instead -- which is what
// this did until the kernel snap was measured -- looks harmless when raw blocks
// are a scattered 7% of an image. It is not harmless when they come in runs:
// imx-kernel's kernel.img is a FIT image holding already-compressed payloads, so
// mksquashfs stores long stretches of it raw, and three runs there were handed
// windows of 917 KiB, 1.5 MiB and 655 KiB for 8 MiB of plaintext each. Those
// three alone cost 18.5 MiB of the pair's 20 MiB of patch.
func (p *srcWindowPicker) windowsFrom(lo int64, maxU int) ([]srcWindow, bool) {
	if maxU <= 0 || len(p.ext) == 0 {
		return nil, false
	}
	// The first block at or after lo. Landing mid-block rounds forward: a
	// partial block cannot be decompressed.
	i := sort.Search(len(p.ext), func(k int) bool { return p.ext[k].Offset >= lo })
	if i == len(p.ext) {
		return nil, false
	}
	var out []srcWindow
	// cur is the window being built; -1 means there is none yet. uTotal is the
	// plaintext across all of them, since maxU bounds what the applier holds at
	// once and that is the sum, not any one window.
	cur := -1
	uTotal := 0
	prevRaw := false
	for ; i < len(p.ext); i++ {
		e := p.ext[i]
		if uTotal != 0 && uTotal+e.USize > maxU {
			break
		}
		// A change of kind starts a new window. Extents tile the region, so a
		// discontinuity cannot happen on a verified image, but it would mean the
		// same thing, and the check costs nothing.
		if cur >= 0 && (e.Raw != prevRaw || out[cur].Off+int64(out[cur].Len) != e.Offset) {
			cur = -1
		}
		if cur < 0 {
			out = append(out, srcWindow{Off: e.Offset})
			cur = len(out) - 1
		}
		out[cur].Len += e.CSize
		out[cur].ULen += e.USize
		// Only a compressed window's sizes are recorded, and only for a
		// compressor that cannot find its own block boundaries. A raw window is
		// plaintext already and is never decoded. See srcWindow.CSizes.
		if p.winSizes && !e.Raw {
			out[cur].CSizes = append(out[cur].CSizes, e.CSize)
		}
		uTotal += e.USize
		prevRaw = e.Raw
	}
	if len(out) == 0 {
		return nil, false
	}
	// A window of raw blocks satisfies ULen == Len -- the applier's "already
	// plaintext" invariant -- for free: the extent walk refuses an image whose
	// raw block occupies a different number of bytes than it holds, so every
	// extent here already has CSize == USize when Raw.
	return out, true
}

// window returns the first of the windows at lo, which is what a caller that
// only wants to know whether a window exists there is asking for.
func (p *srcWindowPicker) window(lo int64, maxU int) (srcWindow, bool) {
	w, ok := p.windowsFrom(lo, maxU)
	if !ok {
		return srcWindow{}, false
	}
	return w[0], true
}

// extentsIn returns the source blocks a window spans, which is what
// DecompressExtents needs to decode it the same way the applier will.
func (p *srcWindowPicker) extentsIn(w srcWindow) []extent {
	i := sort.Search(len(p.ext), func(k int) bool { return p.ext[k].Offset >= w.Off })
	end := w.Off + int64(w.Len)
	j := i
	for ; j < len(p.ext) && p.ext[j].Offset < end; j++ {
	}
	return p.ext[i:j]
}

// candidateRun is a span of consecutive unmatched target blocks, before the cost
// model has decided how to ship it.
type candidateRun struct {
	// ext is the target's blocks, in ascending offset.
	ext []extent
	// srcAnchor is where in the source the run's plaintext is expected to
	// live, and so where its window starts. It comes from the path matcher
	// when the run's first block belongs to a file the source also has: the
	// same plaintext offset of the same file is the closest thing to a
	// correspondence two revisions have.
	srcAnchor int64
	// srcFallback is the offset-proximity guess -- the source offset just
	// past the block the preceding OP_COPY matched. Consecutive revisions
	// keep near-monotonic layouts, so the bytes bracketed by two matches are
	// usually the source's version of what changed between them. It stands in
	// when no path correspondence was found, and it is retried when the
	// anchor yields no window at all, which costs nothing: picking a window
	// is index arithmetic, and only the chosen one is ever decompressed.
	srcFallback int64
	// anchoredBy records which of the two the anchor came from, for the
	// generator's report. It says nothing about the delta's correctness.
	anchoredBy anchorKind
}

// USizeTotal is the plaintext the run reconstructs.
func (r *candidateRun) USizeTotal() int {
	n := 0
	for _, e := range r.ext {
		n += e.USize
	}
	return n
}

// CSizeTotal is what the run would cost as literals.
func (r *candidateRun) CSizeTotal() int {
	n := 0
	for _, e := range r.ext {
		n += e.CSize
	}
	return n
}

// builtRun is a patch run the generator has proved out: the patch reconstructs
// the plaintext, and every block recompresses to the target's exact bytes.
type builtRun struct {
	blocks  []planBlock
	windows []srcWindow
	patch   []byte
}

// RunLogHeader explains the columns of the per-run log. It is printed once,
// before the first run, by whoever set GenerateOpts.RunLog.
//
// It sits here rather than beside that caller because the line it describes is
// written a few lines below: a column added to one and not the other is the
// kind of drift that makes a log worse than none, and keeping both in view is
// what stops it.
const RunLogHeader = `run log columns, one line per patch run considered:
  tgt=OFF          the run's first block, as a byte offset into the target's data region
  blocks=N         how many consecutive target blocks the run covers
  u=BYTES          the run's plaintext, which is what the device would recompress
  win=OFF,U,xN     the source given to the diff: first byte offset, total plaintext, number of windows
  patch=BYTES      what hdiffz produced, which is what the delta would carry
  lit=BYTES        what the same blocks cost shipped verbatim, which patch has to beat
  anchor=OFF,HOW   where in the source the run was expected to live, and how that was decided:
                   path (same file in both revisions), fuzzy (a version bump in the name),
                   cursor (no correspondence found, so the previous match's position stands)
a run whose patch does not beat lit by enough is not logged as rejected here; the
generator's summary counts those.`

// buildPatchRun tries to turn a candidate into a patch run. It returns nil, nil
// when the run should ship as literals instead -- because no window was
// available, because a block did not recompress to the target's bytes, or
// because the cost model judged the saving too small to be worth the device's
// compression. None of those are errors: OP_LITERAL is always a correct answer.
func buildPatchRun(ctx context.Context, tgt *squashfsImage, run *candidateRun, pick *srcWindowPicker,
	comp Compressor, tune PatchRunTuning, opts *GenerateOpts, stats *Stats) (*builtRun, error) {

	if tune.Disabled {
		return nil, nil
	}
	uTotal := run.USizeTotal()
	maxWinU := int(float64(uTotal) * tune.WindowRatio)
	if maxWinU > 2*tune.MaxRunUSize {
		maxWinU = 2 * tune.MaxRunUSize
	}
	// The cost model's necessary condition, evaluated before any work: the best
	// conceivable patch is a zero-byte one, so a run that would fail the model
	// even then cannot pass it. Asking runWorthCompressing itself keeps the two
	// in step -- there is no second inequality here to drift out of agreement --
	// and it saves the two decompressions and the hdiffz that would otherwise be
	// run only to be thrown away. Small runs are the common case: a gadget snap
	// revision is nothing but small runs.
	literal := run.CSizeTotal()
	if !runWorthCompressing(0, literal, uTotal, tune) {
		stats.RunsTooExpensive++
		stats.RunsRejectedBytes += int64(literal)
		return nil, nil
	}

	// The window's budget beyond the run's own plaintext is what pays for the
	// anchor being approximate, and WindowBackFrac decides how much of that
	// slack looks backwards.
	back := int(float64(maxWinU) * tune.WindowBackFrac)
	wins, ok := pick.windowsAround(run.srcAnchor, maxWinU, back)
	kind := run.anchoredBy
	if !ok && run.srcAnchor != run.srcFallback {
		// The anchor sat past the last source block, which is what a file that
		// lives near the end of the source looks like when the target grew.
		wins, ok = pick.windowsAround(run.srcFallback, maxWinU, back)
		kind = anchorNone
	}
	if !ok {
		stats.RunsNoWindow++
		return nil, nil
	}
	switch kind {
	case anchorPath:
		stats.RunsPathAnchored++
	case anchorFuzzy:
		stats.RunsFuzzyAnchored++
	default:
		stats.RunsCursorAnchored++
	}

	// The windows are consecutive, so their extents concatenate, and
	// DecompressExtents splices raw blocks into place exactly as the applier's
	// gatherWindowsTo does window by window. One call therefore produces the
	// same bytes the device will, which is what makes the patch valid.
	var srcExt []extent
	winU := 0
	for _, w := range wins {
		srcExt = append(srcExt, pick.extentsIn(w)...)
		winU += w.ULen
	}
	old, err := pick.src.DecompressExtents(ctx, srcExt)
	if err != nil {
		return nil, fmt.Errorf("decompressing the source window at %d: %w", wins[0].Off, err)
	}
	if len(old) != winU {
		return nil, fmt.Errorf("source windows at %d decompressed to %d bytes, expected %d",
			wins[0].Off, len(old), winU)
	}
	plain, err := tgt.DecompressExtents(ctx, run.ext)
	if err != nil {
		return nil, fmt.Errorf("decompressing the target run at %d: %w", run.ext[0].Offset, err)
	}
	if len(plain) != uTotal {
		return nil, fmt.Errorf("target run at %d decompressed to %d bytes, expected %d",
			run.ext[0].Offset, len(plain), uTotal)
	}

	patch, err := runHdiffz(ctx, old, plain, opts.HdiffzArgs)
	if err != nil {
		return nil, fmt.Errorf("diffing a run of %d blocks: %w", len(run.ext), err)
	}
	if opts.RunLog != nil {
		fmt.Fprintf(opts.RunLog, "run tgt=%d blocks=%d u=%d win=%d,%d,x%d patch=%d lit=%d anchor=%d,%s\n",
			run.ext[0].Offset, len(run.ext), uTotal, wins[0].Off, winU, len(wins),
			len(patch), literal, run.srcAnchor, kind)
	}

	// Now the real thing: if the patch does not beat the literals by enough,
	// there is nothing to verify.
	if !runWorthCompressing(len(patch), literal, uTotal, tune) {
		stats.RunsTooExpensive++
		stats.RunsRejectedBytes += int64(literal)
		return nil, nil
	}

	// Per-block verification. The applier can only check that a block's
	// recompressed length matches; the generator has the target image in
	// hand, so it compares the bytes. Any disagreement -- a compressor that
	// does not reproduce mksquashfs's output, a patch that reconstructed
	// something subtly wrong -- downgrades the whole run to literals rather
	// than shipping a delta that fails on the device.
	blocks := make([]planBlock, len(run.ext))
	uSizes := make([]int, len(run.ext))
	for i, e := range run.ext {
		blocks[i] = planBlock{USize: e.USize, CSize: e.CSize}
		uSizes[i] = e.USize
	}
	mismatch := false
	err = comp.CompressBlocks(ctx, plainBytes(plain), uSizes, int(tgt.SB.BlockSize),
		func(idx int, blk CompressedBlock) error {
			e := run.ext[idx]
			want := tgt.Data[e.Offset : e.Offset+int64(e.CSize)]
			if blk.Raw != e.Raw || !bytes.Equal(blk.OnDisk, want) {
				mismatch = true
				stats.RunBlockMismatches++
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("recompressing a run of %d blocks: %w", len(run.ext), err)
	}
	if mismatch {
		stats.RunsVerifyFailed++
		stats.RunsRejectedBytes += int64(literal)
		return nil, nil
	}

	return &builtRun{blocks: blocks, windows: wins, patch: patch}, nil
}

// runWorthCompressing is the cost model. A patch run costs the device a source
// decompression plus a full recompression of the run's plaintext, so it has to
// buy a reduction in delta size proportionate to the work it adds -- uTotal is
// that work, and it is why the decision cannot be made on delta bytes alone.
func runWorthCompressing(patchLen, literalLen, uTotal int, tune PatchRunTuning) bool {
	saving := literalLen - patchLen
	if saving < tune.MinSaving {
		return false
	}
	if uTotal > 0 && float64(saving)/float64(uTotal) < tune.MinSavingRate {
		return false
	}
	return float64(patchLen) <= float64(literalLen)*tune.MaxCostRatio
}
