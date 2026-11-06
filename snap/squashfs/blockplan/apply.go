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
	"encoding/binary"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"strings"

	"github.com/snapcore/snapd/logger"
)

// Applying a block-plan delta is a single forward pass that writes the target
// exactly once:
//
//	[superblock from SEC_SB][data region from the instructions]
//	[metadata region recompressed from the patched blob][SEC_MDTAIL][zero pad]
//
// The applier makes no squashfs structural decisions. It never parses an inode,
// never walks a directory, never chooses between compressed and raw. Every such
// decision was made and verified by the generator and arrives as an explicit
// number, so the only thing that can go wrong here is a mismatch -- and every
// mismatch is checked:
//
//   - SEC_CANARY proves the local compressor reproduces the generator's bytes,
//     before a single target byte is written;
//   - every recompressed block's on-disk length must equal the recorded one;
//   - the data region must end exactly on inode_table_start, the metadata region
//     exactly on export_table_start, the tail exactly on bytes_used;
//   - the whole image's sha256 must equal the delta's target digest.
//
// The source is read at random offsets and never held whole, so peak memory is
// set by the run cap in the header, not by the size of the image.

// applyState is the mutable context threaded through one apply.
type applyState struct {
	pr   *planReader
	src  *os.File
	w    *targetWriter
	pay  *crcReader
	comp Compressor
	// blockSize is both the image block size and the LZMA2 dictionary size
	// for data blocks.
	blockSize int
	// oldFile, patchFile and newFile are the three files a patch run works
	// in: the source window, the patch, and the plaintext hpatchz rebuilds.
	// They are allocated once and reset per run, because a delta has as many
	// runs as the target has changed regions -- see memFile.
	oldFile, patchFile, newFile *memFile

	stats ApplyStats
}

// Apply reconstructs the target image from src and delta, writing it to out. src
// must be the exact source revision the delta was generated against, which the
// source digest in the delta proves before anything is written. opts may be nil.
func Apply(ctx context.Context, src *os.File, delta io.Reader, out io.Writer, opts *ApplyOpts) (*ApplyStats, error) {
	if opts == nil {
		opts = &ApplyOpts{}
	}
	pr, err := openPlan(delta)
	if err != nil {
		return nil, err
	}
	h := pr.Header

	// Negotiate the run cap before anything is read, written or forked. A patch
	// run needs its plaintext and its source window at once, so this one number
	// sets the apply's whole memory demand -- and a caller that cannot afford it
	// wants to know now.
	if opts.MaxRunUSize > 0 && int64(h.MaxRunUSize) > int64(opts.MaxRunUSize) {
		return nil, fmt.Errorf("the delta's patch runs need up to %s at once and this applier allows %s",
			humanBytes(int64(h.MaxRunUSize)), humanBytes(int64(opts.MaxRunUSize)))
	}

	fi, err := src.Stat()
	if err != nil {
		return nil, err
	}
	if uint64(fi.Size()) != h.SourceSize {
		return nil, fmt.Errorf("source is %d bytes, delta was built against %d", fi.Size(), h.SourceSize)
	}
	if !opts.SkipSourceDigest {
		if err := verifySourceDigest(src, fi.Size(), h.SourceSHA256); err != nil {
			return nil, err
		}
	}

	// The target superblock has to survive the same rules the generator
	// checked the real target against, or the delta describes an image this
	// code cannot assemble.
	sbBytes := pr.section(secSB)
	if len(sbBytes) != superblockSize {
		return nil, fmt.Errorf("SEC_SB is %d bytes, want %d", len(sbBytes), superblockSize)
	}
	tsb, err := parseSuperblock(sbBytes)
	if err != nil {
		return nil, fmt.Errorf("SEC_SB: %w", err)
	}
	if err := tsb.checkSupportedGeometry(int64(h.TargetSize)); err != nil {
		return nil, fmt.Errorf("target superblock: %w", err)
	}
	// The header duplicates three of the superblock's numbers so they can be
	// bounds-checked before any section is read. They must agree.
	if tsb.InodeTableStart != h.TargetInodeTableStart || tsb.BytesUsed != h.TargetBytesUsed {
		return nil, fmt.Errorf("SEC_SB disagrees with the delta header: inode_table %d vs %d, bytes_used %d vs %d",
			tsb.InodeTableStart, h.TargetInodeTableStart, tsb.BytesUsed, h.TargetBytesUsed)
	}
	if tsb.BlockSize != h.BlockSize {
		return nil, fmt.Errorf("SEC_SB block size %d disagrees with the delta header's %d", tsb.BlockSize, h.BlockSize)
	}
	// The compressor comes from the image the delta describes, so a device needs
	// no configuration to apply one -- and an image whose compressor this build
	// cannot reproduce byte for byte is refused here, before the target is
	// touched. An override is honoured only if it agrees with SEC_SB.
	comp := opts.Comp
	if comp == nil {
		if comp, err = newCompressor(tsb.CompressionId, opts.Jobs); err != nil {
			return nil, err
		}
	} else if err := checkCompressorMatches(comp, tsb.CompressionId); err != nil {
		return nil, err
	}

	st := &applyState{pr: pr, src: src, comp: comp, blockSize: int(h.BlockSize)}
	st.pay = newCRCReader(pr.pay, pr.payLen, pr.payCRC)

	// The source's own metadata region feeds both the metadata patch and the
	// canary, so decode it up front: ~200 KB of it, and any failure lands
	// before the target has been touched.
	srcMeta, err := readSourceMetaBlob(ctx, src, fi.Size(), comp)
	if err != nil {
		return nil, fmt.Errorf("source metadata: %w", err)
	}
	// The recorded tool versions are advisory: the canary and the byte checks
	// are the gates, so drift is a warning, not an error. It goes ahead of the
	// canary because the canary is the check compressor drift fails first, and
	// a warning logged after that error would never be logged at all.
	if tv := pr.section(secToolVer); tv != nil {
		checkToolVersions(ctx, logWarnings{}, tv, comp)
	}

	if canary := pr.section(secCanary); canary != nil {
		if err := checkCanary(ctx, canary, srcMeta, comp, st.blockSize); err != nil {
			return nil, err
		}
	}

	// Everything the metadata region needs is settled before any data byte is
	// written, so a bad metadata patch costs no data work at all.
	mdFrame, mdDigest, err := decodeMDFrame(pr.section(secMDFrame), int64(h.TargetInodeTableStart))
	if err != nil {
		return nil, fmt.Errorf("SEC_MDFRAME: %w", err)
	}
	var mdOnDisk int64
	var mdUTotal int
	for _, b := range mdFrame {
		mdOnDisk += int64(2 + b.CSize)
		mdUTotal += b.USize
	}
	if want := int64(tsb.ExportTableStart - tsb.InodeTableStart); mdOnDisk != want {
		return nil, fmt.Errorf("SEC_MDFRAME describes %d bytes of metadata, the superblock leaves room for %d",
			mdOnDisk, want)
	}
	targetMeta, err := buildTargetMetaBlob(ctx, pr, srcMeta, mdUTotal, mdDigest)
	if err != nil {
		return nil, err
	}

	// The scratch the patch runs share, allocated while a failure still costs
	// nothing: a device that cannot spare it should not learn that halfway
	// through an image.
	if err := st.openScratch(); err != nil {
		return nil, err
	}
	defer st.closeScratch()

	st.w = newTargetWriter(out)
	if _, err := st.w.Write(sbBytes); err != nil {
		return nil, err
	}
	if err := st.applyInstructions(ctx); err != nil {
		return nil, err
	}
	if st.w.pos != int64(h.TargetInodeTableStart) {
		return nil, fmt.Errorf("the instructions produced %d bytes of data region, the superblock puts the inode table at %d",
			st.w.pos-superblockSize, h.TargetInodeTableStart)
	}

	if err := st.writeMetaRegion(ctx, targetMeta, mdFrame); err != nil {
		return nil, err
	}
	if st.w.pos != int64(tsb.ExportTableStart) {
		return nil, fmt.Errorf("the metadata region ended at %d, the superblock puts the export table at %d",
			st.w.pos, tsb.ExportTableStart)
	}

	tail := pr.section(secMDTail)
	if want := int64(tsb.BytesUsed - tsb.ExportTableStart); int64(len(tail)) != want {
		return nil, fmt.Errorf("SEC_MDTAIL is %d bytes, the superblock leaves room for %d", len(tail), want)
	}
	if _, err := st.w.Write(tail); err != nil {
		return nil, err
	}
	if err := st.w.writeZeros(int64(h.TargetSize) - st.w.pos); err != nil {
		return nil, err
	}
	if st.w.pos != int64(h.TargetSize) {
		return nil, fmt.Errorf("wrote %d bytes, the delta describes a %d-byte image", st.w.pos, h.TargetSize)
	}

	// SEC_PAY is the last section, so draining it both finishes its CRC and
	// reveals payload the instructions never claimed.
	left, err := st.pay.drain()
	if err != nil {
		return nil, err
	}
	if err := st.pay.verify(); err != nil {
		return nil, err
	}
	if left != 0 {
		return nil, fmt.Errorf("%d bytes of SEC_PAY were not consumed by the instructions", left)
	}
	if got := st.w.digest.Sum(nil); !bytes.Equal(got, h.TargetSHA256[:]) {
		return nil, fmt.Errorf("reconstructed image digest %x does not match the delta's %x",
			got[:8], h.TargetSHA256[:8])
	}
	return &st.stats, nil
}

// ApplyToFile is Apply for the two files it is really driven with: the source
// revision already on the device, and the target it is turning into.
//
// The target is assembled in a temporary file beside it and renamed only once
// the apply has succeeded, so targetSnap either does not exist or is the whole
// image. That matters most for the refusals Apply makes before it does any
// work -- a run cap the caller cannot afford, a source the delta was not built
// against -- because those are the cases where the caller still has a usable
// snap at targetSnap and a truncated file in its place would destroy it.
func ApplyToFile(ctx context.Context, sourceSnap string, delta io.Reader, targetSnap string, opts *ApplyOpts) (*ApplyStats, error) {
	src, err := os.Open(sourceSnap)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	tmp := targetSnap + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	// Removed on every path that does not reach the rename below, including a
	// panic; after the rename there is nothing at tmp for this to find.
	defer func() {
		out.Close()
		os.Remove(tmp)
	}()

	stats, err := Apply(ctx, src, delta, out, opts)
	if err != nil {
		return nil, err
	}
	// Closed explicitly, because a write that only fails on close would
	// otherwise be reported as a successful apply.
	if err := out.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, targetSnap); err != nil {
		return nil, err
	}
	return stats, nil
}

// openScratch allocates the three files a patch run works in. They are allocated
// here rather than per run because an apply has as many runs as the target has
// changed regions, and reused rather than reallocated because that is three
// files over a whole apply instead of three thousand.
func (st *applyState) openScratch() error {
	files := make([]*memFile, 0, 3)
	for _, name := range []string{"old", "patch", "new"} {
		m, err := newMemFile(name)
		if err != nil {
			for _, f := range files {
				f.Close()
			}
			return err
		}
		files = append(files, m)
	}
	st.oldFile, st.patchFile, st.newFile = files[0], files[1], files[2]
	return nil
}

func (st *applyState) closeScratch() {
	for _, f := range []*memFile{st.oldFile, st.patchFile, st.newFile} {
		if f != nil {
			f.Close()
		}
	}
}

// resetScratch empties the three files for the next run, which is what makes
// reusing them safe: a run shorter than the one before it would otherwise be
// read back with the previous run's tail still on the end.
func (st *applyState) resetScratch() error {
	for _, f := range []*memFile{st.oldFile, st.patchFile, st.newFile} {
		if err := f.Reset(); err != nil {
			return err
		}
	}
	return nil
}

// applyInstructions walks the instruction stream, writing the data region.
func (st *applyState) applyInstructions(ctx context.Context) error {
	h := st.pr.Header
	instr := st.pr.section(secInstr)
	d := newInstrDecoder(instr, st.blockSize, int64(h.SourceSize), int64(h.MaxRunUSize),
		st.comp.NeedsBlockSizes())

	var in instruction
	for i := uint32(0); i < h.InstrCount; i++ {
		if err := d.Next(&in); err != nil {
			return fmt.Errorf("instruction %d: %w", i, err)
		}
		switch in.Op {
		case opCopy:
			// The whole point of the format: already-compressed bytes
			// move from source to target with no compressor involved.
			n, err := copyNBuffer(st.w, io.NewSectionReader(st.src, in.SrcOff, in.Len), in.Len)
			if err != nil {
				return fmt.Errorf("instruction %d: copying %d bytes from source offset %d: %w",
					i, in.Len, in.SrcOff, err)
			}
			if n != in.Len {
				return fmt.Errorf("instruction %d: copied %d of %d bytes from source offset %d",
					i, n, in.Len, in.SrcOff)
			}
			st.stats.Copies++
			st.stats.CopiedBytes += in.Len
		case opLiteral:
			n, err := copyNBuffer(st.w, st.pay, in.Len)
			if err != nil {
				return fmt.Errorf("instruction %d: %w", i, err)
			}
			if n != in.Len {
				return fmt.Errorf("instruction %d: SEC_PAY ran out after %d of %d literal bytes", i, n, in.Len)
			}
			st.stats.Literals++
			st.stats.LiteralBytes += in.Len
		case opPatchRun:
			if err := st.applyPatchRun(ctx, &in); err != nil {
				return fmt.Errorf("instruction %d: %w", i, err)
			}
			st.stats.PatchRuns++
		default:
			return fmt.Errorf("instruction %d: opcode %v reached the applier", i, in.Op)
		}
		st.stats.Instructions++
	}
	if !d.done() {
		return fmt.Errorf("%d bytes remain after the header's %d instructions", len(instr)-d.pos, h.InstrCount)
	}
	return nil
}

// applyPatchRun reconstructs a run of blocks' plaintext by patching source
// windows, compresses each block, and writes it only if its on-disk length is
// exactly what the generator recorded.
func (st *applyState) applyPatchRun(ctx context.Context, in *instruction) error {
	uTotal := in.USizeTotal()

	// Three files rather than three heap buffers, because these are the largest
	// things an apply handles: the run's plaintext, which MaxRunUSize caps, and
	// the source window it is rebuilt from, which is a multiple of that again.
	// The window streams from the source through the decompressor straight into
	// the file hpatchz reads, hpatchz writes the plaintext into a file of its
	// own, and the compressor reads that one block at a time -- so a 12 MiB
	// window and an 8 MiB run cost this process a pipe buffer and one block, not
	// 20 MiB.
	if err := st.resetScratch(); err != nil {
		return err
	}
	nWin, err := gatherWindowsTo(ctx, st.src, in.Windows, st.oldFile.File(), st.comp, st.blockSize)
	if err != nil {
		return err
	}
	st.stats.WindowUBytes += nWin

	n, err := copyNBuffer(st.patchFile.File(), st.pay, int64(in.PatchLen))
	if err != nil {
		return fmt.Errorf("reading a %d-byte patch from SEC_PAY: %w", in.PatchLen, err)
	}
	if n != int64(in.PatchLen) {
		return fmt.Errorf("SEC_PAY ran out after %d of %d patch bytes", n, in.PatchLen)
	}

	if err := runHpatchzFiles(ctx, st.oldFile, st.patchFile, st.newFile, uTotal); err != nil {
		return err
	}
	// All three scratch files hold their run's bytes at this point, which makes
	// now the high-water mark of everything but the heap.
	if scratch := nWin + int64(in.PatchLen) + uTotal; scratch > st.stats.PeakScratchBytes {
		st.stats.PeakScratchBytes = scratch
	}

	blocks := in.Blocks
	uSizes := make([]int, len(blocks))
	for i, b := range blocks {
		uSizes[i] = b.USize
	}
	plain := newPlainFile(st.newFile.File(), int(uTotal))
	return st.comp.CompressBlocks(ctx, plain, uSizes, st.blockSize, func(idx int, blk CompressedBlock) error {
		want := blocks[idx]
		if blk.OnDiskLen() != want.CSize || blk.Raw != want.Raw() {
			return fmt.Errorf("block %d recompressed to %d bytes (raw=%v), the delta says %d (raw=%v)",
				idx, blk.OnDiskLen(), blk.Raw, want.CSize, want.Raw())
		}
		if _, err := st.w.Write(blk.OnDisk); err != nil {
			return err
		}
		st.stats.BlocksCompressed++
		st.stats.UCompressedBytes += int64(want.USize)
		st.stats.PatchedBytes += int64(want.USize)
		return nil
	})
}

// gatherWindowsTo decompresses the source windows to dst, in order, and reports
// how much plaintext that was. dst is the patch's "old" file, holding exactly
// what the generator diffed against.
//
// This is the only decompression the applier does over the data region, and it is
// the format's second economy after OP_COPY: decompression runs roughly an
// order of magnitude faster than compression, so reading back the little source
// plaintext a changed run needs costs far less than the whole-image decompress
// snap-1-1-xdelta3 does -- and unlike it, it is never followed by recompressing
// bytes that did not change.
func gatherWindowsTo(ctx context.Context, src *os.File, windows []srcWindow, dst io.Writer, dec Decompressor, blockSize int) (int64, error) {
	var total int64
	for _, win := range windows {
		// The compressed window streams out of the source rather than being read
		// into a buffer first, so the window's size does not reach the heap from
		// this side either.
		stored := io.NewSectionReader(src, win.Off, int64(win.Len))
		if win.Plain() {
			// Already plaintext, so there is nothing to decompress and no
			// process to run -- see srcWindow.Plain.
			n, err := copyNBuffer(dst, stored, int64(win.Len))
			if err != nil {
				return total, fmt.Errorf("reading source window [%d,+%d): %w", win.Off, win.Len, err)
			}
			if n != int64(win.Len) {
				return total, fmt.Errorf("source window [%d,+%d) ended after %d bytes", win.Off, win.Len, n)
			}
			total += n
			continue
		}
		// One call for the whole window rather than one per block, which for xz
		// is one process for however many blocks it spans. The declared
		// plaintext length is enforced, so a window that does not decompress to
		// exactly ULen is rejected here.
		n, err := dec.DecompressTo(ctx, dst, stored, win.CSizes, blockSize, win.ULen)
		if err != nil {
			return total, fmt.Errorf("decompressing source window [%d,+%d): %w", win.Off, win.Len, err)
		}
		total += n
	}
	return total, nil
}

// --- metadata region ---

// readSourceMetaBlob decompresses the source's metadata region. This is the only
// place the applier looks inside the source image, and it reads nothing but the
// superblock's table pointers and the two-byte metadata block headers -- no
// inodes, no directories, no block size words.
//
// comp is the compressor the delta describes the target with, and the source has
// to have been built by the same one: every OP_COPY moves source bytes into the
// target unchanged, so a source compressed differently would put blocks in the
// image that its own superblock contradicts.
func readSourceMetaBlob(ctx context.Context, src *os.File, size int64, comp Compressor) ([]byte, error) {
	head := make([]byte, superblockSize)
	if _, err := src.ReadAt(head, 0); err != nil {
		return nil, err
	}
	sb, err := parseSuperblock(head)
	if err != nil {
		return nil, err
	}
	if err := sb.checkSupportedGeometry(size); err != nil {
		return nil, err
	}
	if sb.CompressionId != comp.ID() {
		return nil, fmt.Errorf("source was built with %s, the delta describes a %s image",
			compressorName(sb.CompressionId), compressorName(comp.ID()))
	}
	start, end := int64(sb.InodeTableStart), int64(sb.ExportTableStart)
	region := make([]byte, end-start)
	if _, err := src.ReadAt(region, start); err != nil {
		return nil, err
	}
	// walkMetaRegion works over an image view, so present the region as one.
	// Assembled here rather than through newImageView because the compressor is
	// already at hand: this is the one the delta is being applied with, and
	// deriving a second one from the same id would build it twice.
	view := &squashfsImage{Path: src.Name(), Data: region, SB: sb, Dec: comp}
	reg, err := view.walkMetaRegion(ctx, 0, int64(len(region)))
	if err != nil {
		return nil, err
	}
	return reg.Blob, nil
}

// buildTargetMetaBlob turns the source metadata blob into the target's, by
// patching it when SEC_MDPATCH is present and taking it unchanged when it is
// not -- which is how "the metadata did not change" is encoded.
//
// The digest check is what makes the ordering claim above true: it settles the
// whole metadata region here, before a single data byte is written, so a bad
// patch costs no data work rather than being caught at recompression time or,
// worse, only by the final image digest.
func buildTargetMetaBlob(ctx context.Context, pr *planReader, srcMeta []byte, wantLen int, wantDigest [32]byte) ([]byte, error) {
	out := srcMeta
	if patch := pr.section(secMDPatch); patch != nil {
		var err error
		if out, err = runHpatchz(ctx, srcMeta, patch, int64(wantLen)); err != nil {
			return nil, fmt.Errorf("SEC_MDPATCH: %w", err)
		}
	}
	if len(out) != wantLen {
		return nil, fmt.Errorf("target metadata is %d bytes, SEC_MDFRAME describes %d", len(out), wantLen)
	}
	if got := sha256.Sum256(out); got != wantDigest {
		return nil, fmt.Errorf("reconstructed metadata digest %x does not match SEC_MDFRAME's %x",
			got[:8], wantDigest[:8])
	}
	return out, nil
}

// writeMetaRegion recompresses the target metadata blob block by block, checking
// each against the framing the delta recorded. Unlike the data region this is
// unconditional work, but it is only ~600 KB of plaintext.
func (st *applyState) writeMetaRegion(ctx context.Context, blob []byte, frame []metaBlock) error {
	uSizes := make([]int, len(frame))
	for i, b := range frame {
		uSizes[i] = b.USize
	}
	perCall := st.comp.MaxBlocksPerCall()
	base := 0
	for i := 0; i < len(frame); {
		j := min(i+perCall, len(frame))
		batch, batchU := frame[i:j], 0
		for _, b := range batch {
			batchU += b.USize
		}
		err := st.comp.CompressBlocks(ctx, plainBytes(blob[base:base+batchU]), uSizes[i:j], squashfsMetadataSize,
			func(idx int, blk CompressedBlock) error {
				want := batch[idx]
				if blk.OnDiskLen() != want.CSize || blk.Raw != want.Raw {
					return fmt.Errorf("metadata block %d recompressed to %d bytes (raw=%v), the delta says %d (raw=%v)",
						i+idx, blk.OnDiskLen(), blk.Raw, want.CSize, want.Raw)
				}
				size := uint16(want.CSize)
				if want.Raw {
					size |= metaUncompressedBit
				}
				var hdr [2]byte
				binary.LittleEndian.PutUint16(hdr[:], size)
				if _, err := st.w.Write(hdr[:]); err != nil {
					return err
				}
				_, err := st.w.Write(blk.OnDisk)
				return err
			})
		if err != nil {
			return err
		}
		st.stats.MetaBlocks += len(batch)
		st.stats.MetaUBytes += int64(batchU)
		st.stats.BlocksCompressed += len(batch)
		st.stats.UCompressedBytes += int64(batchU)
		base += batchU
		i = j
	}
	return nil
}

// --- canary ---

// canarySize is how many bytes of the source metadata blob the canary
// compresses. It is well under a metadata block so both configurations see a
// realistic partial block, which is the common case: most blocks in a snap are
// partial tails.
const canarySize = 4096

// canaryPayloadLen is 4 + (4 + 32) per configuration.
const canaryPayloadLen = 4 + 2*36

// canaryDicts are the two configurations a delta uses, in the order the payload
// records them.
//
// For a compressor with no dictionary the two coincide and the payload records
// the same block twice, which costs 36 bytes and is left alone: the check is
// about the compressor's output drifting, and a compressor whose output does not
// depend on this parameter has no third case to cover.
func canaryDicts(blockSize int) [2]int { return [2]int{blockSize, squashfsMetadataSize} }

// buildCanary compresses the head of the source metadata blob under both
// configurations and records what came out. It carries no input bytes: both
// sides already have the source.
func buildCanary(ctx context.Context, srcMeta []byte, comp Compressor, blockSize int) ([]byte, error) {
	n := min(canarySize, len(srcMeta))
	if n == 0 {
		return nil, fmt.Errorf("source metadata is empty, so no canary can be built")
	}
	out := make([]byte, 0, canaryPayloadLen)
	out = appendUint32(out, uint32(n))
	for _, dict := range canaryDicts(blockSize) {
		blk, err := canaryBlock(ctx, comp, srcMeta[:n], dict)
		if err != nil {
			return nil, err
		}
		out = appendUint32(out, uint32(len(blk)))
		sum := sha256.Sum256(blk)
		out = append(out, sum[:]...)
	}
	return out, nil
}

// checkCanary is the toolchain gate: if the local compressor does not reproduce
// the generator's bytes then no block can be recompressed here, and that has to
// be found now rather than halfway through assembling an image.
func checkCanary(ctx context.Context, canary, srcMeta []byte, comp Compressor, blockSize int) error {
	if len(canary) != canaryPayloadLen {
		return fmt.Errorf("SEC_CANARY is %d bytes, want %d", len(canary), canaryPayloadLen)
	}
	n := int(binary.LittleEndian.Uint32(canary))
	if n <= 0 || n > len(srcMeta) || n > squashfsMetadataSize {
		return fmt.Errorf("SEC_CANARY declares %d bytes of a %d-byte source metadata blob", n, len(srcMeta))
	}
	for i, dict := range canaryDicts(blockSize) {
		rec := canary[4+i*36:]
		wantLen, wantSum := binary.LittleEndian.Uint32(rec), rec[4:36]
		blk, err := canaryBlock(ctx, comp, srcMeta[:n], dict)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(blk)
		if uint32(len(blk)) != wantLen || !bytes.Equal(sum[:], wantSum) {
			return fmt.Errorf("incompatible %s toolchain: with dict=%d the canary compresses to %d bytes %x, the delta expects %d bytes %x",
				compressorName(comp.ID()), dict, len(blk), sum[:8], wantLen, wantSum[:8])
		}
	}
	return nil
}

// canaryBlock compresses the canary input as a single block and returns its
// framed bytes. A store-raw verdict would mean the input was incompressible,
// which real metadata never is, so it is treated as a broken canary.
func canaryBlock(ctx context.Context, comp Compressor, plain []byte, dictSize int) ([]byte, error) {
	var framed []byte
	var raw bool
	err := comp.CompressBlocks(ctx, plainBytes(plain), []int{len(plain)}, dictSize, func(_ int, blk CompressedBlock) error {
		// OnDisk is only valid inside the callback, so keep a copy rather than
		// the block.
		framed = append([]byte(nil), blk.OnDisk...)
		raw = blk.Raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	if raw {
		return nil, fmt.Errorf("the canary input did not compress at all, which no real metadata does")
	}
	return framed, nil
}

// --- output plumbing ---

// targetWriter counts and hashes everything written to the target, so the length
// and digest checks cost nothing extra.
type targetWriter struct {
	w      io.Writer
	pos    int64
	digest hash.Hash
	zeros  []byte
}

func newTargetWriter(w io.Writer) *targetWriter {
	return &targetWriter{w: w, digest: sha256.New()}
}

func (t *targetWriter) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	if n > 0 {
		t.digest.Write(p[:n])
		t.pos += int64(n)
	}
	return n, err
}

func (t *targetWriter) writeZeros(n int64) error {
	if n < 0 {
		return fmt.Errorf("image is %d bytes past its declared size", -n)
	}
	if t.zeros == nil {
		t.zeros = make([]byte, squashfsPadding)
	}
	for n > 0 {
		chunk := int64(len(t.zeros))
		if n < chunk {
			chunk = n
		}
		if _, err := t.Write(t.zeros[:chunk]); err != nil {
			return err
		}
		n -= chunk
	}
	return nil
}

// crcReader hands out SEC_PAY while checksumming it. The section's CRC cannot be
// checked at open time -- the payload is never held whole -- so it is checked
// once the last byte has gone past.
type crcReader struct {
	r    io.Reader
	left int64
	crc  uint32
	want uint32
}

func newCRCReader(r io.Reader, n int64, want uint32) *crcReader {
	if r == nil {
		r = bytes.NewReader(nil)
	}
	return &crcReader{r: r, left: n, want: want}
}

func (c *crcReader) Read(p []byte) (int, error) {
	if c.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.crc = crc32.Update(c.crc, crc32.IEEETable, p[:n])
		c.left -= int64(n)
	}
	return n, err
}

// drain consumes whatever is left, so the CRC covers the whole section, and
// reports how much that was.
func (c *crcReader) drain() (int64, error) {
	return io.Copy(io.Discard, c)
}

func (c *crcReader) verify() error {
	if c.left != 0 {
		return fmt.Errorf("SEC_PAY is %d bytes short", c.left)
	}
	if c.crc != c.want {
		return fmt.Errorf("SEC_PAY is corrupt: CRC32 %#08x, expected %#08x", c.crc, c.want)
	}
	return nil
}

// verifySourceDigest is what makes every OP_COPY safe: the copied bytes are
// never checked individually, so the source as a whole is checked once.
func verifySourceDigest(src *os.File, size int64, want [32]byte) error {
	h := sha256.New()
	if _, err := copyBuffer(h, io.NewSectionReader(src, 0, size)); err != nil {
		return err
	}
	if got := h.Sum(nil); !bytes.Equal(got, want[:]) {
		return fmt.Errorf("source digest %x does not match the delta's %x", got[:8], want[:8])
	}
	return nil
}

// logWarnings is where an apply's advisory messages end up. checkToolVersions
// takes a writer rather than reaching for the log so that a caller which wants
// to hold on to them can, a test included; the caller here is a device, where
// the log is exactly where a warning belongs.
type logWarnings struct{}

func (logWarnings) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			logger.Noticef("%s", line)
		}
	}
	return len(p), nil
}
