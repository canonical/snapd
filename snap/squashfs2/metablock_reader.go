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
	"fmt"
	"os"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

type metablock struct {
	position  int64
	length    int
	data      []byte
	nextBlock int64
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Metadata (inodes and directories) are compressed in 8Kbyte blocks.
// Each compressed block is prefixed by a two byte length, the top bit is set if the block is uncompressed.
// Inodes are packed into the metadata blocks, and are not aligned to block boundaries, therefore inodes
// overlap compressed blocks. Inodes are identified by a 48-bit number which encodes the location of the
// compressed metadata block containing the inode, and the byte offset into
// that block where the inode is placed (<block, offset>).
func metablockReaderCreate(stream *os.File, compression CompressionBackend, offset int64, ref ...internal.MetadataRef) *metaBlockReader {
	m := &metaBlockReader{
		stream:        stream,
		streamOffset:  offset,
		compression:   compression,
		currentBlock:  0,
		currentOffset: 0,
	}

	if len(ref) != 0 {
		err := m.seekToRef(ref[0])
		if err != nil {
			return nil
		}
	}
	return m
}

func (m *metaBlockReader) seekToRef(ref internal.MetadataRef) error {
	if ref.Offset < 0 || ref.Offset >= metadataBlockSize {
		return fmt.Errorf("squashfs: invalid metadata offset %d", ref.Offset)
	}
	m.currentBlock = ref.Block
	m.currentOffset = ref.Offset
	return nil
}

func (m *metaBlockReader) seek(block int64, offset int) error {
	if offset < 0 || offset >= metadataBlockSize {
		return fmt.Errorf("squashfs: invalid metadata offset %d", offset)
	}
	m.currentBlock = block
	m.currentOffset = offset
	return nil
}

func parseMetablockLength(data []byte) (int, bool) {
	length := internal.ReadUint16(data)
	isCompressed := (length & 0x8000) == 0
	length &= 0x7fff
	return int(length), isCompressed
}

func (m *metaBlockReader) readMetablock(position int64) (*metablock, error) {
	if _, err := m.stream.Seek(position, 0); err != nil {
		return nil, err
	}

	// read the first two bytes, contains length of block and
	// whether or not the data is compressed
	buffer := make([]byte, 2)
	if _, err := m.stream.Read(buffer); err != nil {
		return nil, err
	}

	length, isCompressed := parseMetablockLength(buffer)
	if length == 0 {
		return nil, fmt.Errorf("squashfs: invalid metablock length")
	}

	// Store the length of the data into availableData which will be modified
	// in case of compression. We want to keep length intact to calculate next
	// block offset
	availableData := length

	// Read the data in any case, whether uncompressed or not. We determine
	// afterwards what to do with it.
	buffer = make([]byte, length)
	bytesRead, err := m.stream.Read(buffer)
	if err != nil {
		return nil, err
	}

	// If the compression backend is set to nil, then the metadata has been
	// marked as uncompressed in the superblock. And that takes precendence.
	if isCompressed && m.compression != nil {
		// Decompressed data is always less or equal to 8KiB
		decompressedBuffer := make([]byte, metadataBlockSize)
		bytesDecompressed, err := m.compression.Decompress(buffer, decompressedBuffer)
		if err != nil {
			return nil, err
		}

		buffer = decompressedBuffer
		availableData = bytesDecompressed
	} else {
		availableData = bytesRead
	}

	return &metablock{
		position: position,
		length:   availableData,
		data:     buffer,

		// The next block follows just after this one, which means
		// the current position, the length header, and then all the data
		// we read.
		nextBlock: position + int64(length) + 2,
	}, nil
}

func (m *metaBlockReader) read(buffer []byte) error {
	block, err := m.readMetablock(m.streamOffset + m.currentBlock)
	if err != nil {
		return err
	}

	bytesRead := 0
	for bytesRead < len(buffer) {
		// If we have reached the end of the current block, read the next one
		// It's important to note we can start several blocks behind target, so
		// keep switching blocks until we reach the right one
		for m.currentOffset >= block.length {
			nextBlock, err := m.readMetablock(block.nextBlock)
			if err != nil {
				return err
			}
			m.currentBlock = block.position - m.streamOffset
			m.currentOffset -= block.length
			block = nextBlock
		}

		// Now that we have made sure we have enough data, copy it to the
		// buffer and update the current offset
		bytesToCopy := min(block.length-m.currentOffset, len(buffer)-bytesRead)
		copy(buffer[bytesRead:], block.data[m.currentOffset:m.currentOffset+bytesToCopy])
		m.currentOffset += bytesToCopy
		bytesRead += bytesToCopy
	}
	return nil
}
