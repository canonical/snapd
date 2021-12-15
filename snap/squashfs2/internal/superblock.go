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

package internal

import (
	"fmt"
)

const (
	SuperBlockSize = 96

	SuperBlockUncompressedInodes    = 0x1
	SuperBlockUncompressedData      = 0x2
	SuperBlockUncompressedFragments = 0x8
	SuperBlockNoFragments           = 0x10
	SuperBlockAlwaysFragments       = 0x20
	SuperBlockDublicates            = 0x40
	SuperBlockExportable            = 0x80
	SuperBlockUncompressedXattrs    = 0x100
	SuperBlockNoXattrs              = 0x200
	SuperBlockCompressorOptions     = 0x400
	SuperBlockUncompressedIds       = 0x800

	// Compression types supported by squashfs
	CompressionZlib = 1
	CompressionLzma = 2
	CompressionLzo  = 3
	CompressionXz   = 4
	CompressionLz4  = 5
	CompressionZstd = 6
)

var (
	superBlockMagic = [4]byte{'h', 's', 'q', 's'}
)

type MetadataRef struct {
	Offset int
	Block  int64
}

type SuperBlock struct {
	magic           [4]byte
	Inodes          uint32
	MkfsTime        uint32
	BlockSize       uint32
	Fragments       uint32
	CompressionType uint16
	BlockSizeLog2   uint16
	Flags           uint16
	NoIDs           uint16
	Smajor          uint16
	Sminor          uint16
	RootIno         MetadataRef
	BytesUsed       int64
	IdTableStart    int64
	XattrIdTableSz  int64
	InodeTable      int64
	DirectoryTable  int64
	FragmentTable   int64
	LookupTable     int64
}

func readInodeRef(data []byte) MetadataRef {
	value := ReadInt64(data)
	return MetadataRef{
		Offset: int(value & 0xffff),
		Block:  value >> 16,
	}
}

func (sb *SuperBlock) Parse(data []byte) error {
	if len(data) < SuperBlockSize {
		return fmt.Errorf("squashfs: superblock too small")
	}

	copy(sb.magic[:], data[:4])
	if sb.magic != superBlockMagic {
		return fmt.Errorf("squashfs: invalid magic")
	}

	sb.Inodes = ReadUint32(data[4:])
	sb.MkfsTime = ReadUint32(data[8:])
	sb.BlockSize = ReadUint32(data[12:])
	sb.Fragments = ReadUint32(data[16:])
	sb.CompressionType = ReadUint16(data[20:])
	sb.BlockSizeLog2 = ReadUint16(data[22:])
	sb.Flags = ReadUint16(data[24:])
	sb.NoIDs = ReadUint16(data[26:])
	sb.Smajor = ReadUint16(data[28:])
	sb.Sminor = ReadUint16(data[30:])
	sb.RootIno = readInodeRef(data[32:])
	sb.BytesUsed = ReadInt64(data[40:])
	sb.IdTableStart = ReadInt64(data[48:])
	sb.XattrIdTableSz = ReadInt64(data[56:])
	sb.InodeTable = ReadInt64(data[64:])
	sb.DirectoryTable = ReadInt64(data[72:])
	sb.FragmentTable = ReadInt64(data[80:])
	sb.LookupTable = ReadInt64(data[88:])
	return nil
}
