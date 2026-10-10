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
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"time"

	"github.com/snapcore/snapd/logger"
)

// Generating a block plan is a walk over the target's data blocks in ascending
// offset, deciding for each one how the device should come to have it: copied
// out of the source verbatim, rebuilt from a patch against source plaintext, or
// shipped whole in the delta's payload.
//
// That the first of those is possible at all is the premise the format rests on.
// squashfs compresses each data block independently, and every compressor here
// reproduces its squashfs-tools wrapper's settings exactly -- so a block whose
// plaintext did not change has byte-identical compressed bytes in both images,
// and can be copied rather than rebuilt. Matching is therefore done on the
// compressed bytes: it is what an OP_COPY needs to be true, it costs no
// decompression, and it cannot be fooled by two blocks holding the same
// plaintext that were compressed differently.
//
// Everything expensive or structural happens here rather than on the device.
// The generator holds both images, so it can prove what it emits: a patch run's
// blocks are recompressed and byte-compared against the real target before the
// run is kept, and with Verify the finished delta is applied by the very code
// the device will run and compared with the target image byte for byte.

// Generate writes a snap-2-1-hdiffz delta from sourcePath to targetPath at
// deltaPath, and reports what it consists of. opts may be nil.
//
// A refusal is a normal outcome: an image this package cannot reproduce exactly
// -- an unsupported compressor, a geometry it does not recognise, a missing
// patch tool -- is refused rather than approximated, and the caller falls back
// to the format below it.
func Generate(ctx context.Context, sourcePath, targetPath, deltaPath string, opts *GenerateOpts) (*Stats, error) {
	t0 := time.Now()
	if opts == nil {
		opts = &GenerateOpts{}
	}
	maxRunUSize := opts.MaxRunUSize
	if maxRunUSize == 0 {
		maxRunUSize = defaultMaxRunUSize
	}

	src, err := openImage(sourcePath)
	if err != nil {
		return nil, err
	}
	tgt, err := openImage(targetPath)
	if err != nil {
		return nil, err
	}
	if err := src.checkSupported(); err != nil {
		return nil, fmt.Errorf("source %s: %w", sourcePath, err)
	}
	if err := tgt.checkSupported(); err != nil {
		return nil, fmt.Errorf("target %s: %w", targetPath, err)
	}
	logger.Debugf("block plan: source superblock %s", src.SB)
	logger.Debugf("block plan: target superblock %s", tgt.SB)
	// A delta only makes sense between images the same compressor produced the
	// same way; anything else means the applier cannot reproduce a block.
	if src.SB.BlockSize != tgt.SB.BlockSize {
		return nil, fmt.Errorf("source uses %d-byte blocks and target %d", src.SB.BlockSize, tgt.SB.BlockSize)
	}
	if src.SB.CompressionId != tgt.SB.CompressionId {
		return nil, fmt.Errorf("source uses compressor %s and target %s",
			compressorName(src.SB.CompressionId), compressorName(tgt.SB.CompressionId))
	}
	// The compressor comes from the image, so nothing has to be told what built
	// it. An override is honoured only if it agrees with the superblock.
	comp := opts.Comp
	if comp == nil {
		if comp, err = newCompressor(tgt.SB.CompressionId, opts.Jobs); err != nil {
			return nil, err
		}
	} else if err := checkCompressorMatches(comp, tgt.SB.CompressionId); err != nil {
		return nil, err
	}
	blockSize := int(tgt.SB.BlockSize)
	if maxRunUSize < blockSize {
		return nil, fmt.Errorf("run cap %d is below one block (%d)", maxRunUSize, blockSize)
	}

	srcExt, gaps, overlaps, err := src.CheckCoverage(ctx)
	if err != nil {
		return nil, err
	}
	if len(gaps) != 0 || len(overlaps) != 0 {
		return nil, fmt.Errorf("source data region has %d gaps and %d overlaps, so its blocks cannot be reused safely",
			len(gaps), len(overlaps))
	}
	tgtExt, gaps, overlaps, err := tgt.CheckCoverage(ctx)
	if err != nil {
		return nil, err
	}
	if len(gaps) != 0 || len(overlaps) != 0 {
		return nil, fmt.Errorf("target data region has %d gaps and %d overlaps, so it cannot be described block by block",
			len(gaps), len(overlaps))
	}

	stats := &Stats{}
	for _, e := range tgtExt {
		stats.TargetDataBytes += int64(e.CSize)
		stats.TargetUBytes += int64(e.USize)
	}

	// --- metadata ---

	srcMeta, err := src.MetaRegionAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("source metadata: %w", err)
	}
	tgtMeta, err := tgt.MetaRegionAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("target metadata: %w", err)
	}
	mdFrame, err := encodeMDFrame(tgtMeta.Blocks, tgtMeta.Blob)
	if err != nil {
		return nil, err
	}
	stats.MetaBlocks = len(tgtMeta.Blocks)
	stats.MetaUBytes = int64(len(tgtMeta.Blob))

	// --- instructions and payload ---

	pay, err := newMemFile("blockplan-pay")
	if err != nil {
		return nil, err
	}
	defer pay.Close()
	payw := newCRCWriter(pay.File())

	enc := newInstrEncoder(blockSize, comp.NeedsBlockSizes())
	index := indexExtents(src, srcExt)
	pick := newSrcWindowPicker(src, srcExt)
	// The cost model's defaults are measured, so an override starts from them
	// and moves what it names -- see GenerateOpts.Tuning. The run cap is the
	// one field the caller cannot express twice: it is negotiated with the
	// applier through the delta header, so it comes from maxRunUSize either
	// way. Disabled likewise has exactly one spelling, NoPatchRuns.
	tune := DefaultPatchRunTuning(maxRunUSize)
	if opts.Tuning != nil {
		tune = *opts.Tuning
	}
	tune.MaxRunUSize = maxRunUSize
	tune.Disabled = opts.NoPatchRuns

	// Path correspondence is an optimisation, not a requirement: it only picks
	// which source bytes a run is diffed against. An image whose directory table
	// this code cannot walk still gets a correct delta, just a larger one, so the
	// failure is recorded and generation carries on.
	var match *pathMatcher
	if !opts.NoPathMatch {
		match, err = newPathMatcher(src, tgt, srcMeta, tgtMeta)
		if err != nil {
			stats.MatchUnavailable = err.Error()
			match = nil
		}
	}
	if opts.RunLog != nil {
		fmt.Fprintln(opts.RunLog, RunLogHeader)
	}
	if err := emitDataRegion(ctx, tgt, tgtExt, index, pick, match, enc, payw, comp, tune, opts, stats); err != nil {
		return nil, err
	}
	stats.Instructions = enc.Count()

	// --- assemble ---

	var w planWriter
	w.Header = planHeader{
		PatchTool:             patchToolHdiffz,
		BlockSize:             uint32(blockSize),
		MaxRunUSize:           uint32(maxRunUSize),
		InstrCount:            uint32(enc.Count()),
		SourceSize:            uint64(src.Size()),
		TargetSize:            uint64(tgt.Size()),
		TargetBytesUsed:       tgt.SB.BytesUsed,
		TargetInodeTableStart: tgt.SB.InodeTableStart,
		SourceSHA256:          sha256.Sum256(src.Data),
		TargetSHA256:          sha256.Sum256(tgt.Data),
	}

	canary, err := buildCanary(ctx, srcMeta.Blob, comp, blockSize)
	if err != nil {
		return nil, fmt.Errorf("building the canary: %w", err)
	}
	w.addSection(secSB, tgt.Data[:superblockSize])
	w.addSection(secCanary, canary)
	w.addSection(secMDFrame, mdFrame)
	w.addSection(secMDTail, tgt.Data[tgt.SB.ExportTableStart:tgt.SB.BytesUsed])
	if !bytes.Equal(srcMeta.Blob, tgtMeta.Blob) {
		// The metadata never survives verbatim between revisions: the inode
		// table encodes every block's size and offset. It is small, so a
		// patch plus one recompression pass is cheap.
		patch, err := runHdiffz(ctx, srcMeta.Blob, tgtMeta.Blob, opts.HdiffzArgs)
		if err != nil {
			return nil, fmt.Errorf("diffing the metadata: %w", err)
		}
		stats.MDPatchBytes = len(patch)
		w.addSection(secMDPatch, patch)
	}
	stats.InstrBytes = len(enc.Bytes())
	if err := w.addSectionCompressed(ctx, secInstr, enc.Bytes(), comp); err != nil {
		return nil, err
	}
	for _, e := range w.sections {
		if e.entry.ID == secInstr {
			stats.InstrStored = int(e.entry.StoredLen)
		}
	}
	// SEC_TOOLVER is advisory bookkeeping, so its probes never fail the
	// generation; nothing to record means no section. It is added here, before
	// SEC_PAY, because the reader requires SEC_PAY to be the last entry in the
	// section table.
	if tv := captureToolVersions(ctx, comp); tv != nil {
		w.addSection(secToolVer, tv)
	}
	if payw.n > 0 {
		w.addSectionFile(secPay, pay.File(), int(payw.n), payw.crc)
	}

	out, err := os.Create(deltaPath)
	if err != nil {
		return nil, err
	}
	if err := w.writeTo(out); err != nil {
		out.Close()
		os.Remove(deltaPath)
		return nil, err
	}
	if err := out.Close(); err != nil {
		os.Remove(deltaPath)
		return nil, err
	}
	if fi, err := os.Stat(deltaPath); err == nil {
		stats.DeltaSize = fi.Size()
	}

	// The final gate: a delta ships only once this machine has proved it
	// reconstructs the target, using the same code the device will run. One that
	// fails is deleted rather than left where it was written, so a caller that
	// drops the error cannot go on to publish it.
	if opts.Verify {
		if err := verifyPlan(ctx, sourcePath, deltaPath, tgt, comp); err != nil {
			os.Remove(deltaPath)
			return nil, fmt.Errorf("the generated delta does not reconstruct the target: %w", err)
		}
	}
	stats.Elapsed = time.Since(t0)
	return stats, nil
}

// extentIndex maps the content of a source block to every offset it appears at.
type extentIndex struct {
	src    *squashfsImage
	byHash map[[32]byte][]extent
}

// indexExtents hashes every source block's compressed bytes.
func indexExtents(im *squashfsImage, ext []extent) *extentIndex {
	idx := &extentIndex{src: im, byHash: make(map[[32]byte][]extent, len(ext))}
	for _, e := range ext {
		h := sha256.Sum256(im.Data[e.Offset : e.Offset+int64(e.CSize)])
		idx.byHash[h] = append(idx.byHash[h], e)
	}
	return idx
}

// find returns a source extent whose bytes equal want, preferring one that
// continues the current source run so the copies merge.
func (idx *extentIndex) find(want []byte, srcCursor int64) (extent, bool) {
	var first extent
	found := false
	for _, e := range idx.byHash[sha256.Sum256(want)] {
		// Guard against a hash collision the cheap way: a candidate is only
		// usable if the bytes really are equal.
		if !bytes.Equal(idx.src.Data[e.Offset:e.Offset+int64(e.CSize)], want) {
			continue
		}
		if e.Offset == srcCursor {
			return e, true
		}
		if !found {
			first, found = e, true
		}
	}
	return first, found
}

// emitDataRegion walks the target's blocks in ascending offset and turns each
// into a copy from the source, a patch run, or a literal in SEC_PAY.
//
// The extents tile the data region exactly, which CheckCoverage has already
// proved, so emitting them in order reproduces the region byte for byte.
//
// Matched blocks become OP_COPY and cost the device nothing. Everything else
// accumulates into a candidate run, which is closed out by the next match, by
// the run cap, or by the end of the region -- and only then does the cost model
// decide between a patch and literals. The decision has to wait that long
// because it needs the run's whole plaintext, both to diff it and to prove the
// blocks recompress.
func emitDataRegion(ctx context.Context, tgt *squashfsImage, ext []extent, idx *extentIndex,
	pick *srcWindowPicker, match *pathMatcher, enc *instrEncoder, pay *crcWriter, comp Compressor,
	tune PatchRunTuning, opts *GenerateOpts, stats *Stats) error {

	// A copy can absorb the next block when both the target and the source
	// offsets are contiguous.
	var (
		copyOff, copyLen int64
		srcCursor        int64
		run              candidateRun
		maxBlocks        = comp.MaxBlocksPerCall()
	)
	flushCopy := func() error {
		if copyLen == 0 {
			return nil
		}
		if err := enc.Copy(copyOff, copyLen); err != nil {
			return err
		}
		stats.Copies++
		stats.CopiedBytes += copyLen
		copyLen = 0
		return nil
	}
	// litRun ships a run's blocks verbatim: their on-disk bytes are already
	// final, so one instruction covers the lot.
	litRun := func(r *candidateRun) error {
		var n int64
		for _, e := range r.ext {
			if _, err := pay.Write(tgt.Data[e.Offset : e.Offset+int64(e.CSize)]); err != nil {
				return err
			}
			n += int64(e.CSize)
		}
		if err := enc.Literal(n); err != nil {
			return err
		}
		stats.Literals++
		stats.LiteralBytes += n
		return nil
	}
	flushRun := func() error {
		if len(run.ext) == 0 {
			return nil
		}
		defer func() { run.ext = nil }()
		built, err := buildPatchRun(ctx, tgt, &run, pick, comp, tune, opts, stats)
		if err != nil {
			return err
		}
		if built == nil {
			return litRun(&run)
		}
		if _, err := pay.Write(built.patch); err != nil {
			return err
		}
		if err := enc.PatchRun(built.blocks, built.windows, len(built.patch)); err != nil {
			return err
		}
		stats.PatchRuns++
		stats.PatchBytes += int64(len(built.patch))
		stats.PatchedUBytes += int64(run.USizeTotal())
		for _, w := range built.windows {
			stats.WindowUBytes += int64(w.ULen)
		}
		return nil
	}

	for _, e := range ext {
		want := tgt.Data[e.Offset : e.Offset+int64(e.CSize)]
		if m, ok := idx.find(want, srcCursor); ok {
			if err := flushRun(); err != nil {
				return err
			}
			if copyLen != 0 && copyOff+copyLen == m.Offset {
				copyLen += int64(m.CSize)
			} else {
				if err := flushCopy(); err != nil {
					return err
				}
				copyOff, copyLen = m.Offset, int64(m.CSize)
			}
			srcCursor = m.Offset + int64(m.CSize)
			stats.ReusedUBytes += int64(e.USize)
			continue
		}
		if err := flushCopy(); err != nil {
			return err
		}
		// Close the run before it outgrows either the memory cap or what the
		// compressor will take in one call.
		if len(run.ext) != 0 &&
			(run.USizeTotal()+e.USize > tune.MaxRunUSize || len(run.ext)+1 > maxBlocks) {
			if err := flushRun(); err != nil {
				return err
			}
		}
		if len(run.ext) == 0 {
			// A run's window is chosen once, at its first block, so this is
			// where correspondence is decided. The path matcher answers for
			// that block's file; the source cursor -- where the preceding copy
			// left off -- stands in when it cannot, and is retried if the
			// anchor turns out to have no window.
			run.srcFallback = srcCursor
			run.srcAnchor, run.anchoredBy = srcCursor, anchorNone
			if off, kind := match.anchor(e.Offset); kind != anchorNone {
				run.srcAnchor, run.anchoredBy = off, kind
			}
		}
		run.ext = append(run.ext, e)
	}
	if err := flushRun(); err != nil {
		return err
	}
	return flushCopy()
}

// verifyPlan runs the applier over the finished delta and compares the result
// with the real target byte for byte.
func verifyPlan(ctx context.Context, sourcePath, deltaPath string, tgt *squashfsImage, comp Compressor) error {
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	deltaFile, err := os.Open(deltaPath)
	if err != nil {
		return err
	}
	defer deltaFile.Close()

	// The reconstruction is compared as it arrives rather than buffered: the
	// applier writes the image once, in order, so a comparing writer catches the
	// first wrong byte without a second copy of a 70 MB image in the heap.
	cmp := &compareWriter{want: tgt.Data}
	if _, err := Apply(ctx, srcFile, deltaFile, cmp, &ApplyOpts{
		Comp: comp,
		// The source is right here on disk and was just hashed into the
		// header, so re-reading it to hash it again proves nothing.
		SkipSourceDigest: true,
	}); err != nil {
		return err
	}
	if cmp.at != len(cmp.want) {
		return fmt.Errorf("the reconstruction is %d bytes, %s is %d", cmp.at, tgt.Path, len(cmp.want))
	}
	return nil
}

// compareWriter checks a stream against expected bytes as they are written,
// failing at the first difference. It is what lets the final gate hold the target
// image once rather than twice.
type compareWriter struct {
	want []byte
	at   int
}

func (c *compareWriter) Write(p []byte) (int, error) {
	if c.at+len(p) > len(c.want) {
		return 0, fmt.Errorf("the reconstruction is longer than the %d-byte target", len(c.want))
	}
	want := c.want[c.at : c.at+len(p)]
	if !bytes.Equal(p, want) {
		return 0, fmt.Errorf("the reconstruction differs from the target at offset %d", c.at+firstDiff(p, want))
	}
	c.at += len(p)
	return len(p), nil
}

// firstDiff is the offset of the first differing byte, or the shorter length.
func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// crcWriter counts and checksums what goes into SEC_PAY, so its table entry can
// be filled in without a second pass over the payload.
type crcWriter struct {
	w   io.Writer
	n   int64
	crc uint32
}

func newCRCWriter(w io.Writer) *crcWriter { return &crcWriter{w: w} }

func (c *crcWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		c.crc = crc32.Update(c.crc, crc32.IEEETable, p[:n])
		c.n += int64(n)
	}
	return n, err
}
