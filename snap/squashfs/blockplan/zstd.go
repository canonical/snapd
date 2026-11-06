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

// The stable zstd entry points squashfs-tools' zstd_wrapper.c uses, plus the
// context pair on each side so that a run of blocks does not allocate a context
// per block. Declared here rather than pulled from zstd.h so that no zstd
// development package is needed to build.
typedef void *(*sqd_zstd_new_fn)(void);
typedef size_t (*sqd_zstd_freectx_fn)(void *ctx);
typedef size_t (*sqd_zstd_compress_fn)(void *cctx, void *dst, size_t dst_cap,
	const void *src, size_t src_len, int level);
typedef size_t (*sqd_zstd_decompress_fn)(void *dctx, void *dst, size_t dst_cap,
	const void *src, size_t src_len);
typedef unsigned (*sqd_zstd_iserr_fn)(size_t code);
typedef const char *(*sqd_zstd_errname_fn)(size_t code);
typedef size_t (*sqd_zstd_bound_fn)(size_t src_len);

static sqd_zstd_new_fn sqd_zstd_new_cctx;
static sqd_zstd_freectx_fn sqd_zstd_free_cctx;
static sqd_zstd_compress_fn sqd_zstd_compress_cctx;
static sqd_zstd_new_fn sqd_zstd_new_dctx;
static sqd_zstd_freectx_fn sqd_zstd_free_dctx;
static sqd_zstd_decompress_fn sqd_zstd_decompress_dctx;
static sqd_zstd_iserr_fn sqd_zstd_is_error;
static sqd_zstd_errname_fn sqd_zstd_error_name;
static sqd_zstd_bound_fn sqd_zstd_compress_bound;
typedef const char *(*sqd_zstd_version_fn)(void);
static sqd_zstd_version_fn sqd_zstd_version;

// sqd_zstd_load resolves the entry points from one library path, returning a
// dlerror string on failure and NULL on success.
static const char *sqd_zstd_load(const char *path) {
	void *h = dlopen(path, RTLD_NOW | RTLD_LOCAL);
	if (h == NULL) {
		return dlerror();
	}
	sqd_zstd_new_cctx = (sqd_zstd_new_fn)dlsym(h, "ZSTD_createCCtx");
	sqd_zstd_free_cctx = (sqd_zstd_freectx_fn)dlsym(h, "ZSTD_freeCCtx");
	sqd_zstd_compress_cctx = (sqd_zstd_compress_fn)dlsym(h, "ZSTD_compressCCtx");
	sqd_zstd_new_dctx = (sqd_zstd_new_fn)dlsym(h, "ZSTD_createDCtx");
	sqd_zstd_free_dctx = (sqd_zstd_freectx_fn)dlsym(h, "ZSTD_freeDCtx");
	sqd_zstd_decompress_dctx = (sqd_zstd_decompress_fn)dlsym(h, "ZSTD_decompressDCtx");
	sqd_zstd_is_error = (sqd_zstd_iserr_fn)dlsym(h, "ZSTD_isError");
	sqd_zstd_error_name = (sqd_zstd_errname_fn)dlsym(h, "ZSTD_getErrorName");
	sqd_zstd_compress_bound = (sqd_zstd_bound_fn)dlsym(h, "ZSTD_compressBound");
	// The version string is diagnostic only: a library that predates it must
	// still load, so it is not part of the entry point check below.
	sqd_zstd_version = (sqd_zstd_version_fn)dlsym(h, "ZSTD_versionString");
	if (sqd_zstd_new_cctx == NULL || sqd_zstd_free_cctx == NULL ||
			sqd_zstd_compress_cctx == NULL || sqd_zstd_new_dctx == NULL ||
			sqd_zstd_free_dctx == NULL || sqd_zstd_decompress_dctx == NULL ||
			sqd_zstd_is_error == NULL || sqd_zstd_error_name == NULL ||
			sqd_zstd_compress_bound == NULL) {
		dlclose(h);
		return "library does not export the ZSTD entry points";
	}
	return NULL;
}

// sqd_zstd_version_str is the library's own version string, or NULL when it
// does not export one.
static const char *sqd_zstd_version_str(void) {
	if (sqd_zstd_version == NULL) {
		return NULL;
	}
	return sqd_zstd_version();
}

static void *sqd_zstd_cctx_new(void) { return sqd_zstd_new_cctx(); }
static void sqd_zstd_cctx_free(void *c) { sqd_zstd_free_cctx(c); }
static void *sqd_zstd_dctx_new(void) { return sqd_zstd_new_dctx(); }
static void sqd_zstd_dctx_free(void *c) { sqd_zstd_free_dctx(c); }

static size_t sqd_zstd_bound(size_t n) { return sqd_zstd_compress_bound(n); }

// sqd_zstd_compress is zstd_wrapper.c's zstd_compress: one ZSTD_compressCCtx
// with the block size as the destination capacity.
//
// Any error is reported as a zero length, which is what the wrapper does and
// what makes mksquashfs store the block raw. The distinction the wrapper cannot
// make either -- "would not fit" against a real failure -- needs zstd's
// experimental error-code API, and guessing differently here would put a
// compressed block where the image has a raw one.
static int sqd_zstd_compress(void *cctx, const unsigned char *src, int src_len,
		unsigned char *dst, int dst_cap, int level, const char **errname) {
	size_t res = sqd_zstd_compress_cctx(cctx, dst, (size_t)dst_cap, src, (size_t)src_len, level);
	if (sqd_zstd_is_error(res)) {
		*errname = sqd_zstd_error_name(res);
		return 0;
	}
	return (int)res;
}

// sqd_zstd_decompress returns the plaintext length, or -1 with errname set.
static int sqd_zstd_decompress(void *dctx, const unsigned char *src, int src_len,
		unsigned char *dst, int dst_cap, const char **errname) {
	size_t res = sqd_zstd_decompress_dctx(dctx, dst, (size_t)dst_cap, src, (size_t)src_len);
	if (sqd_zstd_is_error(res)) {
		*errname = sqd_zstd_error_name(res);
		return -1;
	}
	return (int)res;
}
*/
import "C"

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// This file reproduces the zstd compressor, which mksquashfs uses for `-comp
// zstd`. It follows lzoblock.go in shape -- a dlopen'd library, see dynlib.go --
// and differs in what it has to get right.
//
// The one thing that matters is zstd_wrapper.c's parameters: a single
// ZSTD_compressCCtx at level 15 with the block size, not the compressed bound,
// as the destination capacity. That capacity is not a detail: a block that
// compresses to more than the block size comes back as an error, which the
// wrapper turns into a raw store, so allowing more room would put a compressed
// block where the image has a verbatim one. -Xcompression-level cannot reach
// this code, since such an image carries COMPRESSOR_OPTIONS and is refused.
//
// zstd frames do record their content size, so a run of them could in principle
// be walked without being told each block's length, as xz's are. That is not
// done: it needs the streaming decode API to find the boundaries while reading,
// and the saving is the window sizes in the instruction stream. NeedsBlockSizes
// is true, like lzo.

// compressorZstd is the squashfs compressor id of zstd.
const compressorZstd uint16 = 6

// Registered from this file for the reason lzo is: the codec exists only where
// cgo does, and an image it cannot reproduce is refused rather than
// mis-compressed.
func init() {
	registerCompressor(compressorZstd, func(jobs int) (Compressor, error) {
		return newZstdCompressor(jobs)
	}, codecZSTD, zstdDecompressBlob)
}

const (
	// zstdBlockLevel is zstd_wrapper.c's default compression level, and the
	// only one an image without COMPRESSOR_OPTIONS can have been built at.
	zstdBlockLevel = 15

	// zstdBlobLevel compresses the instruction section, which is not a
	// squashfs block and so has no level to reproduce -- only to be read back.
	// It is compressed once on the build machine and decompressed on every
	// device, which is the trade that argues for the maximum.
	zstdBlobLevel = 19

	// zstdMaxBlocksPerCall matches the other implementations; see
	// lzoMaxBlocksPerCall for why the cap is kept the same.
	zstdMaxBlocksPerCall = 8192
)

// zstdLibSoname is what to dlopen. libzstd has carried this soname across the
// whole 1.x series, and SEC_CANARY is what watches the version behind it for
// output drift.
const zstdLibSoname = "libzstd.so.1"

var zstdLoad struct {
	sync.Once
	path string
	err  error
}

// loadZstd opens the library once per process, for the reason given in loadLZO.
func loadZstd() (string, error) {
	zstdLoad.Do(func() {
		var tried []string
		for _, cand := range libraryCandidates(zstdLibSoname) {
			cpath := C.CString(cand)
			msg := C.sqd_zstd_load(cpath)
			C.free(unsafe.Pointer(cpath))
			if msg == nil {
				zstdLoad.path = cand
				return
			}
			tried = append(tried, fmt.Sprintf("%s: %s", cand, C.GoString(msg)))
		}
		zstdLoad.err = fmt.Errorf("cannot load %s, needed for zstd images: %s",
			zstdLibSoname, strings.Join(tried, "; "))
	})
	return zstdLoad.path, zstdLoad.err
}

// zstdCompressor reproduces squashfs zstd blocks through the dlopen'd library.
//
// The two contexts are what make a run cheap: zstd's one-shot calls would each
// allocate and free their own working state, which at level 15 is megabytes per
// block. They are reset by every call, so reusing them does not change a byte of
// output.
// zstdWorker is one block's worth of working state: a compression context, a
// decompression context and the two buffers they write into. The contexts are
// what make a run cheap and are also what stops one from serving two blocks at
// once, so a worker is the unit the job count multiplies.
type zstdWorker struct {
	cctx unsafe.Pointer
	dctx unsafe.Pointer
	cbuf []byte // compressed output
	ubuf []byte // decompressed output
	src  []byte // a copy of one block's plaintext, taken under the pool's lock
}

type zstdCompressor struct {
	// jobs is how many blocks may be worked on at once. Each costs a worker,
	// and a worker costs a pair of library contexts -- megabytes at level 15 --
	// plus its buffers, which is the memory the job count buys speed with.
	jobs int
	// workers are created on demand: the blob and metadata paths compress one
	// small thing at a time and never need more than the first.
	workers []*zstdWorker
	// plainMu serialises BlockPlain access, whose contract is one call at a
	// time into a reused buffer. Workers copy the block they are given and
	// then leave the lock, so the parallel part is the compression.
	plainMu sync.Mutex
}

func newZstdCompressor(jobs int) (*zstdCompressor, error) {
	if _, err := loadZstd(); err != nil {
		return nil, err
	}
	return &zstdCompressor{jobs: resolveJobs(jobs)}, nil
}

// newZstdWorker allocates one worker's contexts.
func newZstdWorker() (*zstdWorker, error) {
	w := &zstdWorker{
		cctx: C.sqd_zstd_cctx_new(),
		dctx: C.sqd_zstd_dctx_new(),
	}
	if w.cctx == nil || w.dctx == nil {
		w.free()
		return nil, fmt.Errorf("zstd could not allocate its compression contexts")
	}
	// The contexts are library allocations, so they outlive Go's reach: a
	// cleanup frees them when the worker becomes unreachable. There is no
	// Close in the Compressor interface because only the two library-backed
	// implementations have anything to close, and a generate holds one
	// compressor for the whole run.
	runtime.AddCleanup(w, func(ctxs [2]unsafe.Pointer) {
		if ctxs[0] != nil {
			C.sqd_zstd_cctx_free(ctxs[0])
		}
		if ctxs[1] != nil {
			C.sqd_zstd_dctx_free(ctxs[1])
		}
	}, [2]unsafe.Pointer{w.cctx, w.dctx})
	return w, nil
}

// free releases the contexts on the one path that has no cleanup registered yet.
func (w *zstdWorker) free() {
	if w.cctx != nil {
		C.sqd_zstd_cctx_free(w.cctx)
		w.cctx = nil
	}
	if w.dctx != nil {
		C.sqd_zstd_dctx_free(w.dctx)
		w.dctx = nil
	}
}

// ensureWorkers creates the contexts for a batch of n blocks. It is called
// before the batch is handed out, never from inside it: the pool is a plain
// slice, and growing it from a goroutine that is already working would be a
// data race on the very thing that makes the work safe to parallelise.
func (z *zstdCompressor) ensureWorkers(n int) error {
	for len(z.workers) < n {
		w, err := newZstdWorker()
		if err != nil {
			return err
		}
		z.workers = append(z.workers, w)
	}
	return nil
}

// worker returns the state for one position in a batch.
func (z *zstdCompressor) worker(j int) (*zstdWorker, error) {
	if err := z.ensureWorkers(j + 1); err != nil {
		return nil, err
	}
	return z.workers[j], nil
}

func (z *zstdCompressor) ID() uint16 { return compressorZstd }

// ToolVersion is the version of the very library the blocks were compressed
// with, or "" when the library does not export one.
func (z *zstdCompressor) ToolVersion() string {
	if v := zstdVersion(); v != "" {
		return "zstd: " + v
	}
	return ""
}

// zstdVersion is the loaded library's own version string.
func zstdVersion() string {
	if v := C.sqd_zstd_version_str(); v != nil {
		return C.GoString(v)
	}
	return ""
}

func (z *zstdCompressor) MaxBlocksPerCall() int { return zstdMaxBlocksPerCall }

// NeedsBlockSizes is true -- see the note at the top of this file: a zstd frame
// does carry its own sizes, but finding them while streaming needs an API this
// does not use.
func (z *zstdCompressor) NeedsBlockSizes() bool { return true }

func (z *zstdCompressor) SectionCodec() uint16 { return codecZSTD }

// compressBlock compresses one block at the wrapper's level and capacity,
// returning its on-disk bytes and whether mksquashfs would store it raw.
//
// dstCap is the block size the image was built with, not the compressed bound,
// because that is the capacity zstd_wrapper.c passes and the raw-store decision
// depends on it.
func (w *zstdWorker) compressBlock(src []byte, dstCap int) (onDisk []byte, raw bool, err error) {
	if dstCap <= 0 {
		return nil, false, fmt.Errorf("block size %d is not a destination capacity", dstCap)
	}
	dst := grow(&w.cbuf, dstCap)
	var errName *C.char
	n := int(C.sqd_zstd_compress(w.cctx,
		(*C.uchar)(unsafe.Pointer(&src[0])), C.int(len(src)),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.int(dstCap),
		C.int(zstdBlockLevel), &errName))
	// Zero is the wrapper's verdict that the block does not compress usefully,
	// whatever the reason; mangle2() then stores the plaintext.
	if n <= 0 || n >= len(src) {
		return src, true, nil
	}
	return dst[:n], false, nil
}

func (z *zstdCompressor) CompressBlocks(ctx context.Context, plain BlockPlain, uSizes []int, dictSize int, fn func(int, CompressedBlock) error) error {
	if len(uSizes) == 0 {
		return nil
	}
	if len(uSizes) > zstdMaxBlocksPerCall {
		return fmt.Errorf("%d blocks in one call exceeds the cap of %d", len(uSizes), zstdMaxBlocksPerCall)
	}
	total := 0
	for i, u := range uSizes {
		if u <= 0 {
			return fmt.Errorf("block %d has non-positive size %d", i, u)
		}
		if u > dictSize {
			return fmt.Errorf("block %d is %d bytes, over the %d-byte block size", i, u, dictSize)
		}
		total += u
	}
	if total != plain.Len() {
		return fmt.Errorf("block sizes sum to %d but %d bytes of plaintext were given", total, plain.Len())
	}
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
	batch := min(z.jobs, len(uSizes))
	out := make([]done, batch)
	for base := 0; base < len(uSizes); base += batch {
		n := min(batch, len(uSizes)-base)
		if err := z.ensureWorkers(n); err != nil {
			return err
		}
		err := runParallel(ctx, n, n, func(j int) error {
			w := z.workers[j]
			i := base + j
			// BlockPlain hands out one block at a time into a buffer it
			// reuses, so the read is serialised and the block copied; what
			// runs in parallel is the compression, which at level 15 is the
			// expensive part by three orders of magnitude.
			z.plainMu.Lock()
			b, err := plain.Block(offs[i], uSizes[i])
			var src []byte
			if err == nil {
				src = grow(&w.src, uSizes[i])
				copy(src, b)
			}
			z.plainMu.Unlock()
			if err != nil {
				return err
			}
			onDisk, raw, err := w.compressBlock(src, dictSize)
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
func (w *zstdWorker) decompressBlock(src []byte, maxUSize int) ([]byte, error) {
	if len(src) == 0 {
		return nil, fmt.Errorf("empty compressed block")
	}
	dst := grow(&w.ubuf, maxUSize)
	var errName *C.char
	n := int(C.sqd_zstd_decompress(w.dctx,
		(*C.uchar)(unsafe.Pointer(&src[0])), C.int(len(src)),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.int(maxUSize), &errName))
	if n < 0 {
		return nil, fmt.Errorf("ZSTD_decompressDCtx failed: %s", C.GoString(errName))
	}
	if n == 0 || n > maxUSize {
		return nil, fmt.Errorf("block decompressed to %d bytes, outside (0,%d]", n, maxUSize)
	}
	return dst[:n], nil
}

func (z *zstdCompressor) DecompressBlocks(ctx context.Context, dst, src []byte, cSizes []int, maxUSize int) ([]byte, []int, error) {
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
	batch := min(z.jobs, len(cSizes))
	out := make([][]byte, batch)
	for base := 0; base < len(cSizes); base += batch {
		n := min(batch, len(cSizes)-base)
		if err := z.ensureWorkers(n); err != nil {
			return nil, nil, err
		}
		err := runParallel(ctx, n, n, func(j int) error {
			i := base + j
			plain, err := z.workers[j].decompressBlock(src[offs[i]:offs[i]+cSizes[i]], maxUSize)
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

// DecompressTo streams a run of blocks one frame at a time. cSizes is required,
// for the reason NeedsBlockSizes gives.
func (z *zstdCompressor) DecompressTo(ctx context.Context, w io.Writer, r io.Reader, cSizes []int, maxUSize, wantLen int) (int64, error) {
	if len(cSizes) == 0 {
		return 0, fmt.Errorf("zstd needs each block's size to decompress a run")
	}
	// r is a stream, so the reads stay in order and only the decompression
	// fans out: a batch is read block by block into its workers' buffers,
	// decompressed in parallel, and written in order.
	var written int64
	batch := min(z.jobs, len(cSizes))
	out := make([][]byte, batch)
	for base := 0; base < len(cSizes); base += batch {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n := min(batch, len(cSizes)-base)
		if err := z.ensureWorkers(n); err != nil {
			return written, err
		}
		for j := 0; j < n; j++ {
			i := base + j
			c := cSizes[i]
			if c <= 0 {
				return written, fmt.Errorf("block %d claims %d bytes", i, c)
			}
			stored := grow(&z.workers[j].src, c)
			if _, err := io.ReadFull(r, stored); err != nil {
				return written, fmt.Errorf("reading block %d of %d bytes: %w", i, c, err)
			}
		}
		err := runParallel(ctx, n, n, func(j int) error {
			wk := z.workers[j]
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

// CompressBlob compresses a whole blob as one frame, with the compressed bound
// as its capacity rather than a block size: this is container compression, not a
// squashfs block, so there is no raw-store rule to reproduce and no reason to
// refuse a blob that happens not to shrink.
func (z *zstdCompressor) CompressBlob(ctx context.Context, raw []byte) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("nothing to compress")
	}
	bound := int(C.sqd_zstd_bound(C.size_t(len(raw))))
	if bound <= 0 {
		return nil, fmt.Errorf("ZSTD_compressBound reported %d for %d bytes", bound, len(raw))
	}
	w, err := z.worker(0)
	if err != nil {
		return nil, err
	}
	dst := make([]byte, bound)
	var errName *C.char
	n := int(C.sqd_zstd_compress(w.cctx,
		(*C.uchar)(unsafe.Pointer(&raw[0])), C.int(len(raw)),
		(*C.uchar)(unsafe.Pointer(&dst[0])), C.int(bound),
		C.int(zstdBlobLevel), &errName))
	if n <= 0 {
		return nil, fmt.Errorf("ZSTD_compressCCtx failed on a %d-byte section: %s",
			len(raw), C.GoString(errName))
	}
	return dst[:n], nil
}

// zstdDecompressBlob undoes CompressBlob, and is what decompressBlob dispatches
// codecZSTD to. It loads the library itself, for the reason lzoDecompressBlob
// does: a section is decoded before the delta's superblock has been parsed.
func zstdDecompressBlob(ctx context.Context, stored []byte, rawLen int) ([]byte, error) {
	// One worker: a section is one blob, decompressed once.
	z, err := newZstdCompressor(1)
	if err != nil {
		return nil, err
	}
	if rawLen <= 0 {
		return nil, fmt.Errorf("section declares %d raw bytes", rawLen)
	}
	w, err := z.worker(0)
	if err != nil {
		return nil, err
	}
	plain, err := w.decompressBlock(stored, rawLen)
	if err != nil {
		return nil, err
	}
	if len(plain) != rawLen {
		return nil, fmt.Errorf("section decompressed to %d bytes, expected %d", len(plain), rawLen)
	}
	return append([]byte(nil), plain...), nil
}
