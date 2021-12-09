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
)

const (
	// https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h#L289
	superblockSize    = 96
	metadataBlockSize = 8192

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
	offset uint16
	ino    int16
	itype  uint16
	size   uint16
	name   string
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
	ref     metadataRef
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

func (sfs *SquashFileSystem) readInodeData() ([]byte, error) {
	typeBuffer := make([]byte, 2)
	err := sfs.inodeReader.read(typeBuffer)
	if err != nil {
		return nil, err
	}

	inodeType := readUint16(typeBuffer)
	inodeSize := getInodeSize(inodeType)
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

func (sfs *SquashFileSystem) loadRootDirectory() error {
	inodeBuffer, err := sfs.readInodeData()
	if err != nil {
		return err
	}

	inode := &squashfs_inode_dir{}
	inode.parse(inodeBuffer)

	sfs.rootDirectory = sfs.directoryCreate(inode, sfs.superBlock.rootIno)
	return nil
}

func (sfs *SquashFileSystem) Close() error {
	return sfs.stream.Close()
}

func (sfs *SquashFileSystem) ReadFile(path string) ([]byte, error) {

	return sfs.rootDirectory.readFile(path)
}

func (sfs *SquashFileSystem) directoryCreate(node *squashfs_inode_dir, ref metadataRef) *directory {
	return &directory{
		node:   node,
		reader: sfs.directoryReader,
		ref:    ref,
		loaded: false,
	}
}

func (d *directory) readDirectoryHeader() squashfs_dir_header {
	header := make([]byte, 12)
	d.reader.read(header)

	return squashfs_dir_header{
		count:      readUint32(header[0:]),
		startBlock: readUint32(header[4:]),
		ino:        readUint32(header[8:]),
	}
}

func (d *directory) readDirectoryEntry() (squashfs_dir_entry, int) {
	buffer := make([]byte, 8)
	d.reader.read(buffer)

	entry := squashfs_dir_entry{
		offset: readUint16(buffer[0:]),
		ino:    readInt16(buffer[2:]),
		itype:  readUint16(buffer[4:]),
		size:   readUint16(buffer[6:]),
		name:   "",
	}

	name := make([]byte, entry.size+1)
	d.reader.read(name)
	entry.name = string(name)
	return entry, int(entry.size) + 8 + 1
}

func (d *directory) loadEntries() error {
	d.reader.seekToRef(d.ref)
	println("loading directory entries", d.node.size)

	bytesRead := 0
	for bytesRead < int(d.node.size) {
		squashfs_dir_header := d.readDirectoryHeader()
		println("squashfs: directory header:", squashfs_dir_header.count, squashfs_dir_header.startBlock, squashfs_dir_header.ino)

		for i := 0; i < int(squashfs_dir_header.count); i++ {
			entry, size := d.readDirectoryEntry()
			println("squashfs: directory entry:", entry.name)
			d.entries = append(d.entries, entry)
			bytesRead += size
			break
		}

		bytesRead += 12
	}

	d.loaded = true
	return nil
}

func (d *directory) readFile(path string) ([]byte, error) {
	println("squashfs: reading file:", path)
	if !d.loaded {
		err := d.loadEntries()
		if err != nil {
			return nil, err
		}
	}

	return nil, nil
}

// Metadata (inodes and directories) are compressed in 8Kbyte blocks.
// Each compressed block is prefixed by a two byte length, the top bit is set if the block is uncompressed.
// Inodes are packed into the metadata blocks, and are not aligned to block boundaries, therefore inodes
// overlap compressed blocks. Inodes are identified by a 48-bit number which encodes the location of the
// compressed metadata block containing the inode, and the byte offset into
// that block where the inode is placed (<block, offset>).
func metablockReaderCreate(stream *os.File, compression CompressionBackend, offset int64, ref ...metadataRef) *metaBlockReader {
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

func (m *metaBlockReader) seekToRef(ref metadataRef) error {
	if ref.offset < 0 || ref.offset >= metadataBlockSize {
		return fmt.Errorf("squashfs: invalid metadata offset %d", ref.offset)
	}
	m.currentBlock = ref.block
	m.currentOffset = ref.offset
	return nil
}

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

func parseMetablockLength(data []byte) (int, bool) {
	length := readUint16(data)
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

	if isCompressed {
		if m.compression == nil {
			return nil, fmt.Errorf("squashfs: no compression backend available")
		}

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

func (n *squashfs_inode_reg) parse(data []byte) {
	n.base = parseInode(data)
	n.startBlock = readUint32(data[16:])
	n.fragment = readUint32(data[20:])
	n.offset = readUint32(data[24:])
	n.size = readUint32(data[28:])
}

func getInodeSize(inoType uint16) int {
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
