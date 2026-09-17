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

// Package blockplan generates and applies snap-2-1-hdiffz deltas, which
// describe a target squashfs image as a plan over the source image's own
// compressed blocks.
//
// The snap-1-1-xdelta3 format diffs the two images' uncompressed contents and
// rebuilds the target by running mksquashfs over the result, so a device
// applying it recompresses the entire snap however little of it changed. That
// was measured at 48 to 74 seconds of CPU and around 213 MiB resident for a
// snapcraft-sized snap, which is what this format exists to avoid.
//
// Here the source image is treated as a store of blocks that are already
// compressed. A block whose plaintext did not change is copied out of the
// source verbatim, still compressed; only the blocks that did change are
// compressed again, from plaintext that hdiffz reconstructs a few megabytes at
// a time. What an apply costs then follows from how much of the image changed
// rather than from how large it is, and its scratch memory is bounded by a
// constant rather than by the largest file in the image. On the same pairs the
// delta is smaller too, because a block-level plan spends nothing on the parts
// of the image it copies.
//
// The requirement all of this rests on is byte-exact reproduction: a block this
// package compresses has to come out exactly as mksquashfs wrote it, or the
// assembled image is not the target. That is why each compressor reproduces a
// specific squashfs-tools wrapper's settings rather than its library's defaults,
// why every delta carries a compressor self-check made on the machine that
// built it, and why the target is verified against a digest before it is
// accepted.
//
// Anything this package cannot reproduce exactly, it refuses: an image built
// with a compressor no implementation here covers, a superblock whose geometry
// it does not recognise, a patch tool that is not installed. A refusal is not a
// failed refresh -- the caller falls back to the format below it, and in the
// end to downloading the snap whole.
package blockplan

import (
	"fmt"
	"io"
	"sync"
)

const (
	// Format is the name this delta format goes by in the store and in the
	// snap delta command line. It follows the compatibility label
	// convention the snap-1-1-xdelta3 format established: the second
	// generation of the snap delta format, patched with the same
	// hdiffz/hpatchz pair the first generation used.
	Format = "snap-2-1-hdiffz"

	// Magic is the first four bytes of a delta in this format, "sqbp" in
	// ASCII. It is distinct from the pseudo-file format's "sqpf" so that
	// dispatching on a delta file's magic simply gains a case.
	Magic = uint32(0x70627173)
)

// --- options and statistics ---
//
// The package's whole public surface, apart from Generate and Apply themselves.
// It lives here rather than beside the code that reads it because both option
// types are what a caller has to understand to drive the format at all, and both
// statistics types are what it needs to report the cost this format exists to
// reduce.

// ApplyOpts carries what an apply needs beyond the three streams. A nil
// *ApplyOpts is the same as a zero one, which is what a device passes.
type ApplyOpts struct {
	// Comp overrides the compressor. Nil is the normal case and derives it
	// from the target superblock the delta carries in SEC_SB, so a device is
	// never told what compressed the image it is assembling; a non-nil one
	// must agree with that superblock.
	Comp Compressor

	// Jobs is how many blocks the derived compressor may work on at once.
	// Zero or less means every core. Raising it raises peak memory as well
	// as speed, because every worker holds its own encoder state and
	// buffers.
	Jobs int

	// MaxRunUSize is the largest patch run this applier is willing to
	// accept. It is how a device with a memory budget refuses a delta up
	// front rather than discovering the cost partway through assembling an
	// image -- falling back to a full download is a far better outcome than
	// failing late.
	//
	// The delta header's own cap is what the instruction decoder bounds
	// every run and window against, so capping the header is enough to cap
	// the work. Zero accepts whatever the delta asks for.
	MaxRunUSize int

	// SkipSourceDigest omits hashing the source. The digest is what makes
	// every OP_COPY safe, so it is only skipped by callers that already know
	// the source is right -- the generator's own final gate.
	SkipSourceDigest bool
}

// ApplyStats records what an apply actually did. UCompressedBytes is the number
// that justifies the format: it is the plaintext the device had to push through
// the compressor, against the whole image in snap-1-1-xdelta3.
type ApplyStats struct {
	Instructions int
	Copies       int
	Literals     int
	PatchRuns    int

	CopiedBytes  int64
	LiteralBytes int64
	PatchedBytes int64

	// BlocksCompressed and UCompressedBytes count the compression work: data
	// blocks from patch runs plus every metadata block.
	BlocksCompressed int
	UCompressedBytes int64

	// WindowUBytes is the source plaintext read back to feed the patch runs.
	// It is the format's only data-region decompression, and it is bounded
	// by what changed rather than by the size of the image.
	WindowUBytes int64

	// MetaBlocks and MetaUBytes break out the metadata region, which is
	// recompressed unconditionally and so is the format's CPU floor.
	MetaBlocks int
	MetaUBytes int64

	// PeakScratchBytes is the most one patch run held in its three scratch
	// files at once: the source window, the patch, and the reconstructed
	// plaintext.
	//
	// It is reported separately from resident memory because those files are
	// memfds. That keeps them out of the heap and out of the resident set,
	// but tmpfs is still RAM, so this is the other half of an apply's memory
	// demand -- and the half MaxRunUSize bounds directly.
	PeakScratchBytes int64
}

// --- shared helpers ---
//
// go.mod is go 1.18, so min and max are not builtins yet and neither is
// binary.LittleEndian.AppendUint32. The package carries its own, as several
// others in the tree do.

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// appendUint32 appends v to b, little end first, which is the byte order both
// squashfs and the .xz container use throughout.
func appendUint32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// copyBufferSize is the buffer a stream copy uses. Generating and applying a
// delta moves whole multi-megabyte windows between processes and files, where
// io.Copy's own 32 KiB buffer costs a syscall pair per 32 KiB of window.
const copyBufferSize = 1 << 20

// ioBufPool keeps those buffers out of the collector's way: an apply makes at
// least one copy per instruction, and a megabyte of fresh garbage per
// instruction would dominate the memory this format exists to save.
var ioBufPool = sync.Pool{
	New: func() any {
		return make([]byte, copyBufferSize)
	},
}

func copyBuffer(dst io.Writer, src io.Reader) (int64, error) {
	buf := ioBufPool.Get().([]byte)
	defer ioBufPool.Put(buf)
	return io.CopyBuffer(dst, src, buf)
}

// copyNBuffer is copyBuffer for the many places that copy a length the delta
// states: a copied extent, a literal, a patch. The count is returned so that a
// short stream is reported as the delta having promised more than it carried,
// rather than as a truncated image.
func copyNBuffer(dst io.Writer, src io.Reader, n int64) (int64, error) {
	buf := ioBufPool.Get().([]byte)
	defer ioBufPool.Put(buf)
	return io.CopyBuffer(dst, io.LimitReader(src, n), buf)
}

// humanBytes formats a byte count for the messages this format reports sizes in.
// A delta refused for asking more memory than the caller allows has to say what
// it would have cost in the units that budget is set in.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
