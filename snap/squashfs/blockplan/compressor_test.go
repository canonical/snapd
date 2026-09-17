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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type compressorSuite struct{}

var _ = Suite(&compressorSuite{})

// stubCompressor stands in for a real codec in the tests that are about
// dispatch rather than about compression: which implementation an image's
// compressor id selects, and which decoder a section's codec selects. Only the
// blob half does anything, because that is the half dispatch reaches.
type stubCompressor struct {
	id    uint16
	codec uint16
}

var errStubBlocks = errors.New("the stub compressor does not compress blocks")

func (s *stubCompressor) ID() uint16          { return s.id }
func (s *stubCompressor) ToolVersion() string { return "stub: 1" }
func (s *stubCompressor) CompressBlocks(ctx context.Context, plain blockplan.BlockPlain, uSizes []int, dictSize int, fn func(int, blockplan.CompressedBlock) error) error {
	return errStubBlocks
}
func (s *stubCompressor) MaxBlocksPerCall() int { return 1 }
func (s *stubCompressor) SectionCodec() uint16  { return s.codec }
func (s *stubCompressor) CompressBlob(ctx context.Context, raw []byte) ([]byte, error) {
	return append([]byte("stub:"), raw...), nil
}
func (s *stubCompressor) NeedsBlockSizes() bool { return true }
func (s *stubCompressor) DecompressBlocks(ctx context.Context, dst, src []byte, cSizes []int, maxUSize int) ([]byte, []int, error) {
	return nil, nil, errStubBlocks
}
func (s *stubCompressor) DecompressTo(ctx context.Context, w io.Writer, r io.Reader, cSizes []int, maxUSize, wantLen int) (int64, error) {
	return 0, errStubBlocks
}

func stubBlobDecoder(ctx context.Context, stored []byte, rawLen int) ([]byte, error) {
	if !bytes.HasPrefix(stored, []byte("stub:")) {
		return nil, fmt.Errorf("not a stub blob")
	}
	raw := stored[len("stub:"):]
	if len(raw) != rawLen {
		return nil, fmt.Errorf("stub blob is %d bytes, expected %d", len(raw), rawLen)
	}
	return raw, nil
}

// mockStub registers a stub compressor under a compressor id and a section
// codec, and reports the jobs count it was built with.
func (s *compressorSuite) mockStub(id, codec uint16) (jobs *int, restore func()) {
	got := new(int)
	restore = blockplan.MockCompressor(id, func(jobs int) (blockplan.Compressor, error) {
		*got = jobs
		return &stubCompressor{id: id, codec: codec}, nil
	}, codec, stubBlobDecoder)
	return got, restore
}

// The compressor id of lz4, which no implementation here covers and which is
// therefore what a refusal looks like.
const idLZ4 = uint16(5)

func (s *compressorSuite) TestNewCompressorSelectsByID(c *C) {
	const id, codec = uint16(200), uint16(200)
	jobs, restore := s.mockStub(id, codec)
	defer restore()

	comp, err := blockplan.NewCompressor(id, 4)
	c.Assert(err, IsNil)
	c.Check(comp.ID(), Equals, id)
	c.Check(comp.SectionCodec(), Equals, codec)
	// The job count is the caller's memory budget, so it has to reach the
	// implementation rather than being resolved somewhere in between.
	c.Check(*jobs, Equals, 4)
}

func (s *compressorSuite) TestNewCompressorRefusesUnimplemented(c *C) {
	const id, codec = uint16(200), uint16(200)
	_, restore := s.mockStub(id, codec)
	defer restore()

	// A refusal has to name both what was asked for and what is available:
	// this is the message an operator sees when a snap cannot be delta'd on
	// their machine, and "unsupported compressor" alone does not say why.
	_, err := blockplan.NewCompressor(idLZ4, 0)
	c.Assert(err, ErrorMatches, `unsupported compressor lz4, this build implements .*`)
}

func (s *compressorSuite) TestCompressorImplemented(c *C) {
	const id, codec = uint16(200), uint16(200)
	c.Check(blockplan.CompressorImplemented(id), Equals, false)

	_, restore := s.mockStub(id, codec)
	c.Check(blockplan.CompressorImplemented(id), Equals, true)
	c.Check(blockplan.CompressorImplemented(idLZ4), Equals, false)

	// Registration is per build, so what a restore leaves behind matters to
	// every later test in the package.
	restore()
	c.Check(blockplan.CompressorImplemented(id), Equals, false)
}

func (s *compressorSuite) TestImplementedCompressorsListsRegisteredNames(c *C) {
	const codec = uint16(200)
	// id 3 is lzo, so the listing has to name it rather than print a number.
	_, restore := s.mockStub(3, codec)
	defer restore()

	c.Check(blockplan.ImplementedCompressors(), Matches, `(.*, )?lzo(, .*)?`)
}

func (s *compressorSuite) TestCompressorName(c *C) {
	for id, name := range map[uint16]string{1: "gzip", 2: "lzma", 3: "lzo", 4: "xz", 5: "lz4", 6: "zstd"} {
		c.Check(blockplan.CompressorName(id), Equals, name)
	}
	// An id from a squashfs newer than this code still has to be printable,
	// because it is exactly the case a refusal reports.
	c.Check(blockplan.CompressorName(42), Equals, "id 42")
}

func (s *compressorSuite) TestDecompressBlobDispatchesOnTheSectionCodec(c *C) {
	const id, codec = uint16(200), uint16(201)
	_, restore := s.mockStub(id, codec)
	defer restore()

	ctx := context.Background()
	comp, err := blockplan.NewCompressor(id, 0)
	c.Assert(err, IsNil)
	raw := []byte("an instruction stream, as far as this test is concerned")
	stored, err := comp.CompressBlob(ctx, raw)
	c.Assert(err, IsNil)

	// A section is decoded by the codec its table row names, which is what
	// lets sections be read before the image's own compressor is known.
	back, err := blockplan.DecompressBlob(ctx, comp.SectionCodec(), stored, len(raw))
	c.Assert(err, IsNil)
	c.Check(back, DeepEquals, raw)
}

func (s *compressorSuite) TestDecompressBlobRefusesUnknownCodec(c *C) {
	_, err := blockplan.DecompressBlob(context.Background(), 99, []byte("x"), 1)
	c.Assert(err, ErrorMatches, `unknown codec 99`)
}

func (s *compressorSuite) TestCheckCompressorMatches(c *C) {
	const id, codec = uint16(200), uint16(200)
	_, restore := s.mockStub(id, codec)
	defer restore()

	comp, err := blockplan.NewCompressor(id, 0)
	c.Assert(err, IsNil)
	c.Check(blockplan.CheckCompressorMatches(comp, id), IsNil)
	// An override that contradicts the image would produce blocks that are
	// valid and wrong, so it is refused rather than trusted.
	c.Check(blockplan.CheckCompressorMatches(comp, 4), ErrorMatches,
		`configured compressor is id 200 but the image was built with xz`)
}

func (s *compressorSuite) TestResolveJobs(c *C) {
	// Zero and negative mean "every core", which is what a build machine
	// wants and what the flagless callers pass.
	c.Check(blockplan.ResolveJobs(0), Equals, runtime.NumCPU())
	c.Check(blockplan.ResolveJobs(-1), Equals, runtime.NumCPU())
	c.Check(blockplan.ResolveJobs(3), Equals, 3)
}

func (s *compressorSuite) TestRunParallelVisitsEveryIndexOnce(c *C) {
	const n = 37
	for _, jobs := range []int{1, 2, 5, 64} {
		comment := Commentf("with %d jobs", jobs)
		var mu sync.Mutex
		seen := make([]int, n)
		err := blockplan.RunParallel(context.Background(), jobs, n, func(i int) error {
			mu.Lock()
			defer mu.Unlock()
			seen[i]++
			return nil
		})
		c.Assert(err, IsNil, comment)
		for i, times := range seen {
			// Once, not at least once: callers key their scratch buffers
			// on the index, so a repeated index is two goroutines
			// writing the same buffer.
			c.Check(times, Equals, 1, Commentf("index %d ran %d times %s", i, times, comment))
		}
	}
}

func (s *compressorSuite) TestRunParallelDoesNothingForNoWork(c *C) {
	called := false
	err := blockplan.RunParallel(context.Background(), 4, 0, func(i int) error {
		called = true
		return nil
	})
	c.Assert(err, IsNil)
	c.Check(called, Equals, false)
}

func (s *compressorSuite) TestRunParallelStopsOnError(c *C) {
	boom := errors.New("boom")
	for _, jobs := range []int{1, 4} {
		comment := Commentf("with %d jobs", jobs)
		var running, started int32
		err := blockplan.RunParallel(context.Background(), jobs, 200, func(i int) error {
			atomic.AddInt32(&running, 1)
			defer atomic.AddInt32(&running, -1)
			atomic.AddInt32(&started, 1)
			return boom
		})
		c.Check(err, Equals, boom, comment)
		// Returning while a call is still in flight would hand the caller
		// back scratch that is still being written.
		c.Check(atomic.LoadInt32(&running), Equals, int32(0), comment)
		// Every call fails here, so no worker gets as far as a second
		// index: a failure stops the work instead of being remembered
		// while the remaining 196 indices are ground through.
		c.Check(int(atomic.LoadInt32(&started)) <= jobs, Equals, true,
			Commentf("%d of 200 indices ran %s", started, comment))
	}
}

func (s *compressorSuite) TestRunParallelReportsTheFirstError(c *C) {
	early := errors.New("the first failure")
	late := errors.New("a failure further along")
	var ran [10]bool
	// One worker, so which failure comes first is fixed rather than a race:
	// what is being pinned down is that the error the caller sees is the one
	// that stopped the work.
	err := blockplan.RunParallel(context.Background(), 1, len(ran), func(i int) error {
		ran[i] = true
		switch i {
		case 3:
			return early
		case 5:
			return late
		}
		return nil
	})
	c.Check(err, Equals, early)
	c.Check(ran[3], Equals, true)
	c.Check(ran[5], Equals, false)
}

func (s *compressorSuite) TestRunParallelHonoursTheContext(c *C) {
	for _, jobs := range []int{1, 4} {
		comment := Commentf("with %d jobs", jobs)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var started int32
		err := blockplan.RunParallel(ctx, jobs, 100, func(i int) error {
			atomic.AddInt32(&started, 1)
			return nil
		})
		c.Check(err, Equals, context.Canceled, comment)
		c.Check(atomic.LoadInt32(&started), Equals, int32(0), comment)
	}
}

// --- run plaintext ---
//
// The two BlockPlain implementations are the difference between generating a
// delta and applying one: the generator has the run's plaintext in the heap
// because it decompressed it to diff it, and the applier has it in a file
// because hpatchz wrote it there and nothing else wants it. Both have to offer
// a compressor the same view of it.

func (s *compressorSuite) TestPlainBytes(c *C) {
	raw := []byte("0123456789abcdef")
	p := blockplan.PlainBytes(raw)
	c.Check(p.Len(), Equals, len(raw))

	blk, err := p.Block(4, 6)
	c.Assert(err, IsNil)
	c.Check(string(blk), Equals, "456789")

	// The whole plaintext, from the beginning, is what an encoder is handed
	// on its standard input.
	r, err := p.Stream()
	c.Assert(err, IsNil)
	all, err := io.ReadAll(r)
	c.Assert(err, IsNil)
	c.Check(all, DeepEquals, raw)

	for _, t := range []struct{ off, n int }{{-1, 4}, {4, -1}, {12, 8}, {len(raw) + 1, 1}} {
		_, err := p.Block(t.off, t.n)
		c.Check(err, ErrorMatches, `block \[-?\d+,\+-?\d+\) is outside 16 bytes of plaintext`,
			Commentf("Block(%d, %d)", t.off, t.n))
	}
}

func (s *compressorSuite) TestPlainFile(c *C) {
	raw := []byte("0123456789abcdef")
	path := filepath.Join(c.MkDir(), "run")
	c.Assert(os.WriteFile(path, raw, 0644), IsNil)
	f, err := os.Open(path)
	c.Assert(err, IsNil)
	defer f.Close()

	p := blockplan.NewPlainFile(f, len(raw))
	c.Check(p.Len(), Equals, len(raw))

	blk, err := p.Block(4, 6)
	c.Assert(err, IsNil)
	c.Check(string(blk), Equals, "456789")
	// A shorter block after a longer one must not show the tail of the
	// previous read, which a reused buffer would.
	blk, err = p.Block(0, 3)
	c.Assert(err, IsNil)
	c.Check(string(blk), Equals, "012")

	// Reading blocks must not disturb where the encoder's stream starts,
	// because the applier does both to the same file.
	r, err := p.Stream()
	c.Assert(err, IsNil)
	all, err := io.ReadAll(r)
	c.Assert(err, IsNil)
	c.Check(all, DeepEquals, raw)

	for _, t := range []struct{ off, n int }{{-1, 4}, {4, -1}, {12, 8}, {len(raw) + 1, 1}} {
		_, err := p.Block(t.off, t.n)
		c.Check(err, ErrorMatches, `block \[-?\d+,\+-?\d+\) is outside 16 bytes of plaintext`,
			Commentf("Block(%d, %d)", t.off, t.n))
	}
}
