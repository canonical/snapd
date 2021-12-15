// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
 * https://www.kernel.org/doc/html/v5.8/filesystems/squashfs.html
 */

package squashfs2

/*
#cgo LDFLAGS: -llzma
#include <lzma.h>
#include <stdlib.h>

int wrapper_lzma_code(lzma_stream* handle, void* in, void* out, lzma_action action) {
    handle->next_in = in;
    handle->next_out = out;
    return lzma_code(handle, action);
}
*/
import "C"

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

// Flags passed to liblzma stream decoder constructors.
// See liblzma/src/liblzma/api/lzma/container.h.
const (
	// This flag makes lzma_code() return LZMA_NO_CHECK if the input stream being decoded has no integrity check
	tellNoCheck = 1 << 0

	// This flag makes lzma_code() return LZMA_UNSUPPORTED_CHECK if the input
	// stream has an integrity check, but the type of the integrity check is not
	// supported by this liblzma version or build. Such files can still be
	// decoded, but the integrity check cannot be verified.
	tellUnsupportedCheck = 1 << 1

	// This flag makes lzma_code() return LZMA_GET_CHECK as soon as the type
	// of the integrity check is known. The type can then be got with
	// lzma_get_check().
	tellAnyCheck = 1 << 2

	// This flag enables decoding of concatenated files with file formats that
	// allow concatenating compressed files as is. From the formats currently
	// supported by liblzma, only the .xz format allows concatenated files.
	// Concatenated files are not allowed with the legacy .lzma format.
	concatenated = 1 << 3
)

const (
	actionRun       = 0 // Run coding
	actionSyncFlush = 1 // Make all the input available at output.
	actionFullFlush = 2 // Finish encoding of the current Block.
	actionFinish    = 3 // Finish the coding operation.
)

const (
	lzmaErrOk          = 0  // no error
	lzmaErrEndOfStream = 1  // end of stream reached
	lzmaErrNoCheck     = 2  // no integrity check found in input stream
	lzmaErrUnsupported = 3  // unsupported integrity check
	lzmaErrGetCheck    = 4  // integrity check is now available
	lzmaErrMem         = 5  // memory allocation failed
	lzmaErrMemlimit    = 6  // memory usage limit reached
	lzmaErrFormat      = 7  // unsupported file format
	lzmaErrOptions     = 8  // unsupported preset or compression options
	lzmaErrData        = 9  // compressed data is corrupt
	lzmaErrBufsize     = 10 // compressed data is truncated
	lzmaErrProg        = 11 // compression program is corrupt
)

type xzBackend struct {
	dictionarySize    int
	executableFilters int
}

type lzmaBackend struct {
	// no options for lzma
}

type lzmaReader struct {
	stream *C.lzma_stream
}

// According to spec the XZ options are of size 8 bytes
// i32 - Dictionary Size
// i32 - Executable Filters (have no idea how to use those)
func xzParseOptions(m *metaBlockReader) (int, int, error) {
	buffer := make([]byte, 8)
	err := m.read(buffer)
	if err != nil {
		return -1, -1, err
	}

	dictionarySize := internal.ReadInt32(buffer[0:])
	executableFilters := internal.ReadInt32(buffer[4:])
	return int(dictionarySize), int(executableFilters), nil
}

func createXzBackend(m *metaBlockReader) (xzBackend, error) {
	dictionarySize := -1
	executableFilters := -1
	if m != nil {
		size, filters, err := xzParseOptions(m)
		if err != nil {
			return xzBackend{}, err
		}
		dictionarySize = size
		executableFilters = filters
	}

	return xzBackend{
		dictionarySize:    dictionarySize,
		executableFilters: executableFilters,
	}, nil
}

func createLzmaBackend() (lzmaBackend, error) {
	return lzmaBackend{}, nil
}

func (xb xzBackend) createXzReader() (*lzmaReader, error) {
	// create the lzma stream
	stream := (*C.lzma_stream)(C.calloc(1, (C.size_t)(unsafe.Sizeof(C.lzma_stream{}))))
	if stream == nil {
		return &lzmaReader{}, fmt.Errorf("failed to allocate lzma stream")
	}

	// Initialize decoder
	ret := C.lzma_auto_decoder(stream, C.uint64_t(math.MaxUint64), C.uint32_t(concatenated))
	if ret != 0 {
		return nil, fmt.Errorf("failed to initialize lzma decoder: %d", ret)
	}

	return &lzmaReader{stream: stream}, nil
}

func (r *lzmaReader) Close() {
	if r.stream != nil {
		C.lzma_end(r.stream)
		C.free(unsafe.Pointer(r.stream))
	}
}

func (xb xzBackend) Decompress(compressedData []byte, decompressedData []byte) (int, error) {
	reader, err := xb.createXzReader()
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	reader.stream.avail_in = C.size_t(len(compressedData))
	reader.stream.avail_out = C.size_t(len(decompressedData))

	// actually do the uncompression
	ret := C.wrapper_lzma_code(reader.stream,
		unsafe.Pointer(&compressedData[0]),
		unsafe.Pointer(&decompressedData[0]),
		C.lzma_action(actionRun),
	)
	if ret != lzmaErrOk && ret != lzmaErrEndOfStream {
		return 0, fmt.Errorf("failed to uncompress data")
	}

	bytesRead := len(decompressedData) - int(reader.stream.avail_out)
	return bytesRead, nil
}

func (xb lzmaBackend) createLzmaReader() (*lzmaReader, error) {
	// create the lzma stream
	stream := (*C.lzma_stream)(C.calloc(1, (C.size_t)(unsafe.Sizeof(C.lzma_stream{}))))
	if stream == nil {
		return &lzmaReader{}, fmt.Errorf("failed to allocate lzma stream")
	}

	// Initialize decoder
	ret := C.lzma_auto_decoder(stream, C.uint64_t(math.MaxUint64), C.uint32_t(concatenated))
	if ret != 0 {
		return nil, fmt.Errorf("failed to initialize lzma decoder: %d", ret)
	}

	return &lzmaReader{stream: stream}, nil
}

func (xb lzmaBackend) Decompress(compressedData []byte, decompressedData []byte) (int, error) {
	reader, err := xb.createLzmaReader()
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	reader.stream.avail_in = C.size_t(len(compressedData))
	reader.stream.avail_out = C.size_t(len(decompressedData))

	// actually do the uncompression
	ret := C.wrapper_lzma_code(reader.stream,
		unsafe.Pointer(&compressedData[0]),
		unsafe.Pointer(&decompressedData[0]),
		C.lzma_action(actionRun),
	)
	if ret != lzmaErrOk && ret != lzmaErrEndOfStream {
		return 0, fmt.Errorf("failed to uncompress data")
	}

	bytesRead := len(decompressedData) - int(reader.stream.avail_out)
	return bytesRead, nil
}
