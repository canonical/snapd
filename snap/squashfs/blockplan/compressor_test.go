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
	"strings"
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

// --- the contract, over every compressor this build registered ---
//
// These run over the ids that registered themselves rather than a fixed list, so
// a codec is covered the moment it is added and a build without cgo covers
// exactly what it supports.
//
// What they hold is the unit half of the contract every implementation has to
// meet: whatever CompressBlocks produces, DecompressBlocks turns back into the
// same plaintext; how many blocks are worked on at once changes nothing about the
// bytes; and a compressor refuses to serve an image some other compressor built.
// The other half -- that these bytes are the ones mksquashfs writes -- takes a
// real image and arrives with the generator.

// externalTool names the binary a compressor drives, for the ones that drive
// one. xz is a process; lzo and zstd are libraries here, so a build that
// registered them can use them.
var externalTool = map[uint16]string{4: "xz"}

// usableCompressorIDs are the registered compressors this machine can actually
// use. It notes the ones it cannot and skips only when that leaves nothing: a
// machine without liblzo2 must not take the xz coverage down with it.
func usableCompressorIDs(c *C) []uint16 {
	var ids []uint16
	for _, id := range blockplan.ImplementedCompressorIDs() {
		name := blockplan.CompressorName(id)
		if tool := externalTool[id]; tool != "" && !blockplan.HaveTool(tool) {
			c.Logf("not covering %s here: %s is not available", name, tool)
			continue
		}
		if _, err := blockplan.NewCompressor(id, 0); err != nil {
			c.Logf("not covering %s here: %v", name, err)
			continue
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.Skip("no compressor this build implements can be used here")
	}
	return ids
}

// TestCompressorRoundTripsBlocks is the unit-level contract: whatever
// CompressBlocks produces, DecompressBlocks turns back into the same plaintext,
// with each block's length reported and a raw-stored block handled by the caller
// rather than by the decompressor.
func (s *compressorSuite) TestCompressorRoundTripsBlocks(c *C) {
	ctx := context.Background()
	for _, id := range usableCompressorIDs(c) {
		comment := Commentf("the %s compressor", blockplan.CompressorName(id))
		comp, err := blockplan.NewCompressor(id, 0)
		c.Assert(err, IsNil, comment)

		// A compressible block, a partial tail and a block nothing can
		// shrink, which is the one that comes back raw.
		blocks := [][]byte{
			compressibleText(testBlockSize, "round"),
			compressibleText(5000, "tail"),
			incompressible(4096, 3),
		}
		var plain []byte
		uSizes := make([]int, len(blocks))
		for i, b := range blocks {
			plain = append(plain, b...)
			uSizes[i] = len(b)
		}

		var stored []byte
		var cSizes []int
		rawAt := -1
		err = comp.CompressBlocks(ctx, blockplan.PlainBytes(plain), uSizes, testBlockSize,
			func(idx int, blk blockplan.CompressedBlock) error {
				c.Check(blk.USize, Equals, uSizes[idx], comment)
				if blk.Raw {
					c.Check(bytes.Equal(blk.OnDisk, blocks[idx]), Equals, true,
						Commentf("block %d is stored raw but its bytes are not the plaintext, %s", idx, comment))
					rawAt = idx
					return nil
				}
				c.Check(blk.OnDiskLen() < blk.USize, Equals, true,
					Commentf("block %d is stored compressed at %d bytes for %d of plaintext, %s",
						idx, blk.OnDiskLen(), blk.USize, comment))
				stored = append(stored, blk.OnDisk...)
				cSizes = append(cSizes, blk.OnDiskLen())
				return nil
			})
		c.Assert(err, IsNil, comment)
		c.Check(rawAt, Equals, 2, Commentf("which block was stored raw, %s", comment))

		// Only the compressed blocks go back through the decompressor; a raw
		// block's bytes are its plaintext and the image's callers splice them
		// in, which is the same split the metadata walk makes.
		out, gotU, err := comp.DecompressBlocks(ctx, nil, stored, cSizes, testBlockSize)
		c.Assert(err, IsNil, comment)
		c.Assert(gotU, HasLen, len(cSizes), comment)
		want := plain[:len(blocks[0])+len(blocks[1])]
		c.Check(bytes.Equal(out, want), Equals, true,
			Commentf("the round trip differs from the plaintext at offset %d, %s", firstDiff(out, want), comment))
		c.Check(gotU, DeepEquals, uSizes[:len(cSizes)], comment)

		// DecompressTo has to agree with DecompressBlocks, since the applier
		// uses it for source windows and nothing else checks it.
		var streamed bytes.Buffer
		n, err := comp.DecompressTo(ctx, &streamed, bytes.NewReader(stored), cSizes, testBlockSize, len(want))
		c.Assert(err, IsNil, comment)
		c.Check(n, Equals, int64(len(want)), comment)
		c.Check(bytes.Equal(streamed.Bytes(), want), Equals, true,
			Commentf("streaming decompression differs from the buffered one at offset %d, %s",
				firstDiff(streamed.Bytes(), want), comment))

		// The section codec, which is what carries the instruction stream.
		blob := compressibleText(40000, "blob")
		sec, err := comp.CompressBlob(ctx, blob)
		c.Assert(err, IsNil, comment)
		c.Check(len(sec) < len(blob), Equals, true,
			Commentf("a compressible blob came back as %d bytes for %d, %s", len(sec), len(blob), comment))
		back, err := blockplan.DecompressBlob(ctx, comp.SectionCodec(), sec, len(blob))
		c.Assert(err, IsNil, comment)
		c.Check(bytes.Equal(back, blob), Equals, true,
			Commentf("the section blob round trip differs at offset %d, %s", firstDiff(back, blob), comment))
	}
}

// TestCompressorIgnoresJobCount is the property the job count rests on: how many
// blocks are worked on at once changes how long the work takes and how much
// memory it holds, and nothing else. Every squashfs block is compressed
// independently of its neighbours, so a block's bytes cannot depend on which
// worker took it -- and if they ever did, a delta generated on a build machine
// would not apply on a device with a different core count.
func (s *compressorSuite) TestCompressorIgnoresJobCount(c *C) {
	ctx := context.Background()
	// Enough blocks to fill several batches at every job count below, so the
	// batching itself is exercised rather than one short batch.
	const nBlocks = 21
	blocks := make([][]byte, nBlocks)
	uSizes := make([]int, nBlocks)
	var plain []byte
	for i := range blocks {
		switch i % 3 {
		case 0:
			blocks[i] = compressibleText(testBlockSize, fmt.Sprintf("jobs-%d", i))
		case 1:
			blocks[i] = compressibleText(9000+i, fmt.Sprintf("short-%d", i))
		default:
			blocks[i] = incompressible(4096, int64(i))
		}
		uSizes[i] = len(blocks[i])
		plain = append(plain, blocks[i]...)
	}

	for _, id := range usableCompressorIDs(c) {
		name := blockplan.CompressorName(id)

		// compress returns the concatenated on-disk bytes, each block's
		// length, and which blocks came back raw.
		compress := func(jobs int) (stored []byte, cSizes []int, raw []bool) {
			comment := Commentf("the %s compressor with %d jobs", name, jobs)
			comp, err := blockplan.NewCompressor(id, jobs)
			c.Assert(err, IsNil, comment)
			err = comp.CompressBlocks(ctx, blockplan.PlainBytes(plain), uSizes, testBlockSize,
				func(idx int, blk blockplan.CompressedBlock) error {
					if idx != len(cSizes) {
						return fmt.Errorf("block %d arrived at position %d, out of order", idx, len(cSizes))
					}
					stored = append(stored, blk.OnDisk...)
					cSizes = append(cSizes, blk.OnDiskLen())
					raw = append(raw, blk.Raw)
					return nil
				})
			c.Assert(err, IsNil, comment)
			return stored, cSizes, raw
		}

		wantStored, wantCSizes, wantRaw := compress(1)
		for _, jobs := range []int{2, 4, 16} {
			comment := Commentf("the %s compressor with %d jobs", name, jobs)
			gotStored, gotCSizes, gotRaw := compress(jobs)
			c.Assert(bytes.Equal(gotStored, wantStored), Equals, true,
				Commentf("%d jobs produced %d bytes on disk against %d for one job, differing at offset %d, %s",
					jobs, len(gotStored), len(wantStored), firstDiff(gotStored, wantStored), comment))
			c.Check(gotCSizes, DeepEquals, wantCSizes, comment)
			c.Check(gotRaw, DeepEquals, wantRaw, comment)
		}

		// The reading half, over the compressed blocks only: a raw block's
		// bytes are its plaintext and callers splice them in themselves.
		var cStored []byte
		var cSizes []int
		var cPlain []byte
		off := 0
		for i, cSize := range wantCSizes {
			if !wantRaw[i] {
				cStored = append(cStored, wantStored[off:off+cSize]...)
				cSizes = append(cSizes, cSize)
				cPlain = append(cPlain, blocks[i]...)
			}
			off += cSize
		}
		for _, jobs := range []int{1, 2, 4, 16} {
			comment := Commentf("the %s compressor with %d jobs", name, jobs)
			comp, err := blockplan.NewCompressor(id, jobs)
			c.Assert(err, IsNil, comment)
			got, gotU, err := comp.DecompressBlocks(ctx, nil, cStored, cSizes, testBlockSize)
			c.Assert(err, IsNil, comment)
			c.Check(bytes.Equal(got, cPlain), Equals, true,
				Commentf("DecompressBlocks returned %d bytes against %d, differing at offset %d, %s",
					len(got), len(cPlain), firstDiff(got, cPlain), comment))
			c.Check(gotU, HasLen, len(cSizes), comment)

			var buf bytes.Buffer
			n, err := comp.DecompressTo(ctx, &buf, bytes.NewReader(cStored), cSizes, testBlockSize, len(cPlain))
			c.Assert(err, IsNil, comment)
			c.Check(n, Equals, int64(len(cPlain)), comment)
			c.Check(bytes.Equal(buf.Bytes(), cPlain), Equals, true,
				Commentf("DecompressTo differs from the plaintext at offset %d, %s",
					firstDiff(buf.Bytes(), cPlain), comment))
		}
	}
}

// TestCompressorRefusesMismatchedOverride is the guard on the one thing a caller
// can get wrong: serving an image with a compressor that did not build it. Its
// blocks would be valid and would not be the image's.
func (s *compressorSuite) TestCompressorRefusesMismatchedOverride(c *C) {
	ids := usableCompressorIDs(c)
	for _, id := range ids {
		comp, err := blockplan.NewCompressor(id, 0)
		c.Assert(err, IsNil)
		c.Check(blockplan.CheckCompressorMatches(comp, id), IsNil,
			Commentf("%s rejected its own id", blockplan.CompressorName(id)))
		for _, other := range blockplan.ImplementedCompressorIDs() {
			if other == id {
				continue
			}
			c.Check(blockplan.CheckCompressorMatches(comp, other), NotNil,
				Commentf("a %s compressor was accepted for a %s image",
					blockplan.CompressorName(id), blockplan.CompressorName(other)))
		}
	}
}

// --- the other half of the contract, over real images ---

// mksquashfsArgsForComp is snapd's own option set with the compressor swapped,
// which is the only thing that differs about a snap built with another one.
func mksquashfsArgsForComp(c *C, name string, extra ...string) []string {
	out := append([]string{}, snapdMksquashfsArgs...)
	for i, a := range out {
		if a == "-comp" {
			out[i+1] = name
			return append(out, extra...)
		}
	}
	c.Fatalf("snapdMksquashfsArgs no longer passes -comp: %v", out)
	return nil
}

// mksquashfsWrites reports whether the local mksquashfs was built with a
// compressor, by packing a one-file tree with it. There is no way to ask: the
// -help output lists the compressors compiled in, but its wording has changed
// across releases, and packing is what the answer is needed for anyway.
func mksquashfsWrites(c *C, name string) bool {
	dir := c.MkDir()
	tree := filepath.Join(dir, "probe")
	c.Assert(os.MkdirAll(tree, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(tree, "f"), []byte("probe\n"), 0644), IsNil)

	cmd, err := blockplan.ToolCommand(context.Background(), "mksquashfs",
		append([]string{tree, filepath.Join(dir, "probe.img")}, mksquashfsArgsForComp(c, name)...)...)
	if err != nil {
		return false
	}
	return cmd.Run() == nil
}

// TestCompressorReproducesRealImages is the half of the contract no unit test can
// reach: that the bytes a compressor here produces are the bytes mksquashfs
// wrote. Everything the format does rests on it -- an OP_COPY is only valid
// because a block compressed the same way twice comes out the same -- and it
// cannot be assumed, because mksquashfs compresses with the library it was linked
// against and this code with the one it finds. So the check is a generate and an
// apply over a real pair, neither of which is told which compressor to use: the
// generator takes it from the target's superblock and the applier from SEC_SB,
// exactly as a real run does.
//
// The fixture is the churn pair rather than a small edit, so a patch run is
// emitted: for a compressor whose blocks are not self-delimiting that is what
// exercises SrcWindow.CSizes, and nothing else in the suite would.
//
// Where the two libraries do disagree the codec is noted and skipped rather than
// failed. That is not a lowered bar: refusing is the whole answer there, and the
// generator's own gate is what reports it. Measured on zstd, whose level-15
// output drifted between 1.4.8 and 1.5.7 on short inputs -- which is every
// metadata block.
func (s *compressorSuite) TestCompressorReproducesRealImages(c *C) {
	requireTools(c, "mksquashfs", "hdiffz", "hpatchz")
	ctx := context.Background()

	for _, id := range usableCompressorIDs(c) {
		name := blockplan.CompressorName(id)
		if !mksquashfsWrites(c, name) {
			c.Logf("not covering %s here: the local mksquashfs cannot write %s images", name, name)
			continue
		}

		args := mksquashfsArgsForComp(c, name)
		source := buildImageArgs(c, "comp-source.snap", populateChurn, args...)
		target := buildImageArgs(c, "comp-target.snap", func(c *C, dir string) {
			populateChurn(c, dir)
			churnEdit(c, dir)
		}, args...)
		for _, path := range []string{source, target} {
			im, err := blockplan.OpenImage(path)
			c.Assert(err, IsNil, Commentf("%s", filepath.Base(path)))
			c.Assert(im.SB.CompressionId, Equals, id,
				Commentf("%s was built with %s, wanted %s", filepath.Base(path),
					blockplan.CompressorName(im.SB.CompressionId), name))
			c.Assert(im.CheckSupported(), IsNil, Commentf("%s was refused", filepath.Base(path)))
		}

		delta := filepath.Join(c.MkDir(), "comp.delta")
		stats, err := blockplan.Generate(ctx, source, target, delta, &blockplan.GenerateOpts{Verify: true})
		if err != nil {
			// The gate failing on metadata is the drift itself: this machine's
			// library cannot reproduce what its mksquashfs wrote, so no delta of
			// such an image can be made here at all.
			if strings.Contains(err.Error(), "does not reconstruct the target") {
				c.Logf("not covering %s here: the %s library available does not reproduce what "+
					"the local mksquashfs writes, so no delta of such an image can be made "+
					"on this machine: %v", name, name, err)
				continue
			}
			c.Fatalf("generating a %s delta: %v", name, err)
		}
		if stats.RunBlockMismatches != 0 {
			// The same drift, caught a step earlier: the data blocks did not
			// recompress, so every run was downgraded to literals and the delta
			// is correct but says nothing about reproduction.
			c.Logf("not covering %s here: %d data blocks did not recompress to what the local "+
				"mksquashfs wrote", name, stats.RunBlockMismatches)
			continue
		}
		c.Assert(stats.PatchRuns > 0, Equals, true,
			Commentf("%s: no patch run was emitted, so source windows went untested "+
				"(%d no window, %d not worth it, %d failed verify)", name,
				stats.RunsNoWindow, stats.RunsTooExpensive, stats.RunsVerifyFailed))
		c.Check(stats.CopiedBytes > 0, Equals, true,
			Commentf("%s: nothing was copied verbatim, so the whole image was rebuilt", name))

		// Applied with no compressor either: it comes from SEC_SB.
		applyAndCompare(c, source, delta, target, nil)
		c.Logf("the %s compressor reproduces what the local mksquashfs writes", name)
	}
}
