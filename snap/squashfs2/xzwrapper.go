package squashfs2

import (
	"bytes"
	"io"

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

	dictionarySize := readInt32(buffer[0:])
	executableFilters := readInt32(buffer[4:])
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

	println("XZ dictionary size:", dictionarySize)
	println("XZ executable filters:", executableFilters)
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
