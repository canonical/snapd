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

import (
	"bytes"
	"io"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
	"github.com/ulikunitz/xz"
)

type xzBackend struct {
	DictionarySize    int
	ExecutableFilters int
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
		DictionarySize:    dictionarySize,
		ExecutableFilters: executableFilters,
	}, nil
}

func (xb xzBackend) Decompress(compressedData []byte, decompressedData []byte) (int, error) {
	reader, err := xz.NewReader(bytes.NewBuffer(compressedData))
	if err != nil {
		return 0, err
	}

	// configure the reader from the options
	if xb.DictionarySize > 0 {
		reader.DictCap = xb.DictionarySize
	}

	bytesRead, err := reader.Read(decompressedData)
	if err != nil {
		if err != io.EOF {
			return 0, err
		}
	}
	return bytesRead, nil
}
