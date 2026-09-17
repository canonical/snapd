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
	"context"
	"os/exec"
	"sort"
)

// This file exports what the tests need to reach inside the package. The tests
// themselves live in package blockplan_test and drive the code the way the rest
// of snapd does, but most of this package is unexported on purpose -- the format
// is one API, not thirty -- and the parts that carry the risk are the internal
// ones: the block walks, the instruction encoding, the run costing.

// --- compressor.go ---

type BlobDecoder = blobDecoder

var (
	NewCompressor          = newCompressor
	CompressorImplemented  = compressorImplemented
	ImplementedCompressors = implementedCompressors
	CompressorName         = compressorName
	DecompressBlob         = decompressBlob
	CheckCompressorMatches = checkCompressorMatches
	ResolveJobs            = resolveJobs
	RunParallel            = runParallel
	NewPlainFile           = newPlainFile
)

type PlainBytes = plainBytes

// ImplementedCompressorIDs lists the ids this build registered, in a stable
// order so that a failure names the same compressor run to run. It is how the
// contract tests cover every codec a build has rather than a fixed list.
func ImplementedCompressorIDs() []uint16 {
	ids := make([]uint16, 0, len(compressorFactories))
	for id := range compressorFactories {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// MockCompressor registers a compressor and the section codec that goes with it
// for the duration of a test, so that the dispatch and refusal paths can be
// driven without depending on which codecs this build happens to include.
func MockCompressor(id uint16, mk func(jobs int) (Compressor, error), codec uint16, dec BlobDecoder) (restore func()) {
	oldMk, hadMk := compressorFactories[id]
	oldDec, hadDec := blobDecoders[codec]
	registerCompressor(id, mk, codec, dec)
	return func() {
		if hadMk {
			compressorFactories[id] = oldMk
		} else {
			delete(compressorFactories, id)
		}
		if hadDec {
			blobDecoders[codec] = oldDec
		} else {
			delete(blobDecoders, codec)
		}
	}
}

// --- tool.go ---

var (
	ToolCommand = toolCommand
	HaveTool    = haveTool
)

func MockCommandFromSystemSnapWithContext(f func(ctx context.Context, name string, args ...string) (*exec.Cmd, error)) (restore func()) {
	old := snapdtoolCommandFromSystemSnapWithContext
	snapdtoolCommandFromSystemSnapWithContext = f
	return func() {
		snapdtoolCommandFromSystemSnapWithContext = old
	}
}

// --- xz.go ---

type XZBlockSplitter = xzBlockSplitter

var (
	NewXZBlockSplitter = newXZBlockSplitter
	AppendXZFrame      = appendXZFrame
	XZDecompressAll    = xzDecompressAll
	XZBlockHeaderSize  = blockHeaderSize
)

// Next walks to the following block, as CompressBlocks does over a running xz.
func (s *xzBlockSplitter) Next() (payload []byte, uSize int, err error) {
	return s.next()
}

// --- memfd.go ---

type MemFile = memFile

var (
	NewMemFile     = newMemFile
	NewMemFileWith = newMemFileWith
	NewDiskFile    = newDiskFile
)

// OnDisk reports which backing a scratch file got, which is the one thing about
// it a caller never needs to know and a test must.
func (m *memFile) OnDisk() bool { return m.onDisk }
