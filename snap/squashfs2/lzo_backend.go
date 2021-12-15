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
#cgo LDFLAGS: -llzo2
#include <lzo/lzoconf.h>
#include <lzo/lzo1x.h>

// wrap lzo_init as it is a macro and we can't invoke those from go
static int wrapper_lzo_init(void) { return lzo_init(); }

// expose other macros like the expected memory requirements for each
// algorithm
static int lzo1x_1_mem_compress() { return LZO1X_1_MEM_COMPRESS; }
static int lzo1x_11_mem_compress() { return LZO1X_1_11_MEM_COMPRESS; }
static int lzo1x_12_mem_compress() { return LZO1X_1_12_MEM_COMPRESS; }
static int lzo1x_15_mem_compress() { return LZO1X_1_15_MEM_COMPRESS; }
static int lzo1x_999_mem_compress() { return LZO1X_999_MEM_COMPRESS; }
*/
import "C"
import (
	"fmt"
	"unsafe"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

const (
	lzoAlgorithm1       = 0
	lzoAlgorithm11      = 1
	lzoAlgorithm12      = 2
	lzoAlgorithm15      = 3
	lzoAlgorithm999     = 4
	lzoAlgorithmDefault = lzoAlgorithm999

	lzoCompressionLevelDefault = 8 // default compression level for lzo1x_999, all others this must be 0
)

const (
	lzoErrOk                = 0
	lzoErrError             = -1
	lzoErrOutOfMemory       = -2
	lzoErrNotCompressible   = -3
	lzoErrInputOverrun      = -4
	lzoErrOutputOverrun     = -5
	lzoErrLookbehindOverrun = -6
	lzoErrEofNotFound       = -7
	lzoErrInputNotConsumed  = -8
	lzoErrNotImplemented    = -9
)

type lzoBackend struct {
	algorithm         int
	level             int
	memoryRequirement int
}

// According to spec the LZO options are of size 8 bytes
// i32 - Algorithm
// i32 - Compression Level
func lzoParseOptions(m *metaBlockReader) (int, int, error) {
	buffer := make([]byte, 8)
	err := m.read(buffer)
	if err != nil {
		return -1, -1, err
	}

	dictionarySize := internal.ReadInt32(buffer[0:])
	executableFilters := internal.ReadInt32(buffer[4:])
	return int(dictionarySize), int(executableFilters), nil
}

func lzoGetMemoryRequirement(algorithm int) int {
	switch algorithm {
	case lzoAlgorithm1:
		return int(C.lzo1x_1_mem_compress())
	case lzoAlgorithm11:
		return int(C.lzo1x_11_mem_compress())
	case lzoAlgorithm12:
		return int(C.lzo1x_12_mem_compress())
	case lzoAlgorithm15:
		return int(C.lzo1x_15_mem_compress())
	case lzoAlgorithm999:
		return int(C.lzo1x_999_mem_compress())
	default:
		return 0
	}
}

func createLzoBackend(m *metaBlockReader) (lzoBackend, error) {
	algorithm := lzoAlgorithmDefault
	compressionLevel := lzoCompressionLevelDefault
	if m != nil {
		alg, level, err := lzoParseOptions(m)
		if err != nil {
			return lzoBackend{}, err
		}
		algorithm = alg
		compressionLevel = level
	}

	memoryRequirement := lzoGetMemoryRequirement(algorithm)
	if memoryRequirement == 0 {
		return lzoBackend{}, fmt.Errorf("squashfs: lzo: invalid or unsupported algorithm")
	}

	return lzoBackend{
		algorithm:         algorithm,
		level:             compressionLevel,
		memoryRequirement: memoryRequirement,
	}, nil
}

func (xb lzoBackend) Decompress(compressedData []byte, decompressedData []byte) (int, error) {

	length := len(decompressedData)

	// actually do the uncompression
	ret := C.lzo1x_decompress(
		(*C.uchar)(unsafe.Pointer(&compressedData[0])), C.lzo_uint(len(compressedData)),
		(*C.uchar)(unsafe.Pointer(&decompressedData[0])),
		(*C.lzo_uint)(unsafe.Pointer(&length)),
		nil,
	)
	if ret != lzoErrOk {
		return 0, fmt.Errorf("lzo: failed to uncompress data, error code %d", ret)
	}
	return length, nil
}
