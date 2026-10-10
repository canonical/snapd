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
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// This file is the seam between the delta format and whichever compressor built
// the image it describes.
//
// Everything the format does with a block -- copy it, recompress it, decompress
// a window of it -- goes through Compressor, and which implementation serves a
// given delta is decided by one number: the compressor id in the image's own
// superblock. Nothing guesses, and nothing is configured: an image says what
// compressed it, and the applier reads that from SEC_SB before it touches a
// byte of the target.
//
// The requirement every implementation has to meet is byte-exact reproduction,
// not merely valid output. Recompressing a block has to produce the same bytes
// mksquashfs produced, or the image the applier assembles is not the target --
// which is why each implementation follows a specific squashfs-tools wrapper's
// defaults rather than the library's own, and why SEC_CANARY exists to catch a
// toolchain that has drifted.

// Compressor reproduces one squashfs compressor, in both directions.
//
// Implementations are per-delta values, not shared singletons: an lzo or zstd
// compressor owns library scratch state, and one Compressor is used by one
// caller at a time. A call may still spread its own blocks over several
// goroutines -- that is what the job count buys -- but the scratch each of
// those needs is the implementation's business, not the caller's.
type Compressor interface {
	Decompressor

	// ID is the squashfs compressor id this implementation reproduces, which
	// is what ties it back to the superblock it was derived from.
	ID() uint16

	// ToolVersion reports, as "name: version", the tool or library on this
	// machine this implementation compresses with. An empty string means it
	// could not be determined, which SEC_TOOLVER records as no line rather
	// than a failure.
	ToolVersion() string

	// CompressBlocks compresses each block of plain, delimited by uSizes,
	// independently with the given dictionary size, calling fn once per
	// block in ascending order. Blocks are independent, so callers may
	// split a large run across several calls at any block boundary.
	CompressBlocks(ctx context.Context, plain BlockPlain, uSizes []int, dictSize int, fn func(idx int, blk CompressedBlock) error) error

	// MaxBlocksPerCall bounds how many blocks one call may carry.
	MaxBlocksPerCall() int

	// SectionCodec is the delta-container codec CompressBlob produces, and
	// CompressBlob compresses a whole small blob -- the instruction stream --
	// as an ordinary stream of that codec.
	//
	// This is container-level compression with nothing to do with reproducing
	// squashfs blocks. It follows the image's compressor purely so that
	// applying a delta needs no compressor beyond the one the image already
	// requires: an lzo device should not have to carry xz for the sake of the
	// instruction section.
	SectionCodec() uint16
	CompressBlob(ctx context.Context, raw []byte) ([]byte, error)
}

// Decompressor is the reading half: a run of consecutive on-disk squashfs
// blocks turned back into plaintext.
//
// Both entry points take the run rather than a single block, because the cost
// of decompressing is dominated by per-call overhead for the process-based
// implementation and by nothing at all for the in-process ones.
type Decompressor interface {
	// NeedsBlockSizes reports whether the decompressor must be told where each
	// block in a run ends.
	//
	// It is false only for a self-delimiting format: concatenated .xz streams
	// decode in a single pass with no external framing, which is why the
	// instruction stream records nothing about the blocks inside a source
	// window. An lzo block carries no length anywhere, so a window of them is
	// undecodable without the sizes, and the encoder records them -- see
	// srcWindow.
	NeedsBlockSizes() bool

	// DecompressBlocks decompresses the len(cSizes) consecutive compressed
	// blocks concatenated in src, appending their plaintext to dst and
	// returning it together with each block's plaintext length.
	//
	// The per-block lengths are the reason this exists rather than a plain
	// "decompress it all": a metadata block's uncompressed size is recorded
	// nowhere in the image, so the walk that splits a region into blocks has
	// to learn it from the decompressor. maxUSize bounds each block's
	// plaintext and must be the block size the run came from.
	DecompressBlocks(ctx context.Context, dst, src []byte, cSizes []int, maxUSize int) ([]byte, []int, error)

	// DecompressTo streams the plaintext of consecutive compressed blocks from
	// r to w, refusing any total other than wantLen when that is non-negative.
	//
	// cSizes gives each block's on-disk length, and may be nil only when
	// NeedsBlockSizes is false. Nothing is held whole on either side, which is
	// what lets the applier put a multi-megabyte source window in a file
	// without it passing through the heap.
	DecompressTo(ctx context.Context, w io.Writer, r io.Reader, cSizes []int, maxUSize, wantLen int) (int64, error)
}

// CompressedBlock is one squashfs block after compression.
//
// Raw reports the mangle2() decision: mksquashfs stores a block uncompressed
// whenever compression did not shrink it, and then its on-disk bytes are simply
// the plaintext. OnDisk is what the block occupies in the image either way, so
// callers need not reproduce that choice.
type CompressedBlock struct {
	// OnDisk is the block's exact bytes in the image, excluding any
	// metadata-block size header: the compressed form, or the plaintext
	// itself when the block is stored raw.
	//
	// It is valid only until the callback returns. Both cases can point into a
	// reused buffer, so anything that outlives the call must copy.
	OnDisk []byte
	Raw    bool
	USize  int
}

// OnDiskLen is the number of bytes this block occupies in the image, excluding
// any metadata-block size header.
func (b CompressedBlock) OnDiskLen() int { return len(b.OnDisk) }

// BlockPlain is a run's plaintext as CompressBlocks needs to see it: a stream to
// feed the encoder, plus access to one block at a time for the two things that
// need individual blocks -- each block's checksum, where the container has one,
// and a raw-stored block's body, which is its plaintext verbatim.
//
// It is an interface so that a run's plaintext need not sit in the heap, which is
// what bounds apply memory to a block rather than to a whole run. There are
// exactly two sources: a slice on the build machine, which decompressed the run
// in order to diff it and so has the bytes anyway, and a file on the device,
// where hpatchz has just written the run and nothing else wants it.
type BlockPlain interface {
	// Len is the total plaintext length. It must equal the sum of the block
	// sizes passed alongside it.
	Len() int
	// Block returns the n bytes at off. The result is valid only until the
	// next call, because a file-backed implementation reads into a reused
	// buffer.
	Block(off, n int) ([]byte, error)
	// Stream returns the whole plaintext from the beginning, to be handed to
	// the encoder as its standard input. It may be called only once.
	Stream() (io.Reader, error)
}

// plainBytes is a run's plaintext in the heap, which is what the generator has.
type plainBytes []byte

func (p plainBytes) Len() int { return len(p) }

func (p plainBytes) Block(off, n int) ([]byte, error) {
	if off < 0 || n < 0 || off+n > len(p) {
		return nil, fmt.Errorf("block [%d,+%d) is outside %d bytes of plaintext", off, n, len(p))
	}
	return p[off : off+n], nil
}

func (p plainBytes) Stream() (io.Reader, error) { return bytes.NewReader(p), nil }

// plainFile is a run's plaintext in a file, which is what the applier has:
// hpatchz wrote it there, and leaving it there is what keeps a run's worth of
// plaintext out of the heap.
type plainFile struct {
	f   *os.File
	n   int
	buf []byte
}

func newPlainFile(f *os.File, n int) *plainFile { return &plainFile{f: f, n: n} }

func (p *plainFile) Len() int { return p.n }

func (p *plainFile) Block(off, n int) ([]byte, error) {
	if off < 0 || n < 0 || off+n > p.n {
		return nil, fmt.Errorf("block [%d,+%d) is outside %d bytes of plaintext", off, n, p.n)
	}
	if cap(p.buf) < n {
		p.buf = make([]byte, n)
	}
	b := p.buf[:n]
	if _, err := p.f.ReadAt(b, int64(off)); err != nil {
		return nil, fmt.Errorf("reading plaintext [%d,+%d): %w", off, n, err)
	}
	return b, nil
}

func (p *plainFile) Stream() (io.Reader, error) {
	// The file itself rather than a reader over it: os/exec gives an *os.File to
	// the child as its own fd 0, with no pipe and no copying goroutine, so the
	// plaintext goes from tmpfs into the encoder without passing through this
	// process at all. Block() uses ReadAt, which does not disturb the offset the
	// child is reading from.
	if _, err := p.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return p.f, nil
}

// --- section codecs ---
//
// A delta's own sections are compressed too, and which codec a section uses is
// recorded in the section table rather than derived from the image: sections are
// decoded before the target's superblock has even been parsed. Each compressor
// names the codec it writes -- see Compressor.SectionCodec -- so which of these
// a given delta uses still follows from the image it describes.
const (
	codecNone uint16 = 0
	codecXZ   uint16 = 1
	codecLZO  uint16 = 2
	codecZSTD uint16 = 3
)

// blobDecoder undoes one section codec over a whole blob.
type blobDecoder func(ctx context.Context, stored []byte, rawLen int) ([]byte, error)

// --- dispatch ---

// compressorFactories maps a squashfs compressor id to its implementation, and
// blobDecoders maps a section codec to the function that undoes it. Both are
// filled in by registerCompressor.
//
// A missing compressor id is the refusal: the format reproduces an image only if
// it can reproduce that image's compressor byte for byte, and every other id
// belongs to a compressor no snap in the store uses.
var (
	compressorFactories = map[uint16]func(jobs int) (Compressor, error){}
	blobDecoders        = map[uint16]blobDecoder{}
)

// registerCompressor is how each codec makes itself available, from its own
// init(). Keeping registration in the codec's own file is what makes the ones
// that need cgo droppable: a build without cgo simply has no lzo entry, and
// refuses lzo images rather than failing to link.
//
// A compressor and the section codec it writes are registered together because
// they have to agree: dec must decode what the compressor's CompressBlob
// produced, and codec must be what its SectionCodec reports.
func registerCompressor(id uint16, mk func(jobs int) (Compressor, error), codec uint16, dec blobDecoder) {
	compressorFactories[id] = mk
	blobDecoders[codec] = dec
}

// compressorNames are for messages only, and cover ids this format does not
// implement so that a refusal can say what it refused.
var compressorNames = map[uint16]string{
	1: "gzip",
	2: "lzma",
	3: "lzo",
	4: "xz",
	5: "lz4",
	6: "zstd",
}

func compressorName(id uint16) string {
	if n, ok := compressorNames[id]; ok {
		return n
	}
	return fmt.Sprintf("id %d", id)
}

// compressorImplemented reports whether this build can reproduce a compressor.
// It is separate from newCompressor because the geometry check runs on a
// superblock, repeatedly and long before any compression happens, and must not
// pay for -- or fail on -- setting up library state.
func compressorImplemented(id uint16) bool {
	_, ok := compressorFactories[id]
	return ok
}

// implementedCompressors lists what this build supports, for refusal messages.
func implementedCompressors() string {
	names := make([]string, 0, len(compressorFactories))
	for id := range compressorFactories {
		names = append(names, compressorName(id))
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// newCompressor builds the compressor for a squashfs compressor id. jobs is how
// many blocks it may work on at once; zero or less means every core.
//
// A failure here is a clean refusal at the front of a generate or an apply,
// which is the point of doing it up front: a library that cannot be loaded is
// found before any image work rather than partway through assembling a target.
func newCompressor(id uint16, jobs int) (Compressor, error) {
	mk, ok := compressorFactories[id]
	if !ok {
		return nil, fmt.Errorf("unsupported compressor %s, this build implements %s",
			compressorName(id), implementedCompressors())
	}
	return mk(jobs)
}

// decompressBlob undoes a section codec.
//
// It dispatches on the codec the section table records rather than on the
// image's compressor, because sections are decoded before SEC_SB is even parsed
// -- and because a codec is a property of the delta container, not of the image
// the delta describes.
func decompressBlob(ctx context.Context, codec uint16, stored []byte, rawLen int) ([]byte, error) {
	if dec, ok := blobDecoders[codec]; ok {
		return dec(ctx, stored, rawLen)
	}
	return nil, fmt.Errorf("unknown codec %d", codec)
}

// --- parallelism ---
//
// Squashfs blocks are compressed independently of one another, which is the
// property this whole format is built on -- and it makes every compressor's
// inner loop embarrassingly parallel. Nothing about the output depends on how
// the work is divided: block i produces the same bytes whichever core it lands
// on, and the results are consumed in index order regardless.
//
// What parallelism does cost is memory. Each worker holds its own encoder
// state and its own block-sized buffers, so raising the job count raises the
// resident set roughly in proportion. That is the trade the job count makes,
// and it is why the default is the machine's core count rather than something
// unbounded.

// resolveJobs turns a jobs setting into a concrete worker count. Zero or less
// is the default and means every core, which is what a build machine wants; a
// device that must stay within a memory budget passes an explicit number.
func resolveJobs(jobs int) int {
	if jobs > 0 {
		return jobs
	}
	return runtime.NumCPU()
}

// runParallel calls work(i) for every i in [0,n), spreading them over jobs
// goroutines.
//
// Callers pass one batch of at most jobs indices at a time and key their
// scratch on i, which is what makes the work reentrant: within a batch no
// index is handed out twice, so a buffer belonging to index i has exactly one
// goroutine touching it, and the batch ends before the next one reuses it.
//
// It returns the first error any call reported and stops handing out further
// indices, but it always waits for the calls already running: their scratch is
// the caller's, and returning while a goroutine still holds it would hand the
// caller a buffer being written from underneath it.
func runParallel(ctx context.Context, jobs, n int, work func(i int) error) error {
	if n <= 0 {
		return nil
	}
	if jobs > n {
		jobs = n
	}
	if jobs <= 1 {
		for i := 0; i < n; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := work(i); err != nil {
				return err
			}
		}
		return nil
	}

	var (
		mu    sync.Mutex
		next  int
		first error
		wg    sync.WaitGroup
	)
	// take hands out the next index, or reports that there is nothing left to
	// do -- either because the work is done or because something failed.
	take := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if first != nil || next >= n {
			return 0, false
		}
		i := next
		next++
		return i, true
	}
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if first == nil {
			first = err
		}
	}

	wg.Add(jobs)
	for j := 0; j < jobs; j++ {
		go func() {
			defer wg.Done()
			for {
				i, ok := take()
				if !ok {
					return
				}
				if err := ctx.Err(); err != nil {
					fail(err)
					return
				}
				if err := work(i); err != nil {
					fail(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	return first
}

// checkCompressorMatches vets a caller-supplied compressor against the image it
// is about to be used on. An override that does not match the superblock would
// produce blocks that are valid and wrong, which no later check would catch
// before the final digest.
func checkCompressorMatches(comp Compressor, id uint16) error {
	if comp.ID() != id {
		return fmt.Errorf("configured compressor is %s but the image was built with %s",
			compressorName(comp.ID()), compressorName(id))
	}
	return nil
}
