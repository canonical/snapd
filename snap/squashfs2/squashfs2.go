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
	"strings"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

type CompressionBackend interface {
	Decompress(in []byte, out []byte) (int, error)
}

type metaBlockReader struct {
	stream       *os.File
	streamOffset int64
	compression  CompressionBackend

	// current reference into metadata block
	currentBlock  int64 // block position, offset from start of metadata stream
	currentOffset int   // offset into block
}

type directory struct {
	node    *internal.InodeDir
	reader  *metaBlockReader
	loaded  bool
	entries []internal.DirectoryEntry
}

type SquashFileSystem struct {
	stream          *os.File
	superBlock      *internal.SuperBlock
	compression     CompressionBackend
	inodeReader     *metaBlockReader
	directoryReader *metaBlockReader
	rootDirectory   *directory
}

func readSuperBlock(stream *os.File) (*internal.SuperBlock, error) {
	buffer := make([]byte, internal.SuperBlockSize)
	_, err := stream.Read(buffer)
	if err != nil {
		return nil, err
	}

	sb := &internal.SuperBlock{}
	if err := sb.Parse(buffer); err != nil {
		return nil, err
	}
	return sb, nil
}

func createCompressionBackend(stream *os.File, sb *internal.SuperBlock) (CompressionBackend, error) {
	println("squashfs: compression type", sb.CompressionType)
	var optionsBlock *metaBlockReader = nil
	if sb.Flags&internal.SuperBlockCompressorOptions != 0 {
		optionsBlock = metablockReaderCreate(stream, nil, internal.SuperBlockSize)
	}

	switch sb.CompressionType {
	case internal.CompressionXz:
		return createXzBackend(optionsBlock)
	case internal.CompressionLzma:
		return createLzmaBackend() // lzma does not support the options block
	case internal.CompressionLzo:
		return createLzoBackend(optionsBlock)
	default:
		return nil, fmt.Errorf("squashfs: unsupported compression type %d", sb.CompressionType)
	}
}

// createInodeReader Instantiates a new inode metadata reader with the appropriate compression support
func createInodeReader(stream *os.File, cb CompressionBackend, sb *internal.SuperBlock) (*metaBlockReader, error) {
	if sb.Flags&internal.SuperBlockUncompressedInodes != 0 {
		inodeReader := metablockReaderCreate(stream, nil, sb.InodeTable, sb.RootIno)
		if inodeReader == nil {
			return nil, fmt.Errorf("squashfs: failed to create inode reader")
		}
		return inodeReader, nil
	} else {
		inodeReader := metablockReaderCreate(stream, cb, sb.InodeTable, sb.RootIno)
		if inodeReader == nil {
			return nil, fmt.Errorf("squashfs: failed to create inode reader")
		}
		return inodeReader, nil
	}
}

// SquashFS layout
// from: https://dr-emann.github.io/squashfs/
// ---------------
// |  superblock   |
// |---------------|
// |  compression  |
// |    options    |
// |---------------|
// |  datablocks   |
// |  & fragments  |
// |---------------|
// |  inode table  |
// |---------------|
// |   directory   |
// |     table     |
// |---------------|
// |   fragment    |
// |    table      |
// |---------------|
// |    export     |
// |    table      |
// |---------------|
// |    uid/gid    |
// |  lookup table |
// |---------------|
// |     xattr     |
// |     table     |
// |---------------|
func Open(path string) (*SquashFileSystem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	// Remember to close the file when we're done if any
	// errors happens.
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	sb, err := readSuperBlock(f)
	if err != nil {
		return nil, err
	}

	// handle compression type
	cb, err := createCompressionBackend(f, sb)
	if err != nil {
		return nil, err
	}

	// create inode reader
	inodeReader, err := createInodeReader(f, cb, sb)
	if err != nil {
		return nil, err
	}

	// create directory reader
	directoryReader := metablockReaderCreate(f, cb, sb.DirectoryTable)
	if directoryReader == nil {
		return nil, fmt.Errorf("squashfs: failed to create directory reader")
	}

	sfs := &SquashFileSystem{
		stream:          f,
		superBlock:      sb,
		compression:     cb,
		inodeReader:     inodeReader,
		directoryReader: directoryReader,
	}

	// initialize root directory right away so we can start
	// reading from it immediately.
	err = sfs.loadRootDirectory()
	if err != nil {
		sfs.Close()
		return nil, err
	}

	return sfs, nil
}

func (sfs *SquashFileSystem) directoryCreate(node *internal.InodeDir) *directory {
	return &directory{
		node:   node,
		reader: sfs.directoryReader,
		loaded: false,
	}
}

func (sfs *SquashFileSystem) readInodeData() ([]byte, error) {
	typeBuffer := make([]byte, 2)
	err := sfs.inodeReader.read(typeBuffer)
	if err != nil {
		return nil, err
	}

	inodeType := internal.ReadUint16(typeBuffer)
	switch inodeType {
	case internal.InodeTypeFile:
		inodeBuffer, err := inodeRegularRead(sfs.inodeReader)
		if err != nil {
			return nil, err
		}
		return append(typeBuffer, inodeBuffer...), nil
	default:
		inodeSize := getDefaultInodeSize(inodeType)
		if inodeSize == 0 {
			return nil, fmt.Errorf("squashfs: invalid inode type %d", inodeType)
		}

		inodeBuffer := make([]byte, inodeSize+2)
		copy(inodeBuffer, typeBuffer)
		err = sfs.inodeReader.read(inodeBuffer[2:])
		if err != nil {
			return nil, err
		}
		return inodeBuffer, nil
	}
}

func (sfs *SquashFileSystem) loadRootDirectory() error {
	inodeBuffer, err := sfs.readInodeData()
	if err != nil {
		return err
	}

	inode := &internal.InodeDir{}
	inode.Parse(inodeBuffer)

	sfs.rootDirectory = sfs.directoryCreate(inode)
	return nil
}

func (sfs *SquashFileSystem) Close() error {
	return sfs.stream.Close()
}

func (sfs *SquashFileSystem) readDirectoryEntryInode(entry *internal.DirectoryEntry) ([]byte, error) {
	if err := sfs.inodeReader.seek(int64(entry.StartBlock), int(entry.Offset)); err != nil {
		return nil, err
	}
	return sfs.readInodeData()
}

func (sfs *SquashFileSystem) createDirectoryFromDirectoryEntry(entry *internal.DirectoryEntry) (*directory, error) {
	if !entry.IsDirectory() {
		return nil, fmt.Errorf("squashfs: %s is not a directory", entry.Name)
	}

	inodeBuffer, err := sfs.readDirectoryEntryInode(entry)
	if err != nil {
		return nil, err
	}

	inode := &internal.InodeDir{}
	inode.Parse(inodeBuffer)
	return sfs.directoryCreate(inode), nil
}

func (sfs *SquashFileSystem) readFileFromDirectoryEntry(entry *internal.DirectoryEntry) ([]byte, error) {
	if entry.IsDirectory() {
		return nil, fmt.Errorf("squashfs: %s is must not be a directory", entry.Name)
	}

	if entry.IsSymlink() {
		return nil, fmt.Errorf("squashfs: %s is a symlink, and we do not support this yet", entry.Name)
	}

	inodeBuffer, err := sfs.readDirectoryEntryInode(entry)
	if err != nil {
		return nil, err
	}

	if !entry.IsRegularFile() {
		return nil, fmt.Errorf("squashfs: %s is not a regular file", entry.Name)
	}

	inode := &internal.InodeReg{}
	inode.Parse(inodeBuffer)
	return sfs.readInodeFileData(inode)
}

func (sfs *SquashFileSystem) ReadFile(path string) ([]byte, error) {
	currentDirectory := sfs.rootDirectory

	// split the provided path into tokens based on '/'
	tokens := strings.Split(path, "/")
	for i, token := range tokens {
		entry, err := currentDirectory.lookupDirectoryEntry(token)
		if err != nil {
			return nil, err
		}

		if i == len(tokens)-1 {
			// last token, entry shall not be a directory
			if entry.IsDirectory() {
				return nil, fmt.Errorf("squashfs: %s is a directory", path)
			}
			return sfs.readFileFromDirectoryEntry(entry)
		}

		// otherwise we have to descend into the directory
		// make sure that is a directory
		if !entry.IsDirectory() {
			return nil, fmt.Errorf("squashfs: %s is not a directory", path)
		}
		currentDirectory, err = sfs.createDirectoryFromDirectoryEntry(entry)
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("squashfs: %s not found", path)
}

func getDefaultInodeSize(inoType uint16) int {
	switch inoType {
	case internal.InodeTypeDirectory:
		return 32
	case internal.InodeTypeFile:
		return 32
	case internal.InodeTypeSymlink:
		return 24
	case internal.InodeTypeBlockDev:
		return 24
	case internal.InodeTypeExtendedDirectory:
		return 40
	default:
		return 0
	}
}
