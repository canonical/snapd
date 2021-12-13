// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

import "fmt"

func inodeRegularRead(reader *metaBlockReader) ([]byte, error) {
	// Read the rest of the base inode
	baseData := make([]byte, 30)
	if err := reader.read(baseData); err != nil {
		return nil, err
	}

	// Get size of file, usually offset 28, but the type flags
	// are already read, so it offsets us 2 bytes into the structure
	size := readUint32(baseData[26:])

	// Read the blocksize table, the blocksizes vary in their meaning
	// based on whether or not the fragment table are used. Currently we
	// have no fragment table support, so we assume the data blocks just mean
	// the sizes of the data blocks, excluding bit 24 that tells us whether or
	// not the block is uncompressed.
	var blockData []byte
	for i := uint32(0); i < size; {
		data := make([]byte, 4)
		if err := reader.read(data); err != nil {
			return nil, err
		}

		// add the data to the block data buffer so we can
		// parse it later as well
		blockData = append(blockData, data...)

		// ... but parse it already as we need to know the size
		blockSize := readUint32(data)
		i += (blockSize & 0xFEFFFFFF)
	}
	return append(baseData, blockData...), nil
}

func (n *squashfs_inode_reg) parse(data []byte) {
	n.base = parseInode(data)
	n.startBlock = readUint32(data[16:])
	n.fragment = readUint32(data[20:])
	n.offset = readUint32(data[24:])
	n.size = readUint32(data[28:])

	// read the blocksize table into the struct
	for i := 32; i < len(data); i += 4 {
		blockSize := readUint32(data[i:])
		n.blockSizes = append(n.blockSizes, blockSize)
	}
}

func (n *squashfs_inode_reg) read_data(sfs *SquashFileSystem) ([]byte, error) {
	// seek to the start of the file
	if n.fragment != 0xFFFFFFFF {
		return nil, fmt.Errorf("squashfs: inode uses the fragment table, and we do not support this yet")
	}

	// we should read in block chunks, so allocate a buffer that can hold
	// the number of blocks that cover the entire file size
	blockCount := n.size / sfs.superBlock.blockSize
	if n.size%sfs.superBlock.blockSize != 0 {
		blockCount++
	}
	buffer := make([]byte, blockCount*sfs.superBlock.blockSize)

	sfs.stream.Seek(int64(n.startBlock), 0)

	// Handle the case where compression is turned off for data
	if sfs.superBlock.flags&superBlockUncompressedData != 0 {
		_, err := sfs.stream.Read(buffer)
		if err != nil {
			return nil, err
		}
		return buffer[:n.size], nil
	}

	decompressedBuffer := make([]byte, n.size)
	for _, block := range n.blockSizes {
		if block&0x1000000 == 0 {
			// compressed block
			compressedBuffer := make([]byte, block&0xFEFFFFFF)
			if _, err := sfs.stream.Read(compressedBuffer); err != nil {
				return nil, err
			}

			_, err := sfs.compression.Decompress(compressedBuffer, decompressedBuffer)
			if err != nil {
				return nil, err
			}
		} else {
			// uncompressed block
			if _, err := sfs.stream.Read(decompressedBuffer); err != nil {
				return nil, err
			}
		}
	}
	return decompressedBuffer, nil
}
