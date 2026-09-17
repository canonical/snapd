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
