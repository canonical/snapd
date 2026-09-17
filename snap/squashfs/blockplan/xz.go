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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
)

// This file reproduces, byte for byte, the .xz streams that mksquashfs stores
// in a squashfs image, without linking liblzma.
//
// Every squashfs data and metadata block is compressed independently by
// xz_wrapper.c as a complete .xz stream:
//
//	lzma_stream_buffer_encode(chain, LZMA_CHECK_CRC32, NULL,
//	                          src, size, out, &len, block_size)
//
// with a plain LZMA2 chain at preset 6 whose dict_size is the block size
// (131072 for data blocks, SQUASHFS_METADATA_SIZE=8192 for metadata).
//
// The container that call emits is fully synthesizable, so only the LZMA2
// payload has to come from the real encoder -- and that can be obtained for a
// whole run of blocks with a single `xz --block-list=...` invocation, which
// amortizes away the process cost that makes per-block invocation twice as
// expensive as in-process liblzma.
//
// The one non-obvious rule lives in liblzma's block_buffer_encoder.c: the block
// header is laid out for the *worst case* compressed size lzma2_bound(uSize),
// and then the actual, shorter, compressed-size varint is written into that
// oversized header and zero-padded. That is why buffer-encoder output (16-byte
// header, flags 0xC0 with both size fields present) differs from what the
// streaming encoder -- including Go's and Python's stdlib wrappers -- produces
// (12-byte header, flags 0x00). Reproducing squashfs blocks requires the
// buffer-encoder form.

// compressorXz is the squashfs compressor id of xz, which is what mksquashfs
// defaults to and what every snap in the store is built with.
const compressorXz uint16 = 4

// xz needs no library, so unlike lzo and zstd it is part of every build and
// registers unconditionally.
func init() {
	registerCompressor(compressorXz, func(jobs int) (Compressor, error) {
		return &xzCLI{jobs: jobs}, nil
	}, codecXZ, xzDecompressAll)
}

const (
	// xzCheckCRC32 is the stream flags check id for CRC32, which is what
	// xz_wrapper.c asks for.
	xzCheckCRC32 = 0x01
	// lzma2FilterID is the .xz filter id of LZMA2.
	lzma2FilterID = 0x21

	// lzma2ChunkMax and lzma2HeaderUncompressed mirror liblzma's
	// lzma2_encoder.c: an incompressible chunk carries at most 2 MiB of
	// payload behind a 3-byte header.
	lzma2ChunkMax           = 1 << 21
	lzma2HeaderUncompressed = 3

	// blockHeaderFlagCompressedSize and blockHeaderFlagUncompressedSize are
	// the .xz block header flag bits announcing the two optional size
	// fields. The buffer encoder always sets both.
	blockHeaderFlagCompressedSize   = 0x40
	blockHeaderFlagUncompressedSize = 0x80

	// vliMax is the largest representable .xz variable-length integer.
	vliMax = uint64(1)<<63 - 1
)

var (
	xzStreamMagic = []byte{0xfd, '7', 'z', 'X', 'Z', 0x00}
	xzFooterMagic = []byte{'Y', 'Z'}
)

// appendVLI appends n in the .xz variable-length integer encoding, seven bits
// per byte, little end first, high bit marking continuation.
func appendVLI(b []byte, n uint64) []byte {
	for n >= 0x80 {
		b = append(b, byte(n)|0x80)
		n >>= 7
	}
	return append(b, byte(n))
}

// vliLen returns the number of bytes appendVLI would write for n.
func vliLen(n uint64) int {
	l := 1
	for n >= 0x80 {
		n >>= 7
		l++
	}
	return l
}

// readVLI decodes a variable-length integer at buf[pos:], returning the value
// and the position just past it.
func readVLI(buf []byte, pos int) (uint64, int, error) {
	var n uint64
	for shift := uint(0); ; shift += 7 {
		if pos >= len(buf) {
			return 0, 0, io.ErrUnexpectedEOF
		}
		if shift > 63 {
			return 0, 0, fmt.Errorf("varint longer than 9 bytes")
		}
		b := buf[pos]
		pos++
		n |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			if b == 0 && shift != 0 {
				return 0, 0, fmt.Errorf("varint has a redundant trailing zero byte")
			}
			return n, pos, nil
		}
	}
}

// lzma2Bound is liblzma's lzma2_bound(): the largest LZMA2 stream that u bytes
// of input can produce, which is u stored verbatim in 2 MiB chunks plus the
// end-of-payload marker.
func lzma2Bound(u int) int {
	chunks := (u + lzma2ChunkMax - 1) / lzma2ChunkMax
	return u + chunks*lzma2HeaderUncompressed + 1
}

// dictProp returns the LZMA2 properties byte encoding dictSize, which liblzma
// derives as dict = (2 | (p & 1)) << (p/2 + 11). Sizes that are not exactly
// representable are rejected rather than rounded, because rounding would
// silently produce blocks that do not match the image.
func dictProp(dictSize int) (byte, error) {
	for p := uint(0); p < 41; p++ {
		if uint64(2|(p&1))<<(p/2+11) == uint64(dictSize) {
			return byte(p), nil
		}
	}
	return 0, fmt.Errorf("dictionary size %d is not exactly representable as an LZMA2 properties byte", dictSize)
}

// blockHeaderSize returns the .xz block header size that
// lzma_block_buffer_encode reserves for a block of uSize bytes: sized for the
// worst-case compressed size, not the real one.
func blockHeaderSize(uSize int) int {
	n := 2 // header size byte + flags
	n += vliLen(uint64(lzma2Bound(uSize)))
	n += vliLen(uint64(uSize))
	n += vliLen(lzma2FilterID) + vliLen(1) + 1 // filter id, props length, props
	n += 4                                     // header CRC32
	return (n + 3) & ^3
}

// appendXZFrame appends to dst the complete .xz stream that
// lzma_stream_buffer_encode would emit for a block whose LZMA2 payload is
// payload, whose uncompressed form is uSize bytes long with CRC32 uCRC, and
// which was compressed with the given dictionary size.
func appendXZFrame(dst, payload []byte, uSize int, uCRC uint32, dictSize int) ([]byte, error) {
	prop, err := dictProp(dictSize)
	if err != nil {
		return nil, err
	}
	if uint64(len(payload)) > vliMax || uint64(uSize) > vliMax {
		return nil, fmt.Errorf("block too large to frame: %d compressed, %d uncompressed", len(payload), uSize)
	}
	hSize := blockHeaderSize(uSize)

	hdr := make([]byte, 0, hSize)
	hdr = append(hdr, byte(hSize/4-1))
	hdr = append(hdr, blockHeaderFlagCompressedSize|blockHeaderFlagUncompressedSize)
	hdr = appendVLI(hdr, uint64(len(payload))) // the real size, in a header sized for the bound
	hdr = appendVLI(hdr, uint64(uSize))
	hdr = appendVLI(hdr, lzma2FilterID)
	hdr = appendVLI(hdr, 1)
	hdr = append(hdr, prop)
	if len(hdr) > hSize-4 {
		return nil, fmt.Errorf("internal error: block header overflowed its reserved %d bytes", hSize)
	}
	for len(hdr) < hSize-4 {
		hdr = append(hdr, 0)
	}
	hdr = appendUint32(hdr, crc32.ChecksumIEEE(hdr))

	flags := [2]byte{0x00, xzCheckCRC32}

	dst = append(dst, xzStreamMagic...)
	dst = append(dst, flags[:]...)
	dst = appendUint32(dst, crc32.ChecksumIEEE(flags[:]))

	dst = append(dst, hdr...)
	dst = append(dst, payload...)
	for pad := (-len(payload)) & 3; pad > 0; pad-- {
		dst = append(dst, 0)
	}
	dst = appendUint32(dst, uCRC)

	// index: one record of (unpadded size, uncompressed size)
	idx := make([]byte, 0, 32)
	idx = append(idx, 0x00) // index indicator
	idx = appendVLI(idx, 1) // record count
	idx = appendVLI(idx, uint64(hSize+len(payload)+4))
	idx = appendVLI(idx, uint64(uSize))
	for pad := (-len(idx)) & 3; pad > 0; pad-- {
		idx = append(idx, 0)
	}
	idx = appendUint32(idx, crc32.ChecksumIEEE(idx))
	dst = append(dst, idx...)

	tail := make([]byte, 0, 6)
	tail = appendUint32(tail, uint32(len(idx)/4-1)) // backward size
	tail = append(tail, flags[:]...)
	dst = appendUint32(dst, crc32.ChecksumIEEE(tail))
	dst = append(dst, tail...)
	dst = append(dst, xzFooterMagic...)
	return dst, nil
}

// xzFramedLen returns the length appendXZFrame would produce, without building
// it. Used to take the store-raw decision before committing a buffer.
func xzFramedLen(payloadLen, uSize int) int {
	hSize := blockHeaderSize(uSize)
	idx := 1 + vliLen(1) + vliLen(uint64(hSize+payloadLen+4)) + vliLen(uint64(uSize))
	idx = (idx + 3) & ^3
	return 12 + hSize + payloadLen + ((-payloadLen) & 3) + 4 + idx + 4 + 12
}

// xzBlockSplitter walks an .xz stream forward, yielding each block's LZMA2
// payload as it arrives. It never seeks back to the index, so it can consume
// the output of a running xz process, but that requires the block headers to
// carry the compressed size -- which `xz -T1` omits. Refuse rather than buffer.
type xzBlockSplitter struct {
	r    *bufio.Reader
	hdr  []byte
	done bool
}

func newXZBlockSplitter(r io.Reader) (*xzBlockSplitter, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	head := make([]byte, 12)
	if _, err := io.ReadFull(br, head); err != nil {
		return nil, fmt.Errorf("cannot read xz stream header: %w", err)
	}
	if !bytes.Equal(head[:6], xzStreamMagic) {
		return nil, fmt.Errorf("not an xz stream (magic %x)", head[:6])
	}
	if head[7] != xzCheckCRC32 {
		return nil, fmt.Errorf("xz stream uses check %#x, want CRC32", head[7])
	}
	return &xzBlockSplitter{r: br, hdr: make([]byte, 1024)}, nil
}

// next returns the next block's payload and its declared uncompressed size, or
// io.EOF once the index is reached. The returned slice is only valid until the
// following call.
func (s *xzBlockSplitter) next() (payload []byte, uSize int, err error) {
	if s.done {
		return nil, 0, io.EOF
	}
	b0, err := s.r.ReadByte()
	if err != nil {
		return nil, 0, fmt.Errorf("cannot read xz block header size: %w", err)
	}
	if b0 == 0x00 { // index indicator: no more blocks
		s.done = true
		return nil, 0, io.EOF
	}
	hSize := (int(b0) + 1) * 4
	hdr := s.hdr[:hSize]
	hdr[0] = b0
	if _, err := io.ReadFull(s.r, hdr[1:]); err != nil {
		return nil, 0, fmt.Errorf("cannot read xz block header: %w", err)
	}
	flags := hdr[1]
	if flags&blockHeaderFlagCompressedSize == 0 {
		return nil, 0, fmt.Errorf("xz block header omits the compressed size (flags %#02x); "+
			"run xz with -T2 or higher", flags)
	}
	cSize64, pos, err := readVLI(hdr, 2)
	if err != nil {
		return nil, 0, fmt.Errorf("cannot read compressed size: %w", err)
	}
	var uSize64 uint64
	if flags&blockHeaderFlagUncompressedSize != 0 {
		if uSize64, _, err = readVLI(hdr, pos); err != nil {
			return nil, 0, fmt.Errorf("cannot read uncompressed size: %w", err)
		}
	}
	cSize := int(cSize64)
	if cSize < 0 || cSize > 1<<30 {
		return nil, 0, fmt.Errorf("implausible compressed block size %d", cSize64)
	}
	buf := make([]byte, cSize+((-cSize)&3)+4) // payload + padding + check
	if _, err := io.ReadFull(s.r, buf); err != nil {
		return nil, 0, fmt.Errorf("cannot read xz block payload: %w", err)
	}
	return buf[:cSize], int(uSize64), nil
}

// xzCLI drives the xz command line tool, using it only as an LZMA2 payload
// source and synthesizing the squashfs container in appendXZFrame.
//
// It is the one implementation that does not need a library: the container
// lzma_stream_buffer_encode produces is fully synthesizable, so xz is used
// purely as an LZMA2 payload source and the framing happens here. That is also
// why it is the only implementation whose blocks are self-delimiting.
type xzCLI struct {
	// jobs is how many blocks xz may compress at once, passed as -T. Zero or
	// less means every core; it is floored at 2 because thread counts change
	// only speed, never output, but -T1 emits block headers without the
	// compressed size, which xzBlockSplitter cannot walk forward.
	jobs int
}

// xzMaxBlocksPerCall caps the --block-list argument. The kernel allows 128 KiB
// per argv entry and 8192 blocks of up to seven digits stay well inside that,
// while keeping the plaintext a caller must hold at once bounded.
const xzMaxBlocksPerCall = 8192

func (x *xzCLI) ID() uint16 { return compressorXz }

func (x *xzCLI) MaxBlocksPerCall() int { return xzMaxBlocksPerCall }

// NeedsBlockSizes is false: a squashfs xz block is a complete .xz stream and xz
// decodes a concatenation of them in one pass, so a run needs no framing beyond
// its own bytes.
func (x *xzCLI) NeedsBlockSizes() bool { return false }

func (x *xzCLI) SectionCodec() uint16 { return codecXZ }

// ToolVersion reports the xz this machine will recompress with. The probe is
// best-effort on purpose: a version line is diagnostic, not a gate.
func (x *xzCLI) ToolVersion() string {
	cmd, err := toolCommand(context.Background(), "xz", "--version")
	if err != nil {
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if line == "" {
		return ""
	}
	return "xz: " + line
}

// threadArg is the -T for a call compressing nBlocks blocks.
//
// Thread count changes only how fast the output arrives, never the output
// itself -- --block-list has already fixed where every block begins, so each
// one compresses from a fresh dictionary whichever thread takes it. What it
// does change is memory: each thread holds its own encoder state and its own
// slice of the input, measured here at 6.5 MiB for -T2 rising to 38 MiB for
// -T16 on the same input. That is the trade the job count makes.
//
// It is bounded twice. Below, by 2: -T1 is not merely slower, it omits the
// compressed size from each block header, which xzBlockSplitter needs in order
// to walk the stream forward instead of buffering it to reach the index. Above,
// by the number of blocks in the call, because a thread with no block to take
// costs its encoder state and buys nothing -- and a patch run is often a
// handful of blocks, where the difference is the whole allocation.
func (x *xzCLI) threadArg(nBlocks int) int {
	return max(min(resolveJobs(x.jobs), nBlocks), 2)
}

func (x *xzCLI) CompressBlocks(ctx context.Context, plain BlockPlain, uSizes []int, dictSize int, fn func(int, CompressedBlock) error) error {
	if len(uSizes) == 0 {
		return nil
	}
	if len(uSizes) > xzMaxBlocksPerCall {
		return fmt.Errorf("%d blocks in one call exceeds the cap of %d", len(uSizes), xzMaxBlocksPerCall)
	}
	total := 0
	for i, u := range uSizes {
		if u <= 0 {
			return fmt.Errorf("block %d has non-positive size %d", i, u)
		}
		total += u
	}
	if total != plain.Len() {
		return fmt.Errorf("block sizes sum to %d but %d bytes of plaintext were given", total, plain.Len())
	}
	if _, err := dictProp(dictSize); err != nil {
		return err
	}

	var list strings.Builder
	for i, u := range uSizes {
		if i > 0 {
			list.WriteByte(',')
		}
		list.WriteString(strconv.Itoa(u))
	}

	// --block-list forces a block boundary at each listed size, so every
	// block is compressed from a fresh dictionary exactly as mksquashfs
	// compresses it in isolation.
	args := []string{
		"-c", "-q", "--format=xz", "--check=crc32",
		fmt.Sprintf("-T%d", x.threadArg(len(uSizes))),
		fmt.Sprintf("--lzma2=preset=6,dict=%d", dictSize),
		"--block-list=" + list.String(),
		"-",
	}
	stdin, err := plain.Stream()
	if err != nil {
		return err
	}
	cmd, err := toolCommand(ctx, "xz", args...)
	if err != nil {
		return err
	}
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot run xz: %w", err)
	}
	// From here on every return must reap the child, and must drain or
	// close stdout so a blocked xz does not outlive this call.
	fail := func(err error) error {
		stdout.Close()
		cmd.Wait()
		return err
	}

	split, err := newXZBlockSplitter(stdout)
	if err != nil {
		return fail(err)
	}
	off, idx := 0, 0
	for ; idx < len(uSizes); idx++ {
		payload, declared, err := split.next()
		if err == io.EOF {
			return fail(fmt.Errorf("xz emitted only %d of %d blocks", idx, len(uSizes)))
		}
		if err != nil {
			return fail(err)
		}
		uSize := uSizes[idx]
		if declared != 0 && declared != uSize {
			return fail(fmt.Errorf("block %d: xz split at %d bytes, expected %d", idx, declared, uSize))
		}
		// The block's own plaintext, for its CRC32 and -- if it turns out to be
		// stored raw -- for its on-disk bytes. One block at a time is the whole
		// reason CompressBlocks takes a BlockPlain rather than a slice.
		src, err := plain.Block(off, uSize)
		if err != nil {
			return fail(fmt.Errorf("block %d: %w", idx, err))
		}
		off += uSize

		blk := CompressedBlock{USize: uSize}
		if xzFramedLen(len(payload), uSize) >= uSize {
			// mangle2(): compression did not shrink the block, so mksquashfs
			// stores it verbatim, and the plaintext is the on-disk form.
			blk.Raw = true
			blk.OnDisk = src
		} else {
			framed, err := appendXZFrame(nil, payload, uSize, crc32.ChecksumIEEE(src), dictSize)
			if err != nil {
				return fail(fmt.Errorf("block %d: %w", idx, err))
			}
			if len(framed) >= uSize {
				return fail(fmt.Errorf("internal error: block %d framed to %d bytes, xzFramedLen predicted %d",
					idx, len(framed), xzFramedLen(len(payload), uSize)))
			}
			blk.OnDisk = framed
		}
		if err := fn(idx, blk); err != nil {
			return fail(err)
		}
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("xz failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// DecompressTo ignores cSizes: xz needs no framing to find its block
// boundaries, and NeedsBlockSizes says as much, so nothing passes any.
func (x *xzCLI) DecompressTo(ctx context.Context, w io.Writer, r io.Reader, cSizes []int, maxUSize, wantLen int) (int64, error) {
	return xzDecompressTo(ctx, w, r, wantLen)
}

// DecompressBlocks decodes a run in a single process and splits the plaintext by
// each block's own declared length.
//
// The split points do not need decoding to be known: lzma_stream_buffer_encode
// records the uncompressed size in every block header, so walking the streams
// reads them straight out. That is also why cSizes is required here even though
// NeedsBlockSizes is false -- it is how the walk finds each stream.
func (x *xzCLI) DecompressBlocks(ctx context.Context, dst, src []byte, cSizes []int, maxUSize int) ([]byte, []int, error) {
	if len(cSizes) == 0 {
		if len(src) != 0 {
			return nil, nil, fmt.Errorf("%d bytes of blocks with no sizes given", len(src))
		}
		return dst, nil, nil
	}
	uSizes := make([]int, len(cSizes))
	total, off := 0, 0
	for i, c := range cSizes {
		if c <= 0 || off+c > len(src) {
			return nil, nil, fmt.Errorf("block %d claims %d bytes with %d of %d left",
				i, c, len(src)-off, len(src))
		}
		n, err := xzStreamUncompressedSize(src[off : off+c])
		if err != nil {
			return nil, nil, fmt.Errorf("block %d: %w", i, err)
		}
		if n <= 0 || n > maxUSize {
			return nil, nil, fmt.Errorf("block %d declares %d bytes of plaintext, outside (0,%d]", i, n, maxUSize)
		}
		uSizes[i] = n
		total += n
		off += c
	}
	if off != len(src) {
		return nil, nil, fmt.Errorf("%d bytes follow the last of %d blocks", len(src)-off, len(cSizes))
	}
	// bytes.Buffer over dst appends in place, so a caller accumulating region
	// after region keeps one growing slice rather than a copy per call.
	buf := bytes.NewBuffer(dst)
	buf.Grow(total)
	if _, err := xzDecompressTo(ctx, buf, bytes.NewReader(src), total); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), uSizes, nil
}

// CompressBlob compresses a small blob as an ordinary .xz stream. This is plain
// container-level compression and has nothing to do with reproducing squashfs
// blocks.
func (x *xzCLI) CompressBlob(ctx context.Context, raw []byte) ([]byte, error) {
	cmd, err := toolCommand(ctx, "xz", "-c", "-q", "-9", "-T1", "--format=xz", "--check=crc32", "-")
	if err != nil {
		return nil, err
	}
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("xz failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// xzStreamUncompressedSize reads the uncompressed size out of the first block
// header of an .xz stream, which the buffer encoder always records.
func xzStreamUncompressedSize(stream []byte) (int, error) {
	if len(stream) < 14 {
		return 0, fmt.Errorf("stream too short to hold a block header")
	}
	hdr := stream[12:]
	flags := hdr[1]
	pos := 2
	if flags&blockHeaderFlagCompressedSize != 0 {
		var err error
		if _, pos, err = readVLI(hdr, pos); err != nil {
			return 0, err
		}
	}
	if flags&blockHeaderFlagUncompressedSize == 0 {
		return 0, fmt.Errorf("block header omits the uncompressed size (flags %#02x)", flags)
	}
	n, _, err := readVLI(hdr, pos)
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// xzDecompressTo decompresses a concatenation of complete .xz streams -- which is
// exactly what a run of squashfs blocks is -- from r to w, returning how much
// plaintext came out and refusing any total other than wantLen. A negative
// wantLen accepts whatever arrives.
//
// xz decodes concatenated streams in a single pass, so a whole window costs one
// process regardless of how many blocks it spans. Neither side is ever held
// whole: the compressed bytes stream in from the source and the plaintext streams
// out to w, which is what lets the applier put a 12 MiB window in a file without
// it passing through the heap.
func xzDecompressTo(ctx context.Context, w io.Writer, r io.Reader, wantLen int) (int64, error) {
	// -T1 rather than the caller's job count, which sounds like a missed
	// opportunity and is not one. A squashfs xz block is a single-block stream
	// and a run is those streams concatenated, and xz's threaded decoder
	// parallelises blocks within a stream, not streams within its input: on a
	// 200 MiB run of 1600 such blocks, -T1 and -T8 both take 1.97s and the only
	// measurable difference is 300 KiB more resident. Decompressing a run in
	// parallel would mean splitting it here and running one xz per piece, which
	// is what NeedsBlockSizes would have to become true to allow.
	cmd, err := toolCommand(ctx, "xz", "-dc", "-q", "-T1", "-")
	if err != nil {
		return 0, err
	}
	cmd.Stdin = r
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("cannot run xz: %w", err)
	}
	n, copyErr := copyBuffer(w, stdout)
	// Closing before Wait means a failed write ends xz with EPIPE rather than
	// leaving it blocked on a pipe nobody is draining.
	stdout.Close()
	waitErr := cmd.Wait()
	if copyErr != nil {
		// A failed write to w is the real cause; xz's EPIPE is its consequence.
		return n, copyErr
	}
	if waitErr != nil {
		return n, fmt.Errorf("xz -dc failed: %w: %s", waitErr, strings.TrimSpace(stderr.String()))
	}
	if wantLen >= 0 && n != int64(wantLen) {
		return n, fmt.Errorf("decompressed %d bytes, expected %d", n, wantLen)
	}
	return n, nil
}

// xzDecompressAll is xzDecompressTo for callers that do want the plaintext in
// memory: the generator, which is going to diff or hash it, and the metadata
// region, which is under a megabyte.
func xzDecompressAll(ctx context.Context, streams []byte, wantLen int) ([]byte, error) {
	var out bytes.Buffer
	if wantLen > 0 {
		out.Grow(wantLen)
	}
	if _, err := xzDecompressTo(ctx, &out, bytes.NewReader(streams), wantLen); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
