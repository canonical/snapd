// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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
 */

package blockplan

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
)

// This file reads enough of a squashfs 4.0 image to describe its exact byte
// layout, which is what lets a delta replay that layout instead of asking
// mksquashfs to re-derive it.
//
// The image is a superblock, then the data region, then a run of metadata
// blocks holding the inode and directory tables, then the lookup, id and
// (optionally) xattr tables, then zero padding to a 4 KiB boundary:
//
//	0                              96                inode_table_start
//	+---------------+---------------------------------+---------------------
//	| superblock    | data blocks                     | inode table
//	+---------------+---------------------------------+---------------------
//	                                  export_table_start        bytes_used
//	---------------+--------------------------+-----------------+-----------+
//	 directory tbl | lookup / id / xattr tbls |     padding     |
//	---------------+--------------------------+-----------------+-----------+
//
// Two boundary rules matter, both established by sweeping the 43 snaps
// available locally:
//
//   - The data region is exactly [96, inode_table_start), and the extents
//     derived from the inode table cover it with no gap and no overlap. That is
//     what makes verbatim block reuse safe: nothing lives in the data region
//     that the inode table does not describe.
//
//   - frag_table_start must never be used to derive a boundary. On an image
//     with no fragments it is a stale write position pointing into the middle
//     of the metadata region -- true of all 43 snaps swept. Use
//     [inode_table_start, export_table_start), which walks to an exact
//     metadata block boundary on every one.

const (
	squashfsMagic = uint32(0x73717368)

	// superblockSize is the fixed length of the superblock, and so the offset
	// the data region starts at.
	superblockSize = 96

	// squashfsMetadataSize is SQUASHFS_METADATA_SIZE: the uncompressed size
	// of a full metadata block, and the dictionary size mksquashfs uses for
	// them.
	squashfsMetadataSize = 8192

	// dataUncompressedBit is SQUASHFS_COMPRESSED_BIT_BLOCK. Set in a block
	// size word when the block is stored verbatim.
	dataUncompressedBit = uint32(1) << 24
	// metaUncompressedBit is SQUASHFS_COMPRESSED_BIT, the same flag in the
	// two-byte metadata block header.
	metaUncompressedBit = uint16(1) << 15

	// squashfsNoTable and squashfsNoFragment are the "absent" sentinels for
	// a table pointer and for an inode's fragment index.
	squashfsNoTable    = uint64(0xFFFFFFFFFFFFFFFF)
	squashfsNoFragment = uint32(0xFFFFFFFF)

	// inodeTypeFile and inodeTypeExtFile are the two regular-file inode
	// types; only these reference data blocks.
	inodeTypeFile    = 2
	inodeTypeExtFile = 9

	// flagCompressorOptions is SQUASHFS_COMP_OPT in the superblock flags: the
	// image carries a compressor options record, so it was built with
	// something other than the wrapper's own defaults.
	flagCompressorOptions = uint16(0x0400)

	// squashfsPadding is the alignment mksquashfs zero-pads the image to.
	squashfsPadding = 4096
)

// superblock is the 96-byte squashfs superblock: the geometry, then the eight
// table pointers that describe the layout.
//
// The types in this file are unexported because nothing outside the package has
// any use for them, but their fields keep the names the on-disk structures go
// by, which is also how the tests read them through export_test.go.
//
// Reference: https://dr-emann.github.io/squashfs
type superblock struct {
	Magic            uint32
	InodeCount       uint32
	ModificationTime uint32
	BlockSize        uint32
	FragmentEntryCnt uint32
	CompressionId    uint16
	BlockLog         uint16
	Flags            uint16
	IdCount          uint16
	MajorVersion     uint16
	MinorVersion     uint16
	RootInodeRef     uint64
	BytesUsed        uint64
	IdTableStart     uint64
	XattrTableStart  uint64
	InodeTableStart  uint64
	DirTableStart    uint64
	FragTableStart   uint64
	ExportTableStart uint64
}

func parseSuperblock(raw []byte) (*superblock, error) {
	if len(raw) < superblockSize {
		return nil, fmt.Errorf("short superblock: %d bytes", len(raw))
	}
	le := binary.LittleEndian
	sb := &superblock{
		Magic:            le.Uint32(raw[0:]),
		InodeCount:       le.Uint32(raw[4:]),
		ModificationTime: le.Uint32(raw[8:]),
		BlockSize:        le.Uint32(raw[12:]),
		FragmentEntryCnt: le.Uint32(raw[16:]),
		CompressionId:    le.Uint16(raw[20:]),
		BlockLog:         le.Uint16(raw[22:]),
		Flags:            le.Uint16(raw[24:]),
		IdCount:          le.Uint16(raw[26:]),
		MajorVersion:     le.Uint16(raw[28:]),
		MinorVersion:     le.Uint16(raw[30:]),
		RootInodeRef:     le.Uint64(raw[32:]),
		BytesUsed:        le.Uint64(raw[40:]),
		IdTableStart:     le.Uint64(raw[48:]),
		XattrTableStart:  le.Uint64(raw[56:]),
		InodeTableStart:  le.Uint64(raw[64:]),
		DirTableStart:    le.Uint64(raw[72:]),
		FragTableStart:   le.Uint64(raw[80:]),
		ExportTableStart: le.Uint64(raw[88:]),
	}
	if sb.Magic != squashfsMagic {
		return nil, fmt.Errorf("not a squashfs image (magic %#08x)", sb.Magic)
	}
	return sb, nil
}

func (sb *superblock) String() string {
	return fmt.Sprintf("v%d.%d comp=%d bsize=%d log=%d inodes=%d frags=%d flags=%#04x bytes_used=%d\n"+
		"  inode_tbl=%d dir_tbl=%d frag_tbl=%d export_tbl=%d id_tbl=%d xattr_tbl=%d",
		sb.MajorVersion, sb.MinorVersion, sb.CompressionId, sb.BlockSize, sb.BlockLog,
		sb.InodeCount, sb.FragmentEntryCnt, sb.Flags, sb.BytesUsed,
		sb.InodeTableStart, sb.DirTableStart, sb.FragTableStart, sb.ExportTableStart,
		sb.IdTableStart, sb.XattrTableStart)
}

// extent is one data block as it sits in the image.
type extent struct {
	// Offset is the byte position in the image.
	Offset int64
	// CSize is the number of bytes occupied on disk.
	CSize int
	// USize is the uncompressed size, which for the last block of a file is
	// less than the image block size.
	USize int
	// Raw reports that the block is stored uncompressed, in which case CSize
	// equals USize.
	Raw bool
}

// metaBlock is one metadata block as it sits in the image.
type metaBlock struct {
	// Offset is the byte position of the two-byte size header.
	Offset int64
	// CSize is the payload length, excluding the two-byte header.
	CSize int
	// USize is the uncompressed payload length, at most 8192.
	USize int
	// Raw reports that the payload is stored uncompressed.
	Raw bool
}

// fileInode is the part of a regular-file inode that describes data placement.
type fileInode struct {
	Number     uint32
	Type       uint16
	StartBlock int64
	FileSize   uint64
	Fragment   uint32
	// Sizes are the raw block size words, one per full or partial block.
	Sizes []uint32
}

// squashfsImage is a read-only view of a squashfs image held in memory.
//
// Holding the whole image is deliberate for the generator, which needs random
// access to both source and target and runs on a build machine. The applier
// reads the source with pread instead and never materializes it.
type squashfsImage struct {
	Path string
	Data []byte
	SB   *superblock
	// Dec decompresses this image's blocks. It comes from the superblock's own
	// compressor id, so nothing that reads an image has to be told, or has to
	// guess, what compressed it.
	Dec Decompressor
}

func openImage(path string) (*squashfsImage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sb, err := parseSuperblock(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return newImageView(path, data, sb)
}

// newImageView wraps bytes that have already been read as an image, which is
// what the applier does with the source's metadata region: it holds that region
// alone and presents it as an image so the same walk serves both sides.
//
// The compressor is built here rather than lazily so an image whose compressor
// this build cannot reproduce is refused at the point it is opened, before any
// caller can start work that would have to be undone.
func newImageView(path string, data []byte, sb *superblock) (*squashfsImage, error) {
	// Decompression only, so no thread hint: the compression side builds its
	// own compressor with whatever the caller asked for.
	comp, err := newCompressor(sb.CompressionId, 0)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &squashfsImage{Path: path, Data: data, SB: sb, Dec: comp}, nil
}

// Size is the image length in bytes.
func (im *squashfsImage) Size() int64 { return int64(len(im.Data)) }

// DataRegionEnd is the first byte past the data region.
func (im *squashfsImage) DataRegionEnd() int64 { return int64(im.SB.InodeTableStart) }

// checkSupported rejects images whose layout this delta format does not
// reproduce. Refusing is cheap: the caller falls back to a full download, so a
// wrong answer here costs bandwidth, while a wrong image costs correctness.
func (im *squashfsImage) checkSupported() error {
	if err := im.SB.checkSupportedGeometry(im.Size()); err != nil {
		return err
	}
	if err := im.checkXattrTail(); err != nil {
		return err
	}
	for _, b := range im.Data[im.SB.BytesUsed:] {
		if b != 0 {
			return fmt.Errorf("trailing padding is not all zero")
		}
	}
	return nil
}

// checkXattrTail confirms an image's xattr metadata really does lie inside the
// region the delta ships verbatim, [export_table_start, bytes_used).
//
// The superblock's xattr pointer is the 16-byte squashfs_xattr_table header,
// which generic_write_table writes *after* the id table's metadata blocks; the
// value blocks it indexes sit lower still, at the offset in the header's first
// field. So the lowest xattr byte is that field, not the superblock pointer, and
// it is read here rather than assumed.
func (im *squashfsImage) checkXattrTail() error {
	start := im.SB.XattrTableStart
	if start == squashfsNoTable {
		return nil
	}
	if start+16 > im.SB.BytesUsed {
		return fmt.Errorf("xattr id table header at %d does not fit below bytes_used %d", start, im.SB.BytesUsed)
	}
	values := binary.LittleEndian.Uint64(im.Data[start:])
	if values < im.SB.ExportTableStart || values >= im.SB.BytesUsed {
		return fmt.Errorf("xattr value blocks start at %d, outside the verbatim tail [%d,%d)",
			values, im.SB.ExportTableStart, im.SB.BytesUsed)
	}
	return nil
}

// checkSupportedGeometry holds every rule that the superblock alone decides, so
// the applier can vet the target's superblock -- which arrives in SEC_SB, long
// before the image it describes exists -- against exactly the rules the
// generator applied to the real thing.
func (sb *superblock) checkSupportedGeometry(imageSize int64) error {
	switch {
	case sb.MajorVersion != 4 || sb.MinorVersion != 0:
		return fmt.Errorf("unsupported squashfs version %d.%d", sb.MajorVersion, sb.MinorVersion)
	case !compressorImplemented(sb.CompressionId):
		return fmt.Errorf("unsupported compressor %s, this build implements %s",
			compressorName(sb.CompressionId), implementedCompressors())
	case sb.Flags&flagCompressorOptions != 0:
		return fmt.Errorf("image carries COMPRESSOR_OPTIONS, whose filter chain is not reproduced")
	case sb.FragmentEntryCnt != 0:
		return fmt.Errorf("image has %d fragments, which are not described by the extent walk", sb.FragmentEntryCnt)
	}
	// FragTableStart is deliberately not checked, and never read: with no
	// fragments it holds a stale write position, which on all 43 snaps swept
	// points into the middle of the metadata region.
	// Xattrs need no work of their own: mksquashfs writes their value blocks
	// and id table last of all the tables, so they land above
	// export_table_start and travel verbatim inside SEC_MDTAIL. All this rule
	// has to enforce is that they really are up there -- checkXattrTail does
	// the image-side half, following the header's pointer to the value blocks.
	// Gate on the table pointer, never on the NO_XATTRS flag: core26 has
	// that flag clear while carrying no xattr table at all.
	if sb.XattrTableStart != squashfsNoTable && sb.XattrTableStart < sb.ExportTableStart {
		return fmt.Errorf("image has an xattr table at %d, below the export table at %d, so it is not carried verbatim",
			sb.XattrTableStart, sb.ExportTableStart)
	}
	if sb.ExportTableStart == squashfsNoTable {
		return fmt.Errorf("image has no export table, so inodes cannot be enumerated without walking directories")
	}
	if sb.BlockSize != 1<<sb.BlockLog {
		return fmt.Errorf("block size %d disagrees with block log %d", sb.BlockSize, sb.BlockLog)
	}
	// An xz block's dictionary size is its block size, and it has to be one
	// LZMA2 can name, because the container this format synthesizes encodes it
	// as a single property byte. No other compressor has a dictionary at all.
	if sb.CompressionId == compressorXz {
		if _, err := dictProp(int(sb.BlockSize)); err != nil {
			return fmt.Errorf("block size %d cannot be an LZMA2 dictionary size: %w", sb.BlockSize, err)
		}
	}
	if sb.BytesUsed > uint64(imageSize) {
		return fmt.Errorf("superblock claims %d bytes used but the file is %d", sb.BytesUsed, imageSize)
	}
	if !(superblockSize <= sb.InodeTableStart && sb.InodeTableStart <= sb.ExportTableStart && sb.ExportTableStart <= sb.BytesUsed) {
		return fmt.Errorf("table pointers are not ordered: inode=%d export=%d bytes_used=%d",
			sb.InodeTableStart, sb.ExportTableStart, sb.BytesUsed)
	}
	// The image is zero-padded up to the next 4 KiB boundary and no further, so
	// its length is fully determined by bytes_used. The applier relies on this
	// to know when it is finished.
	if want := paddedImageSize(int64(sb.BytesUsed)); want != imageSize {
		return fmt.Errorf("image is %d bytes, but %d bytes used pads to %d", imageSize, sb.BytesUsed, want)
	}
	return nil
}

// paddedImageSize rounds a bytes_used up to the padding mksquashfs applies.
func paddedImageSize(bytesUsed int64) int64 {
	return (bytesUsed + squashfsPadding - 1) / squashfsPadding * squashfsPadding
}

// --- metadata blocks ---

// readMetaBlock decodes the metadata block whose two-byte header sits at off,
// returning its uncompressed payload and its total on-disk length.
func (im *squashfsImage) readMetaBlock(ctx context.Context, off int64) (content []byte, diskLen int, raw bool, err error) {
	if off < 0 || off+2 > int64(len(im.Data)) {
		return nil, 0, false, fmt.Errorf("metadata block header at %d is out of range", off)
	}
	hdr := binary.LittleEndian.Uint16(im.Data[off:])
	raw = hdr&metaUncompressedBit != 0
	cSize := int(hdr & (metaUncompressedBit - 1))
	if off+2+int64(cSize) > int64(len(im.Data)) {
		return nil, 0, false, fmt.Errorf("metadata block at %d claims %d bytes, past end of image", off, cSize)
	}
	payload := im.Data[off+2 : off+2+int64(cSize)]
	if raw {
		return payload, 2 + cSize, true, nil
	}
	out, _, err := im.Dec.DecompressBlocks(ctx, nil, payload, []int{cSize}, squashfsMetadataSize)
	if err != nil {
		return nil, 0, false, fmt.Errorf("metadata block at %d: %w", off, err)
	}
	return out, 2 + cSize, false, nil
}

// metaRegion is a run of metadata blocks decompressed into one blob, plus the
// index needed to resolve inode references into it.
type metaRegion struct {
	// Start is the image offset the region begins at.
	Start int64
	// Blob is every block's payload concatenated.
	Blob []byte
	// Blocks describes each block's on-disk framing, in order.
	Blocks []metaBlock
	// index maps a block's start offset relative to Start to its position in
	// Blob, which is exactly the mapping an inode reference needs.
	index map[uint64]int
}

// walkMetaRegion decompresses the metadata blocks in [start, end). It requires
// the range to end exactly on a block boundary, which is the check that proves
// the region really is a pure metadata run.
func (im *squashfsImage) walkMetaRegion(ctx context.Context, start, end int64) (*metaRegion, error) {
	if start > end {
		return nil, fmt.Errorf("metadata region [%d,%d) is inverted", start, end)
	}
	// The whole region in one call: for xz that is a single process over the
	// concatenated streams, and for an in-process compressor a loop that costs
	// nothing extra. The raw blocks are spliced back in afterwards.
	var blocks []metaBlock
	var streams []byte
	var cSizes []int
	for off := start; off < end; {
		if off+2 > end {
			return nil, fmt.Errorf("metadata block header at %d crosses the region end %d", off, end)
		}
		hdr := binary.LittleEndian.Uint16(im.Data[off:])
		raw := hdr&metaUncompressedBit != 0
		cSize := int(hdr & (metaUncompressedBit - 1))
		if cSize == 0 || off+2+int64(cSize) > end {
			return nil, fmt.Errorf("metadata block at %d claims %d bytes, past the region end %d", off, cSize, end)
		}
		blk := metaBlock{Offset: off, CSize: cSize, Raw: raw}
		if raw {
			blk.USize = cSize
		} else {
			// A compressed block joins the run handed to the decompressor,
			// and so takes the next of the plaintext lengths it reports.
			streams = append(streams, im.Data[off+2:off+2+int64(cSize)]...)
			cSizes = append(cSizes, cSize)
		}
		blocks = append(blocks, blk)
		off += 2 + int64(cSize)
		if off == end {
			break
		}
		if off > end {
			return nil, fmt.Errorf("metadata walk overran the region end: %d != %d", off, end)
		}
	}

	// A metadata block's uncompressed size is recorded nowhere in the image, so
	// it comes back from the decompressor alongside the plaintext: the split
	// points are the one thing the walk cannot derive from the image itself.
	var plain []byte
	var uSizes []int
	if len(cSizes) > 0 {
		var err error
		plain, uSizes, err = im.Dec.DecompressBlocks(ctx, nil, streams, cSizes, squashfsMetadataSize)
		if err != nil {
			return nil, fmt.Errorf("metadata region [%d,%d): %w", start, end, err)
		}
		if len(uSizes) != len(cSizes) {
			return nil, fmt.Errorf("metadata region [%d,%d): decompressor reported %d sizes for %d blocks",
				start, end, len(uSizes), len(cSizes))
		}
	}

	reg := &metaRegion{Start: start, index: make(map[uint64]int, len(blocks))}
	pos, next := 0, 0
	for _, blk := range blocks {
		reg.index[uint64(blk.Offset-start)] = len(reg.Blob)
		if blk.Raw {
			reg.Blob = append(reg.Blob, im.Data[blk.Offset+2:blk.Offset+2+int64(blk.CSize)]...)
			reg.Blocks = append(reg.Blocks, blk)
			continue
		}
		n := uSizes[next]
		next++
		if pos+n > len(plain) {
			return nil, fmt.Errorf("metadata block at %d claims %d bytes but only %d remain",
				blk.Offset, n, len(plain)-pos)
		}
		blk.USize = n
		reg.Blob = append(reg.Blob, plain[pos:pos+n]...)
		pos += n
		reg.Blocks = append(reg.Blocks, blk)
	}
	if pos != len(plain) {
		return nil, fmt.Errorf("metadata region [%d,%d): consumed %d of %d decompressed bytes", start, end, pos, len(plain))
	}
	return reg, nil
}

// --- inodes ---

// InodeRegion decompresses [inode_table_start, dir_table_start), the run of
// metadata blocks holding the inode table.
func (im *squashfsImage) InodeRegion(ctx context.Context) (*metaRegion, error) {
	return im.walkMetaRegion(ctx, int64(im.SB.InodeTableStart), int64(im.SB.DirTableStart))
}

// MetaRegionAll decompresses the whole metadata region,
// [inode_table_start, export_table_start), which is what the delta patches as
// one blob.
func (im *squashfsImage) MetaRegionAll(ctx context.Context) (*metaRegion, error) {
	return im.walkMetaRegion(ctx, int64(im.SB.InodeTableStart), int64(im.SB.ExportTableStart))
}

// exportRefs reads the lookup table: inode references for inodes 1..inode_count,
// which avoids walking the directory tree to find every inode.
func (im *squashfsImage) exportRefs(ctx context.Context) ([]uint64, error) {
	sb := im.SB
	nPtr := (int(sb.InodeCount)*8 + squashfsMetadataSize - 1) / squashfsMetadataSize
	base := int64(sb.ExportTableStart)
	if base+int64(nPtr)*8 > int64(len(im.Data)) {
		return nil, fmt.Errorf("lookup table at %d needs %d pointers, past end of image", base, nPtr)
	}
	refs := make([]uint64, 0, sb.InodeCount)
	for i := 0; i < nPtr; i++ {
		ptr := binary.LittleEndian.Uint64(im.Data[base+int64(i)*8:])
		content, _, _, err := im.readMetaBlock(ctx, int64(ptr))
		if err != nil {
			return nil, fmt.Errorf("lookup table block %d: %w", i, err)
		}
		for j := 0; j+8 <= len(content); j += 8 {
			refs = append(refs, binary.LittleEndian.Uint64(content[j:]))
		}
	}
	if len(refs) < int(sb.InodeCount) {
		return nil, fmt.Errorf("lookup table yielded %d refs, expected %d", len(refs), sb.InodeCount)
	}
	return refs[:sb.InodeCount], nil
}

// parseFileInode decodes a regular-file inode at blob[off:], or returns nil for
// any other inode type. Only these two types reference data blocks.
func (im *squashfsImage) parseFileInode(blob []byte, off int) (*fileInode, error) {
	if off < 0 || off+16 > len(blob) {
		return nil, fmt.Errorf("inode at %d is out of range", off)
	}
	le := binary.LittleEndian
	fi := &fileInode{
		Type:   le.Uint16(blob[off:]),
		Number: le.Uint32(blob[off+12:]),
	}
	var sizesOff int
	switch fi.Type {
	case inodeTypeFile:
		if off+32 > len(blob) {
			return nil, fmt.Errorf("basic file inode at %d is truncated", off)
		}
		fi.StartBlock = int64(le.Uint32(blob[off+16:]))
		fi.Fragment = le.Uint32(blob[off+20:])
		fi.FileSize = uint64(le.Uint32(blob[off+28:]))
		sizesOff = off + 32
	case inodeTypeExtFile:
		if off+56 > len(blob) {
			return nil, fmt.Errorf("extended file inode at %d is truncated", off)
		}
		fi.StartBlock = int64(le.Uint64(blob[off+16:]))
		fi.FileSize = le.Uint64(blob[off+24:])
		fi.Fragment = le.Uint32(blob[off+44:])
		sizesOff = off + 56
	default:
		return nil, nil
	}
	bsz := uint64(im.SB.BlockSize)
	var nBlocks int
	if fi.Fragment == squashfsNoFragment {
		nBlocks = int((fi.FileSize + bsz - 1) / bsz)
	} else {
		nBlocks = int(fi.FileSize / bsz)
	}
	if sizesOff+nBlocks*4 > len(blob) {
		return nil, fmt.Errorf("inode %d needs %d block size words, past end of inode table", fi.Number, nBlocks)
	}
	fi.Sizes = make([]uint32, nBlocks)
	for i := range fi.Sizes {
		fi.Sizes[i] = le.Uint32(blob[sizesOff+i*4:])
	}
	return fi, nil
}

// FileInodes returns every regular-file inode, in lookup-table order.
func (im *squashfsImage) FileInodes(ctx context.Context) ([]*fileInode, error) {
	reg, err := im.InodeRegion(ctx)
	if err != nil {
		return nil, err
	}
	refs, err := im.exportRefs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*fileInode, 0, len(refs))
	for _, ref := range refs {
		blk := (ref >> 16) & 0xFFFFFFFFFFFF
		inOff := int(ref & 0xFFFF)
		base, ok := reg.index[blk]
		if !ok {
			return nil, fmt.Errorf("inode reference %#x names metadata block %d, which is not in the inode table", ref, blk)
		}
		fi, err := im.parseFileInode(reg.Blob, base+inOff)
		if err != nil {
			return nil, err
		}
		if fi != nil {
			out = append(out, fi)
		}
	}
	return out, nil
}

// fileBlock is one of a file's data blocks together with where it sits in that
// file's plaintext.
//
// The plaintext offset is what makes two revisions comparable. Between them a
// file's blocks change length, change count and move on disk, but the byte at
// plaintext offset n is still roughly the byte at plaintext offset n -- so it is
// the coordinate the generator uses to line a changed target run up against the
// source. See match.go.
type fileBlock struct {
	extent
	// UOff is the block's offset within the file's uncompressed contents,
	// counting sparse holes, which occupy plaintext but no disk.
	UOff int64
}

// inodeExtents returns one inode's data blocks in file order.
func (im *squashfsImage) inodeExtents(fi *fileInode) ([]fileBlock, error) {
	bsz := uint64(im.SB.BlockSize)
	out := make([]fileBlock, 0, len(fi.Sizes))
	off := fi.StartBlock
	var uOff int64
	remaining := fi.FileSize
	for _, w := range fi.Sizes {
		uSize := bsz
		if remaining < bsz {
			uSize = remaining
		}
		remaining -= uSize
		if w == 0 {
			// A sparse hole occupies no bytes on disk. It survives
			// through the inode's size word, which the metadata patch
			// carries, so the assembler emits nothing for it -- but it
			// does occupy plaintext, so the file offset moves on.
			uOff += int64(uSize)
			continue
		}
		cSize := int(w & ^dataUncompressedBit)
		e := extent{
			Offset: off,
			CSize:  cSize,
			USize:  int(uSize),
			Raw:    w&dataUncompressedBit != 0,
		}
		if e.Raw && e.CSize != e.USize {
			return nil, fmt.Errorf("raw block at %d occupies %d bytes but is %d uncompressed",
				off, e.CSize, e.USize)
		}
		out = append(out, fileBlock{extent: e, UOff: uOff})
		off += int64(cSize)
		uOff += int64(uSize)
	}
	return out, nil
}

// Extents returns every distinct data block in the image, sorted by offset.
//
// Distinct matters: mksquashfs deduplicates identical files, so several inodes
// can point at the same blocks. Keying on (offset, size) reproduces that
// sharing for free -- the delta emits the bytes once and every inode that
// referenced them keeps working, because the inode table is carried verbatim.
func (im *squashfsImage) Extents(ctx context.Context) ([]extent, error) {
	inodes, err := im.FileInodes(ctx)
	if err != nil {
		return nil, err
	}
	type key struct {
		off   int64
		cSize int
	}
	seen := make(map[key]extent, 1024)
	for _, fi := range inodes {
		blocks, err := im.inodeExtents(fi)
		if err != nil {
			return nil, fmt.Errorf("inode %d: %w", fi.Number, err)
		}
		for _, b := range blocks {
			k := key{b.Offset, b.CSize}
			if prev, ok := seen[k]; ok && prev.USize != b.USize {
				return nil, fmt.Errorf("block at %d is shared with disagreeing uncompressed sizes %d and %d",
					b.Offset, prev.USize, b.USize)
			}
			seen[k] = b.extent
		}
	}
	out := make([]extent, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Offset != out[j].Offset {
			return out[i].Offset < out[j].Offset
		}
		return out[i].CSize < out[j].CSize
	})
	return out, nil
}

// coverageGap is a byte range of the data region that no extent accounts for,
// or that two extents both claim.
type coverageGap struct {
	From, To int64
}

// CheckCoverage verifies that the extents tile the data region exactly. Any gap
// means bytes exist that the inode table does not describe, and any overlap
// means an extent was misplaced; either makes verbatim reuse unsafe, so both
// are refusals rather than warnings.
func (im *squashfsImage) CheckCoverage(ctx context.Context) (ext []extent, gaps, overlaps []coverageGap, err error) {
	ext, err = im.Extents(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	pos := int64(superblockSize)
	end := im.DataRegionEnd()
	for _, e := range ext {
		switch {
		case e.Offset > pos:
			gaps = append(gaps, coverageGap{pos, e.Offset})
		case e.Offset < pos:
			overlaps = append(overlaps, coverageGap{e.Offset, pos})
		}
		if e.Offset+int64(e.CSize) > pos {
			pos = e.Offset + int64(e.CSize)
		}
	}
	if pos != end {
		gaps = append(gaps, coverageGap{pos, end})
	}
	return ext, gaps, overlaps, nil
}

// decompressExtentRun appends the plaintext of consecutive compressed extents to
// dst, in one call to the decompressor rather than one per block -- which for
// the process-based compressor is the difference between one xz and one per
// block.
//
// Each extent's uncompressed size is checked against what came out. The inode
// is where that number is read from and the compressed data is where it is
// proven, so a disagreement means the extent was misplaced, and reusing it
// verbatim would put the wrong bytes in the target.
func (im *squashfsImage) decompressExtentRun(ctx context.Context, dst []byte, ext []extent) ([]byte, error) {
	cSizes := make([]int, len(ext))
	cTotal, uTotal := 0, 0
	for i, e := range ext {
		if e.Offset < 0 || e.Offset+int64(e.CSize) > int64(len(im.Data)) {
			return nil, fmt.Errorf("extent at %d (%d bytes) is out of range", e.Offset, e.CSize)
		}
		cSizes[i] = e.CSize
		cTotal += e.CSize
		uTotal += e.USize
	}
	streams := make([]byte, 0, cTotal)
	for _, e := range ext {
		streams = append(streams, im.Data[e.Offset:e.Offset+int64(e.CSize)]...)
	}
	before := len(dst)
	out, uSizes, err := im.Dec.DecompressBlocks(ctx, dst, streams, cSizes, int(im.SB.BlockSize))
	if err != nil {
		return nil, err
	}
	if len(uSizes) != len(ext) {
		return nil, fmt.Errorf("decompressor reported %d sizes for %d extents", len(uSizes), len(ext))
	}
	for i, e := range ext {
		if uSizes[i] != e.USize {
			return nil, fmt.Errorf("extent at %d expands to %d bytes, the inode says %d",
				e.Offset, uSizes[i], e.USize)
		}
	}
	if len(out)-before != uTotal {
		return nil, fmt.Errorf("run of %d extents expands to %d bytes, expected %d",
			len(ext), len(out)-before, uTotal)
	}
	return out, nil
}

// DecompressExtents returns the concatenated contents of a run of extents.
func (im *squashfsImage) DecompressExtents(ctx context.Context, ext []extent) ([]byte, error) {
	uTotal := 0
	for _, e := range ext {
		uTotal += e.USize
	}
	// Decode each maximal compressed run in one call and splice the raw blocks
	// in place. A raw block's bytes are its plaintext, so it never reaches the
	// decompressor.
	out := make([]byte, 0, uTotal)
	for i := 0; i < len(ext); {
		if ext[i].Raw {
			if ext[i].Offset < 0 || ext[i].Offset+int64(ext[i].CSize) > int64(len(im.Data)) {
				return nil, fmt.Errorf("extent at %d (%d bytes) is out of range", ext[i].Offset, ext[i].CSize)
			}
			out = append(out, im.Data[ext[i].Offset:ext[i].Offset+int64(ext[i].CSize)]...)
			i++
			continue
		}
		j := i
		for ; j < len(ext) && !ext[j].Raw; j++ {
		}
		var err error
		if out, err = im.decompressExtentRun(ctx, out, ext[i:j]); err != nil {
			return nil, err
		}
		i = j
	}
	return out, nil
}
