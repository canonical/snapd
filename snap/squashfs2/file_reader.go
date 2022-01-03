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
	"io"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

const (
	blockSizeUncompressed = 0x1000000
	blockSizeMask         = 0xFEFFFFFF
)

type FileReader struct {
	fileSystem *SquashFileSystem
	inode      *internal.InodeReg
	fileOffset int64
	position   int64
}

func createFileReader(sfs *SquashFileSystem, entry *internal.DirectoryEntry) (*FileReader, error) {
	inodeBuffer, err := sfs.readDirectoryEntryInode(entry)
	if err != nil {
		return nil, err
	}

	if !entry.IsRegularFile() {
		return nil, fmt.Errorf("squashfs: %s is not a regular file", entry.Name)
	}

	inode := &internal.InodeReg{}
	if err := inode.Parse(inodeBuffer); err != nil {
		return nil, err
	}

	return &FileReader{
		fileSystem: sfs,
		inode:      inode,
		fileOffset: 0,
		position:   0,
	}, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (fr *FileReader) seekUncompressed(offset int64) error {
	_, err := fr.fileSystem.stream.Seek(int64(fr.inode.StartBlock)+offset, 0)
	return err
}

func (fr *FileReader) seekCompressed(offset int64) error {
	var blockOffset int64
	for _, block := range fr.inode.BlockSizes {
		blockSize := int64(block & blockSizeMask)
		if offset < blockOffset+blockSize {
			break
		}
		blockOffset += blockSize
	}
	return fr.seekUncompressed(blockOffset)
}

func (fr *FileReader) readUncompressed(p []byte) (int, error) {
	bytesAvailable := min64(int64(fr.inode.Size)-fr.fileOffset, int64(len(p)))
	if bytesAvailable == 0 {
		return 0, io.EOF
	}

	if err := fr.seekUncompressed(fr.fileOffset); err != nil {
		return 0, err
	}

	bytesRead, err := fr.fileSystem.stream.Read(p[:bytesAvailable])
	if err != nil {
		return 0, err
	}
	fr.fileOffset += int64(bytesRead)
	return bytesRead, nil
}

func (fr *FileReader) readCompressed(p []byte) (int, error) {
	bytesAvailable := min64(int64(fr.inode.Size)-fr.position, int64(len(p)))
	if bytesAvailable == 0 {
		return 0, io.EOF
	}

	if err := fr.seekCompressed(fr.fileOffset); err != nil {
		return 0, err
	}

	// fr.currentOffset is the offset into the file, not into the
	// file's data.
	var fileOffset int64
	var dataOffset int64
	var bytesRead int
	for _, block := range fr.inode.BlockSizes {
		blockSize := int64(block & blockSizeMask)

		// skip forward to the correct block
		if fr.fileOffset >= fileOffset+blockSize {
			fileOffset += blockSize
			dataOffset += int64(fr.fileSystem.superBlock.BlockSize)
			continue
		}

		// bytes left in this block
		bytesLeftInBlock := (dataOffset + int64(fr.fileSystem.superBlock.BlockSize)) - fr.position
		bytesLeftInFile := int64(fr.inode.Size) - fr.position
		bytesToCopy := min64(min64(bytesLeftInBlock, int64(len(p)-bytesRead)), bytesLeftInFile)

		fileData := make([]byte, blockSize)
		if _, err := fr.fileSystem.stream.Read(fileData); err != nil {
			return 0, err
		}

		// handle the data differently based on the state of the blocks
		// compression
		if block&blockSizeUncompressed == 0 {
			decompressedBuffer := make([]byte, fr.fileSystem.superBlock.BlockSize)
			_, err := fr.fileSystem.compression.Decompress(fileData, decompressedBuffer)
			if err != nil {
				return 0, err
			}

			// copy the bytes into the buffer
			bufferOffset := fr.position - dataOffset
			copy(p[bytesRead:], decompressedBuffer[bufferOffset:bufferOffset+bytesToCopy])
		} else {
			// copy the bytes into the buffer
			bufferOffset := fr.position - dataOffset
			copy(p[bytesRead:], fileData[bufferOffset:bufferOffset+bytesToCopy])
		}

		// increase position and bytes read
		fr.position += bytesToCopy
		bytesRead += int(bytesToCopy)

		// should we also switch to next block?
		if bytesLeftInBlock == bytesToCopy {
			fr.fileOffset += blockSize
		}

		// are we done reading?
		if bytesRead == len(p) {
			break
		}
	}
	return bytesRead, nil
}

func (fr *FileReader) Read(p []byte) (int, error) {
	if fr.inode.Fragment != 0xFFFFFFFF {
		return 0, fmt.Errorf("squashfs: inode uses the fragment table, and we do not support this yet")
	}

	// Handle the case where compression is turned off for data
	if fr.fileSystem.superBlock.Flags&internal.SuperBlockUncompressedData != 0 {
		return fr.readUncompressed(p)
	}

	// Otherwise we do an compressed read, where we must read in block boundaries
	// and decompress each block (possibly!)
	return fr.readCompressed(p)
}

func (fr *FileReader) Copy(dst io.Writer, src io.Reader) (written int64, err error) {
	buffer := make([]byte, fr.fileSystem.superBlock.BlockSize)
	for written < int64(fr.inode.Size) {
		bytesRead, err := fr.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			return written, err
		}
		if bytesRead == 0 {
			break
		}
		if _, err := dst.Write(buffer[:bytesRead]); err != nil {
			return written, err
		}
		written += int64(bytesRead)
	}
	return 0, nil
}
