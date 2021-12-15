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

import "errors"

const (
	// Inode types supported by squashfs
	InodeTypeDirectory         = 1
	InodeTypeFile              = 2
	InodeTypeSymlink           = 3
	InodeTypeBlockDev          = 4
	InodeTypeCharDev           = 5
	InodeTypeFifo              = 6
	InodeTypeSocket            = 7
	InodeTypeExtendedDirectory = 8
	InodeTypeExtendedFile      = 9
	InodeTypeExtendedSymlink   = 10
	InodeTypeExtendedBlockDev  = 11
	InodeTypeExtendedCharDev   = 12
	InodeTypeExtendedFifo      = 13
	InodeTypeExtendedSocket    = 14
)

// https://github.com/plougher/squashfs-tools/blob/master/squashfs-tools/squashfs_fs.h#L289
type Inode struct {
	itype uint16
	mode  uint16
	uid   uint16
	gid   uint16
	mtime uint32
	ino   uint32
}

type InodeBlkDev struct {
	Base   Inode
	Nlinks uint32
	DevId  uint32
}

type InodeDir struct {
	Base       Inode
	StartBlock uint32
	Nlinks     uint32
	Size       uint16
	Offset     uint16
	ParentIno  uint32
}

type InodeDirExt struct {
	Base        Inode
	Nlinks      uint32
	Size        uint32
	StartBlock  uint32
	ParentInode uint32
	Indices     uint16
	Offset      uint16
	Xattribs    uint32
}

type InodeReg struct {
	Base       Inode
	StartBlock uint32
	Fragment   uint32
	Offset     uint32
	Size       uint32
	BlockSizes []uint32
}

type InodeSymlink struct {
	Base    Inode
	Nlinks  uint32
	Size    uint32
	Symlink string
}

func parseInode(data []byte) Inode {
	node := Inode{}
	node.itype = ReadUint16(data[0:])
	node.mode = ReadUint16(data[2:])
	node.uid = ReadUint16(data[4:])
	node.gid = ReadUint16(data[6:])
	node.mtime = ReadUint32(data[8:])
	node.ino = ReadUint32(data[12:])
	return node
}

func (n *InodeDir) Parse(data []byte) {
	n.Base = parseInode(data)
	n.StartBlock = ReadUint32(data[16:])
	n.Nlinks = ReadUint32(data[20:])
	n.Size = ReadUint16(data[24:])
	n.Offset = ReadUint16(data[26:])
	n.ParentIno = ReadUint32(data[28:])
}

func (n *InodeReg) Parse(data []byte) {
	n.Base = parseInode(data)
	n.StartBlock = ReadUint32(data[16:])
	n.Fragment = ReadUint32(data[20:])
	n.Offset = ReadUint32(data[24:])
	n.Size = ReadUint32(data[28:])

	// read the blocksize table into the struct
	for i := 32; i < len(data); i += 4 {
		blockSize := ReadUint32(data[i:])
		n.BlockSizes = append(n.BlockSizes, blockSize)
	}
}

type DirectoryHeader struct {
	Count      uint32
	StartBlock uint32
	Ino        uint32
}

type DirectoryEntry struct {
	StartBlock uint32
	Offset     uint16
	Ino        int16
	Itype      uint16
	Size       uint16
	Name       string
}

func (dh *DirectoryHeader) Parse(data []byte) error {
	if len(data) < 12 {
		return &ParseError{
			Stype: "DirectoryHeader",
			Err:   errors.New("data too short"),
		}
	}

	dh.Count = ReadUint32(data[0:])
	dh.StartBlock = ReadUint32(data[4:])
	dh.Ino = ReadUint32(data[8:])
	return nil
}

func (de *DirectoryEntry) Parse(data []byte) error {
	if len(data) < 8 {
		return &ParseError{
			Stype: "DirectoryEntry",
			Err:   errors.New("data too short"),
		}
	}

	de.Offset = ReadUint16(data[0:])
	de.Ino = ReadInt16(data[2:])
	de.Itype = ReadUint16(data[4:])
	de.Size = ReadUint16(data[6:])
	return nil
}

func (de *DirectoryEntry) IsDirectory() bool {
	return de.Itype == InodeTypeDirectory || de.Itype == InodeTypeExtendedDirectory
}

func (de *DirectoryEntry) IsSymlink() bool {
	return de.Itype == InodeTypeSymlink || de.Itype == InodeTypeExtendedSymlink
}

func (de *DirectoryEntry) IsRegularFile() bool {
	return de.Itype == InodeTypeFile
}
