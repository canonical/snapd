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

import (
	"fmt"
	"os"
	"strings"
)

const (
	// https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h#L289
	superblockSize         = 96
	metadataBlockSize      = 8192
	directoryMaxEntryCount = 256

	superBlockUncompressedInodes = 0x1
	superBlockUncompressedData   = 0x2

	// Inode types supported by squashfs
	inodeTypeDirectory         = 1
	inodeTypeFile              = 2
	inodeTypeSymlink           = 3
	inodeTypeBlockDev          = 4
	inodeTypeCharDev           = 5
	inodeTypeFifo              = 6
	inodeTypeSocket            = 7
	inodeTypeExtendedDirectory = 8
	inodeTypeExtendedFile      = 9
	inodeTypeExtendedSymlink   = 10
	inodeTypeExtendedBlockDev  = 11
	inodeTypeExtendedCharDev   = 12
	inodeTypeExtendedFifo      = 13
	inodeTypeExtendedSocket    = 14

	// Compression types supported by squashfs
	compressionZlib = 1
	compressionLzma = 2
	compressionLzo  = 3
	compressionXz   = 4
	compressionLz4  = 5
	compressionZstd = 6
)

var (
	// magic is the magic prefix of squashfs snap files.
	magic = [4]byte{'h', 's', 'q', 's'}
)

type squashfs_inode struct {
	itype uint16
	mode  uint16
	uid   uint16
	gid   uint16
	mtime uint32
	ino   uint32
}

type squashfs_inode_blkdev struct {
	base   squashfs_inode
	nlinks uint32
	devid  uint32
}

type squashfs_inode_dir struct {
	base       squashfs_inode
	startBlock uint32
	nlinks     uint32
	size       uint16
	offset     uint16
	parent_ino uint32
}

type squashfs_inode_dir_ext struct {
	base         squashfs_inode
	nlinks       uint32
	size         uint32
	startBlock   uint32
	parent_inode uint32
	indices      uint16
	offset       uint16
	xattribs     uint32
}

type squashfs_inode_reg struct {
	base       squashfs_inode
	startBlock uint32
	fragment   uint32
	offset     uint32
	size       uint32
	blockSizes []uint32
}

type squashfs_inode_symlink struct {
	base   squashfs_inode
	nlinks uint32
	size   uint32
}

type squashfs_dir_header struct {
	count      uint32
	startBlock uint32
	ino        uint32
}

type squashfs_dir_entry struct {
	startBlock uint32
	offset     uint16
	ino        int16
	itype      uint16
	size       uint16
	name       string
}

type metadataRef struct {
	offset int
	block  int64
}

type squashfs_superblock struct {
	magic           [4]byte
	inodes          uint32
	mkfsTime        uint32
	blockSize       uint32
	fragments       uint32
	compressionType uint16
	blockSizeLog2   uint16
	flags           uint16
	noIDs           uint16
	s_major         uint16
	s_minor         uint16
	rootIno         metadataRef
	bytesUsed       int64
	idTableStart    int64
	xattrIdTableSz  int64
	inodeTable      int64
	directoryTable  int64
	fragmentTable   int64
	lookupTable     int64
}

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
	node    *squashfs_inode_dir
	reader  *metaBlockReader
	loaded  bool
	entries []squashfs_dir_entry
}

type SquashFileSystem struct {
	stream          *os.File
	superBlock      *squashfs_superblock
	compression     CompressionBackend
	inodeReader     *metaBlockReader
	directoryReader *metaBlockReader
	rootDirectory   *directory
}

func readUint16(data []byte) uint16 {
	return uint16(data[0]) | uint16(data[1])<<8
}

func readInt16(data []byte) int16 {
	return int16(readUint16(data))
}

func readUint32(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func readInt32(data []byte) int32 {
	return int32(readUint32(data))
}

func readUint64(data []byte) uint64 {
	return uint64(data[0]) | uint64(data[1])<<8 | uint64(data[2])<<16 | uint64(data[3])<<24 |
		uint64(data[4])<<32 | uint64(data[5])<<40 | uint64(data[6])<<48 | uint64(data[7])<<56
}

func readInt64(data []byte) int64 {
	return int64(readUint64(data))
}

func readInodeRef(data []byte) metadataRef {
	value := readInt64(data)
	return metadataRef{
		offset: int(value & 0xffff),
		block:  value >> 16,
	}
}

func parseSuperBlock(data []byte) (*squashfs_superblock, error) {
	if len(data) < superblockSize {
		return nil, fmt.Errorf("squashfs: superblock too small")
	}

	sb := &squashfs_superblock{}
	copy(sb.magic[:], data[:4])
	if sb.magic != magic {
		return nil, fmt.Errorf("squashfs: invalid magic")
	}

	sb.inodes = readUint32(data[4:])
	sb.mkfsTime = readUint32(data[8:])
	sb.blockSize = readUint32(data[12:])
	sb.fragments = readUint32(data[16:])
	sb.compressionType = readUint16(data[20:])
	sb.blockSizeLog2 = readUint16(data[22:])
	sb.flags = readUint16(data[24:])
	sb.noIDs = readUint16(data[26:])
	sb.s_major = readUint16(data[28:])
	sb.s_minor = readUint16(data[30:])
	sb.rootIno = readInodeRef(data[32:])
	sb.bytesUsed = readInt64(data[40:])
	sb.idTableStart = readInt64(data[48:])
	sb.xattrIdTableSz = readInt64(data[56:])
	sb.inodeTable = readInt64(data[64:])
	sb.directoryTable = readInt64(data[72:])
	sb.fragmentTable = readInt64(data[80:])
	sb.lookupTable = readInt64(data[88:])
	return sb, nil
}

func readSuperBlock(stream *os.File) (*squashfs_superblock, error) {
	buffer := make([]byte, superblockSize)
	_, err := stream.Read(buffer)
	if err != nil {
		return nil, err
	}
	return parseSuperBlock(buffer)
}

func createCompressionBackend(stream *os.File, sb *squashfs_superblock) (CompressionBackend, error) {
	println("squashfs: compression type", sb.compressionType)
	var optionsBlock *metaBlockReader = nil
	if sb.flags&0x400 != 0 {
		optionsBlock = metablockReaderCreate(stream, nil, superblockSize)
	}

	switch sb.compressionType {
	case compressionXz:
		return createXzBackend(optionsBlock)
	default:
		return nil, fmt.Errorf("squashfs: unsupported compression type %d", sb.compressionType)
	}
}

// SquashFS layout
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
func SquashFsOpen(path string) (*SquashFileSystem, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	sb, err := readSuperBlock(f)
	if err != nil {
		return nil, err
	}

	// handle compression type
	cb, err := createCompressionBackend(f, sb)
	if err != nil {
		return nil, err
	}

	sfs := &SquashFileSystem{
		stream:          f,
		superBlock:      sb,
		compression:     cb,
		inodeReader:     metablockReaderCreate(f, cb, sb.inodeTable, sb.rootIno),
		directoryReader: metablockReaderCreate(f, cb, sb.directoryTable),
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

func (sfs *SquashFileSystem) directoryCreate(node *squashfs_inode_dir) *directory {
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

	inodeType := readUint16(typeBuffer)
	switch inodeType {
	case inodeTypeFile:
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

	inode := &squashfs_inode_dir{}
	inode.parse(inodeBuffer)

	sfs.rootDirectory = sfs.directoryCreate(inode)
	return nil
}

func (sfs *SquashFileSystem) Close() error {
	return sfs.stream.Close()
}

func (sfs *SquashFileSystem) readDirectoryEntryInode(entry *squashfs_dir_entry) ([]byte, error) {
	if err := sfs.inodeReader.seek(int64(entry.startBlock), int(entry.offset)); err != nil {
		return nil, err
	}
	return sfs.readInodeData()
}

func (sfs *SquashFileSystem) createDirectoryFromDirectoryEntry(entry *squashfs_dir_entry) (*directory, error) {
	if !entry.isDirectory() {
		return nil, fmt.Errorf("squashfs: %s is not a directory", entry.name)
	}

	inodeBuffer, err := sfs.readDirectoryEntryInode(entry)
	if err != nil {
		return nil, err
	}

	inode := &squashfs_inode_dir{}
	inode.parse(inodeBuffer)
	return sfs.directoryCreate(inode), nil
}

func (sfs *SquashFileSystem) readFileFromDirectoryEntry(entry *squashfs_dir_entry) ([]byte, error) {
	if entry.isDirectory() {
		return nil, fmt.Errorf("squashfs: %s is must not be a directory", entry.name)
	}

	if entry.isSymlink() {
		return nil, fmt.Errorf("squashfs: %s is a symlink, and we do not support this yet", entry.name)
	}

	inodeBuffer, err := sfs.readDirectoryEntryInode(entry)
	if err != nil {
		return nil, err
	}

	if entry.isRegularFile() {
		inode := &squashfs_inode_reg{}
		inode.parse(inodeBuffer)
		return inode.read_data(sfs)
	} else {
		return nil, fmt.Errorf("squashfs: %s is not a regular file", entry.name)
	}
}

func (sfs *SquashFileSystem) ReadFile(path string) ([]byte, error) {
	currentDirectory := sfs.rootDirectory

	// split the provided path into tokens based on '/'
	tokens := strings.Split(path, "/")
	for i, token := range tokens {
		println("squashfs: token", i, "/", len(tokens), token)
		entry, err := currentDirectory.lookupDirectoryEntry(token)
		if err != nil {
			return nil, err
		}

		if i == len(tokens)-1 {
			// last token, entry shall not be a directory
			if entry.isDirectory() {
				return nil, fmt.Errorf("squashfs: %s is a directory", path)
			}
			return sfs.readFileFromDirectoryEntry(entry)
		}

		// otherwise we have to descend into the directory
		// make sure that is a directory
		if !entry.isDirectory() {
			return nil, fmt.Errorf("squashfs: %s is not a directory", path)
		}
		currentDirectory, err = sfs.createDirectoryFromDirectoryEntry(entry)
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("squashfs: %s not found", path)
}

func parseInode(data []byte) squashfs_inode {
	node := squashfs_inode{}
	node.itype = readUint16(data[0:])
	node.mode = readUint16(data[2:])
	node.uid = readUint16(data[4:])
	node.gid = readUint16(data[6:])
	node.mtime = readUint32(data[8:])
	node.ino = readUint32(data[12:])
	return node
}

func (n *squashfs_inode_dir) parse(data []byte) {
	n.base = parseInode(data)
	n.startBlock = readUint32(data[16:])
	n.nlinks = readUint32(data[20:])
	n.size = readUint16(data[24:])
	n.offset = readUint16(data[26:])
	n.parent_ino = readUint32(data[28:])
}

func getDefaultInodeSize(inoType uint16) int {
	switch inoType {
	case inodeTypeDirectory:
		return 32
	case inodeTypeFile:
		return 32
	case inodeTypeSymlink:
		return 24
	case inodeTypeBlockDev:
		return 24
	case inodeTypeExtendedDirectory:
		return 40
	default:
		return 0
	}
}

func (de *squashfs_dir_entry) isDirectory() bool {
	return de.itype == inodeTypeDirectory || de.itype == inodeTypeExtendedDirectory
}

func (de *squashfs_dir_entry) isSymlink() bool {
	return de.itype == inodeTypeSymlink || de.itype == inodeTypeExtendedSymlink
}

func (de *squashfs_dir_entry) isRegularFile() bool {
	return de.itype == inodeTypeFile
}
