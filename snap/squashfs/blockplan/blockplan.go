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
