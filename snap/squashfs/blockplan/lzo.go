// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build cgo

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

/*
#cgo LDFLAGS: -ldl

#include <dlfcn.h>
#include <stddef.h>
#include <stdlib.h>

// The three entry points squashfs-tools' lzo_wrapper.c uses, declared here
// rather than pulled from lzo/lzo1x.h so that no lzo development package is
// needed to build. lzo_uint is size_t on every platform LZO builds on with the
// default configuration, which is what the library the loader finds was built
// with.
typedef size_t lzo_uint;

typedef int (*sqd_lzo_999_fn)(const unsigned char *src, lzo_uint src_len,
	unsigned char *dst, lzo_uint *dst_len, void *wrkmem,
	const unsigned char *dict, lzo_uint dict_len, void *cb, int level);
typedef int (*sqd_lzo_opt_fn)(unsigned char *src, lzo_uint src_len,
	unsigned char *dst, lzo_uint *dst_len, void *wrkmem);
typedef int (*sqd_lzo_dec_fn)(const unsigned char *src, lzo_uint src_len,
	unsigned char *dst, lzo_uint *dst_len, void *wrkmem);

static sqd_lzo_999_fn sqd_lzo_999;
static sqd_lzo_opt_fn sqd_lzo_opt;
static sqd_lzo_dec_fn sqd_lzo_dec;
typedef const char *(*sqd_lzo_version_fn)(void);
static sqd_lzo_version_fn sqd_lzo_version;

// sqd_lzo_load resolves the entry points from one library path, returning a
// dlerror string on failure and NULL on success.
static const char *sqd_lzo_load(const char *path) {
	void *h = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (h == NULL) {
		return dlerror();
	}
	sqd_lzo_999 = (sqd_lzo_999_fn)dlsym(h, "lzo1x_999_compress_level");
	sqd_lzo_opt = (sqd_lzo_opt_fn)dlsym(h, "lzo1x_optimize");
	sqd_lzo_dec = (sqd_lzo_dec_fn)dlsym(h, "lzo1x_decompress_safe");
	// The version string is diagnostic only: a library that predates it must
	// still load, so it is not part of the entry point check below.
	sqd_lzo_version = (sqd_lzo_version_fn)dlsym(h, "lzo_version_string");
	if (sqd_lzo_999 == NULL || sqd_lzo_opt == NULL || sqd_lzo_dec == NULL) {
		dlclose(h);
		return "library does not export the lzo1x entry points";
	}
	return NULL;
}

// sqd_lzo_version_str is the library's own version string, or NULL when it
// does not export one.
static const char *sqd_lzo_version_str(void) {
	if (sqd_lzo_version == NULL) {
		return NULL;
	}
	return sqd_lzo_version();
}

// Status codes, distinguishing which stage failed. The library's own error code
// comes back in lzo_err.
enum {
	SQD_LZO_OK = 0,
	SQD_LZO_COMPRESS_FAILED = 1,
	SQD_LZO_OVERFLOW = 2,
	SQD_LZO_OPTIMIZE_FAILED = 3,
	SQD_LZO_OPTIMIZE_SHORT = 4,
	SQD_LZO_DECOMPRESS_FAILED = 5,
};

// sqd_lzo_compress is lzo_wrapper.c's lzo_compress for the default
// configuration: lzo1x_999 at level 8, then the lzo1x_optimize pass over the
// result. The optimize pass is not an optional extra -- it rewrites the
// compressed stream, so skipping it produces different bytes from mksquashfs
// for the same input.
//
// ref is where optimize decompresses the stream as it goes. mksquashfs hands it
// the source buffer itself; this passes a copy instead, because here the source
// can be a read-only mapping of the image being read.
static int sqd_lzo_compress(const unsigned char *src, int src_len,
		unsigned char *dst, int dst_cap, unsigned char *ref, void *wrkmem,
		int *comp_len, int *lzo_err) {
	lzo_uint comp = 0;
	int res = sqd_lzo_999(src, (lzo_uint)src_len, dst, &comp, wrkmem, NULL, 0, NULL, 8);
	*lzo_err = res;
	if (res != 0) {
		return SQD_LZO_COMPRESS_FAILED;
	}
	if (comp > (lzo_uint)dst_cap) {
		*comp_len = (int)comp;
		return SQD_LZO_OVERFLOW;
	}
	lzo_uint orig = (lzo_uint)src_len;
	res = sqd_lzo_opt(dst, comp, ref, &orig, NULL);
	*lzo_err = res;
	if (res != 0) {
		return SQD_LZO_OPTIMIZE_FAILED;
	}
	if (orig != (lzo_uint)src_len) {
		*comp_len = (int)orig;
		return SQD_LZO_OPTIMIZE_SHORT;
	}
	*comp_len = (int)comp;
	return SQD_LZO_OK;
}

// sqd_lzo_decompress is the safe decompressor, which is what a device must use:
// the bytes come from a delta and a source image, neither of which this process
// produced, and the unsafe variant walks past the end of a truncated stream.
static int sqd_lzo_decompress(const unsigned char *src, int src_len,
		unsigned char *dst, int dst_cap, int *out_len, int *lzo_err) {
	lzo_uint out = (lzo_uint)dst_cap;
	int res = sqd_lzo_dec(src, (lzo_uint)src_len, dst, &out, NULL);
	*lzo_err = res;
	if (res != 0) {
		return SQD_LZO_DECOMPRESS_FAILED;
	}
	*out_len = (int)out;
	return SQD_LZO_OK;
}
*/
import "C"

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"unsafe"
)

// This file reproduces the lzo compressor, which mksquashfs uses when built with
// lzo support and asked for `-comp lzo`.
//
// Two things make it unlike xz. First, there is no command line tool that
// produces a squashfs lzo block: `lzop` writes its own container, so the library
// has to be called directly -- see dynlib.go for where it comes from. Second,
// an lzo block carries no length and no end marker anywhere, so a run of them
// cannot be walked without being told each block's on-disk size, which is why
// NeedsBlockSizes is true and why the instruction stream carries
// SrcWindow.CSizes for these images.
//
// What "reproduce" means here is lzo_wrapper.c's default configuration exactly:
// lzo1x_999 at level 8 followed by lzo1x_optimize. The wrapper accepts
// -Xalgorithm and -Xcompression-level, but an image built with either carries
// COMPRESSOR_OPTIONS, which checkSupportedGeometry refuses -- so the only
// configuration that reaches this code is the default one.

// compressorLzo is the squashfs compressor id of lzo.
const compressorLzo uint16 = 3

// Unlike xz, this codec is in the build only when cgo is: registering from its
// own file is what makes it droppable, and an image it cannot reproduce is
// refused rather than mis-compressed.
func init() {
	registerCompressor(compressorLzo, func(jobs int) (Compressor, error) {
		return newLZOCompressor(jobs)
	}, codecLZO, lzoDecompressBlob)
}

const (
	// lzoWorkspaceSize is LZO1X_999_MEM_COMPRESS: the scratch lzo1x_999 needs,
	// 14 * 16384 entries of two bytes. The library takes it from the caller
	// and never allocates, which is why one of these is held per compressor
	// rather than per call.
	lzoWorkspaceSize = 14 * 16384 * 2

	// lzoMaxBlocksPerCall matches xz's cap. Nothing here needs one -- there is
	// no argv to overflow -- but the cap is also what bounds the plaintext a
	// caller assembles before calling, and that is worth keeping the same
	// whichever compressor is in use.
	lzoMaxBlocksPerCall = 8192
)

// lzoLibSoname is what to dlopen. The version suffix is the soname, not a
// preference: liblzo2.so.2 is the only ABI the library has ever shipped.
const lzoLibSoname = "liblzo2.so.2"

var lzoLoad struct {
	sync.Once
	path string
	err  error
}

// loadLZO opens the library once per process.
//
// Once per process rather than once per compressor because the resolved entry
// points are process-global: dlopen returns the same object for a second
// request anyway, so there is nothing per-instance to keep.
func loadLZO() (string, error) {
	lzoLoad.Do(func() {
		var tried []string
		for _, cand := range libraryCandidates(lzoLibSoname) {
			cpath := C.CString(cand)
			msg := C.sqd_lzo_load(cpath)
			C.free(unsafe.Pointer(cpath))
			if msg == nil {
				lzoLoad.path = cand
				return
			}
			tried = append(tried, fmt.Sprintf("%s: %s", cand, C.GoString(msg)))
		}
		lzoLoad.err = fmt.Errorf("cannot load %s, needed for lzo images: %s",
			lzoLibSoname, strings.Join(tried, "; "))
	})
	return lzoLoad.path, lzoLoad.err
}

// lzoCompressBound is LZO_MAX_EXPANSION: what lzo1x_999 may write for a block it
// cannot compress at all. mksquashfs sizes its own output buffer the same way.
func lzoCompressBound(n int) int { return n + n/16 + 64 + 3 }

// lzoCompressor reproduces squashfs lzo blocks through the dlopen'd library.
//
// The four buffers are why one of these belongs to one goroutine at a time, as
// Compressor documents: they are reused across every block of every call, which
// keeps a whole-image generate from allocating per block.
// lzoWorker is the scratch one block's worth of work needs. The library
// allocates nothing of its own -- the workspace is the caller's, and so are
// both output buffers -- so a worker is the whole of what makes a compression
// or a decompression call reentrant, and a pool of them is the whole of what
// makes it parallel.
type lzoWorker struct {
	// wrk is the lzo1x_999 workspace, allocated on first use rather than with
	// the worker: it is 448 KiB and decompression has no use for it, so a pool
	// serving an applier that mostly reads would otherwise pay for it per job.
	wrk  []byte
	cbuf []byte // compressed output
	ref  []byte // lzo1x_optimize reference buffer
	ubuf []byte // decompressed output
	src  []byte // a copy of one block's plaintext, taken under the pool's lock
}

type lzoCompressor struct {
	// jobs is how many blocks may be worked on at once. Each one costs a
	// worker, and a worker costs its 448 KiB workspace plus a block-sized
	// buffer or three, which is the memory the job count buys speed with.
	jobs int
	// workers are created on demand rather than up front: the blob and
	// metadata paths compress one small thing at a time and never need more
	// than the first.
	workers []*lzoWorker
	// plainMu serialises BlockPlain access, whose contract is one call at a
	// time into a reused buffer. Workers copy the block they are given and
	// then leave the lock, so the parallel part is the compression.
	plainMu sync.Mutex
}

func newLZOCompressor(jobs int) (*lzoCompressor, error) {
	if _, err := loadLZO(); err != nil {
		return nil, err
	}
	return &lzoCompressor{jobs: resolveJobs(jobs)}, nil
}

// ensureWorkers creates the scratch for a batch of n blocks. It is called
// before the batch is handed out, never from inside it: the pool is a plain
// slice, and growing it from a goroutine that is already working would be a
// data race on the very thing that makes the work safe to parallelise.
func (l *lzoCompressor) ensureWorkers(n int) {
	for len(l.workers) < n {
		l.workers = append(l.workers, &lzoWorker{})
	}
}

// worker returns the scratch for one position in a batch.
func (l *lzoCompressor) worker(j int) *lzoWorker {
	l.ensureWorkers(j + 1)
	return l.workers[j]
}

func (l *lzoCompressor) ID() uint16 { return compressorLzo }

// ToolVersion is the version of the very library the blocks were compressed
// with, or "" when the library does not export one.
func (l *lzoCompressor) ToolVersion() string {
	if v := lzoVersion(); v != "" {
		return "lzo: " + v
	}
	return ""
}

// lzoVersion is the loaded library's own version string.
func lzoVersion() string {
	if v := C.sqd_lzo_version_str(); v != nil {
		return C.GoString(v)
	}
	return ""
}

func (l *lzoCompressor) MaxBlocksPerCall() int { return lzoMaxBlocksPerCall }

// NeedsBlockSizes is true: an lzo block ends where the next one begins and
// nothing in the stream says where that is. The image records the length in an
// inode or a metadata header, so a delta that describes a window of source
// blocks has to carry it too.
func (l *lzoCompressor) NeedsBlockSizes() bool { return true }

func (l *lzoCompressor) SectionCodec() uint16 { return codecLZO }

// compressBlock compresses one block, returning its on-disk bytes and whether
// mksquashfs would have stored it raw.
//
// The raw decision is mangle2()'s, not the wrapper's: a block whose compressed
// form is no smaller than its plaintext goes into the image verbatim. The
// wrapper's own refusal -- output longer than the block size -- is subsumed by
// it, since a block is never longer than the block size.
func (l *lzoWorker) compressBlock(src []byte) (onDisk []byte, raw bool, err error) {
	dst := grow(&l.cbuf, lzoCompressBound(len(src)))
	ref := grow(&l.ref, len(src))
	wrk := grow(&l.wrk, lzoWorkspaceSize)
	var compLen, lzoErr C.int
	st := C.sqd_lzo_compress(
		(*C.uchar)(unsafe.Pointer(&src[0])), C.int(len(src)),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.int(len(dst)),
		(*C.uchar)(unsafe.Pointer(&ref[0])), unsafe.Pointer(&wrk[0]),
		&compLen, &lzoErr)
	switch st {
	case C.SQD_LZO_OK:
	case C.SQD_LZO_COMPRESS_FAILED:
		return nil, false, fmt.Errorf("lzo1x_999_compress_level failed with %d", int(lzoErr))
	case C.SQD_LZO_OVERFLOW:
		return nil, false, fmt.Errorf("lzo1x_999 wrote %d bytes for %d, past the %d-byte bound",
			int(compLen), len(src), len(dst))
	case C.SQD_LZO_OPTIMIZE_FAILED:
		return nil, false, fmt.Errorf("lzo1x_optimize failed with %d", int(lzoErr))
	case C.SQD_LZO_OPTIMIZE_SHORT:
		return nil, false, fmt.Errorf("lzo1x_optimize round-tripped %d bytes of %d", int(compLen), len(src))
	default:
		return nil, false, fmt.Errorf("lzo compression returned unknown status %d", int(st))
	}
	if int(compLen) >= len(src) {
		return src, true, nil
	}
	return dst[:compLen], false, nil
}

func (l *lzoCompressor) CompressBlocks(ctx context.Context, plain BlockPlain, uSizes []int, dictSize int, fn func(int, CompressedBlock) error) error {
	if len(uSizes) == 0 {
		return nil
	}
	if len(uSizes) > lzoMaxBlocksPerCall {
		return fmt.Errorf("%d blocks in one call exceeds the cap of %d", len(uSizes), lzoMaxBlocksPerCall)
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
	// dictSize is ignored, and deliberately not validated against anything: lzo
	// has no dictionary and no window parameter, so a metadata block and a data
	// block of the same content compress to the same bytes. The parameter stays
	// in the interface because xz needs it.
	offs := make([]int, len(uSizes))
	off := 0
	for i, u := range uSizes {
		offs[i] = off
		off += u
	}
	// One batch of blocks is compressed at a time, then reported in order.
	// The batch is what bounds the extra memory to the job count: a block's
	// on-disk bytes are valid only until fn returns, so a whole batch's output
	// is held at once, and nothing beyond it ever is.
	type done struct {
		onDisk []byte
		raw    bool
	}
	batch := min(l.jobs, len(uSizes))
	out := make([]done, batch)
	for base := 0; base < len(uSizes); base += batch {
		n := min(batch, len(uSizes)-base)
		l.ensureWorkers(n)
		err := runParallel(ctx, n, n, func(j int) error {
			w := l.workers[j]
			i := base + j
			// BlockPlain hands out one block at a time into a buffer it
			// reuses, so the read is serialised and the block copied; what
			// runs in parallel is the compression, which is the expensive
			// part by three orders of magnitude.
			l.plainMu.Lock()
			b, err := plain.Block(offs[i], uSizes[i])
			var src []byte
			if err == nil {
				src = grow(&w.src, uSizes[i])
				copy(src, b)
			}
			l.plainMu.Unlock()
			if err != nil {
				return err
			}
			onDisk, raw, err := w.compressBlock(src)
			if err != nil {
				return fmt.Errorf("block %d: %w", i, err)
			}
			out[j] = done{onDisk: onDisk, raw: raw}
			return nil
		})
		if err != nil {
			return err
		}
		for j := 0; j < n; j++ {
			i := base + j
			if err := fn(i, CompressedBlock{OnDisk: out[j].onDisk, Raw: out[j].raw, USize: uSizes[i]}); err != nil {
				return err
			}
		}
	}
	return nil
}

// decompressBlock decompresses one block into scratch, returning the plaintext.
// It is valid until the next call.
func (l *lzoWorker) decompressBlock(src []byte, maxUSize int) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("empty compressed block")
	}
	dst := grow(&l.ubuf, maxUSize)
	var outLen, lzoErr C.int
	st := C.sqd_lzo_decompress(
		(*C.uchar)(unsafe.Pointer(&src[0])), C.int(len(src)),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.int(len(dst)),
		&outLen, &lzoErr)
	if st != C.SQD_LZO_OK {
		return nil, fmt.Errorf("lzo1x_decompress_safe failed with %d", int(lzoErr))
	}
	if int(outLen) <= 0 || int(outLen) > maxUSize {
		return nil, fmt.Errorf("block decompressed to %d bytes, outside (0,%d]", int(outLen), maxUSize)
	}
	return dst[:outLen], nil
}

func (l *lzoCompressor) DecompressBlocks(ctx context.Context, dst, src []byte, cSizes []int, maxUSize int) ([]byte, []int, error) {
	if len(cSizes) == 0 {
		if len(src) != 0 {
			return nil, nil, fmt.Errorf("%d bytes of blocks with no sizes given", len(src))
		}
		return dst, nil, nil
	}
	offs := make([]int, len(cSizes))
	off := 0
	for i, c := range cSizes {
		if c <= 0 || off+c > len(src) {
			return nil, nil, fmt.Errorf("block %d claims %d bytes with %d of %d left",
				i, c, len(src)-off, len(src))
		}
		offs[i] = off
		off += c
	}
	if off != len(src) {
		return nil, nil, fmt.Errorf("%d bytes follow the last of %d blocks", len(src)-off, len(cSizes))
	}
	// A batch at a time, for the reason CompressBlocks batches: each block's
	// plaintext lives in its worker's buffer until it has been appended.
	uSizes := make([]int, 0, len(cSizes))
	batch := min(l.jobs, len(cSizes))
	out := make([][]byte, batch)
	for base := 0; base < len(cSizes); base += batch {
		n := min(batch, len(cSizes)-base)
		l.ensureWorkers(n)
		err := runParallel(ctx, n, n, func(j int) error {
			i := base + j
			plain, err := l.workers[j].decompressBlock(src[offs[i]:offs[i]+cSizes[i]], maxUSize)
			if err != nil {
				return fmt.Errorf("block %d: %w", i, err)
			}
			out[j] = plain
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		for j := 0; j < n; j++ {
			dst = append(dst, out[j]...)
			uSizes = append(uSizes, len(out[j]))
		}
	}
	return dst, uSizes, nil
}

// DecompressTo streams a run of blocks, reading each one's on-disk bytes and
// writing its plaintext before touching the next. cSizes is required: without
// it there is nothing to say where a block ends.
func (l *lzoCompressor) DecompressTo(ctx context.Context, w io.Writer, r io.Reader, cSizes []int, maxUSize, wantLen int) (int64, error) {
	if len(cSizes) == 0 {
		return 0, fmt.Errorf("lzo needs each block's size to decompress a run")
	}
	// r is a stream, so the reads stay in order and only the decompression
	// fans out: a batch is read block by block into its workers' buffers,
	// decompressed in parallel, and written in order.
	var written int64
	batch := min(l.jobs, len(cSizes))
	out := make([][]byte, batch)
	for base := 0; base < len(cSizes); base += batch {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n := min(batch, len(cSizes)-base)
		l.ensureWorkers(n)
		for j := 0; j < n; j++ {
			i := base + j
			c := cSizes[i]
			if c <= 0 {
				return written, fmt.Errorf("block %d claims %d bytes", i, c)
			}
			stored := grow(&l.workers[j].src, c)
			if _, err := io.ReadFull(r, stored); err != nil {
				return written, fmt.Errorf("reading block %d of %d bytes: %w", i, c, err)
			}
		}
		err := runParallel(ctx, n, n, func(j int) error {
			wk := l.workers[j]
			plain, err := wk.decompressBlock(wk.src[:cSizes[base+j]], maxUSize)
			if err != nil {
				return fmt.Errorf("block %d: %w", base+j, err)
			}
			out[j] = plain
			return nil
		})
		if err != nil {
			return written, err
		}
		for j := 0; j < n; j++ {
			nw, err := w.Write(out[j])
			written += int64(nw)
			if err != nil {
				return written, err
			}
		}
	}
	if wantLen >= 0 && written != int64(wantLen) {
		return written, fmt.Errorf("run decompressed to %d bytes, expected %d", written, wantLen)
	}
	return written, nil
}

// CompressBlob compresses a whole blob as one lzo block, which is all a section
// codec needs: the section table records the raw length, so the decoder knows
// how much to expect without any framing of its own.
//
// This is container compression, unrelated to reproducing image blocks, and the
// only reason it is lzo at all is so that an lzo device needs no second
// compressor to read the instruction stream.
func (l *lzoCompressor) CompressBlob(ctx context.Context, raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("nothing to compress")
	}
	onDisk, isRaw, err := l.worker(0).compressBlock(raw)
	if err != nil {
		return nil, err
	}
	if isRaw {
		// Incompressible: hand back something no shorter than the input, which
		// is how addSectionCompressed is told to store it uncompressed.
		return append([]byte(nil), raw...), nil
	}
	return append([]byte(nil), onDisk...), nil
}

// lzoDecompressBlob undoes CompressBlob, and is what decompressBlob dispatches
// codecLZO to. It loads the library itself, because a section is decoded before
// the delta's superblock has been parsed and so before any compressor exists.
func lzoDecompressBlob(ctx context.Context, stored []byte, rawLen int) ([]byte, error) {
	// One worker: a section is one blob, decompressed once.
	l, err := newLZOCompressor(1)
	if err != nil {
		return nil, err
	}
	if rawLen <= 0 {
		return nil, fmt.Errorf("section declares %d raw bytes", rawLen)
	}
	plain, err := l.worker(0).decompressBlock(stored, rawLen)
	if err != nil {
		return nil, err
	}
	if len(plain) != rawLen {
		return nil, fmt.Errorf("section decompressed to %d bytes, expected %d", len(plain), rawLen)
	}
	return append([]byte(nil), plain...), nil
}
