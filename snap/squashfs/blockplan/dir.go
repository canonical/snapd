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
	"encoding/binary"
	"fmt"
)

// Walking the directory table gives every file a name, which is the only
// durable identity a file has across two revisions of a snap. Inode numbers are
// assigned in scan order, so inserting one file renumbers everything after it;
// data offsets move for the same reason. A path does not.
//
// Nothing in the *format* needs this: the applier carries the metadata region
// verbatim and never looks up a name. It exists purely so the generator can
// decide which source bytes a changed target run should be diffed against --
// see match.go, which is the only caller.
//
// The layout read here is the tail of the metadata region, [dir_table_start,
// export_table_start). It is a sequence of listings, one per directory, each
// addressed by its directory inode:
//
//	dir header (12 B)  count-1 | start_block | base inode number
//	entry (8 B + name) offset  | inode delta | type | name len-1 | name
//
// start_block and offset point into the *inode* table, so an entry resolves to
// an inode reference exactly as the lookup table's do. A listing runs for
// file_size-3 bytes and may cross metadata block boundaries, which costs nothing
// here because a metaRegion holds the whole region as one contiguous blob.

const (
	// inodeTypeDir and inodeTypeExtDir are the two directory inode types.
	inodeTypeDir    = 1
	inodeTypeExtDir = 8

	// squashfsDirCountMax is SQUASHFS_DIR_COUNT: the most entries one
	// directory header may cover before mksquashfs starts a new one.
	squashfsDirCountMax = 256
)

// fileEntry is one regular file: the path the directory tree gives it, and the
// inode that says where its data lives.
type fileEntry struct {
	Path  string
	Inode *fileInode
}

// dirListing is where a directory inode says its entries are.
type dirListing struct {
	// StartBlock is a metadata block offset relative to dir_table_start.
	StartBlock uint32
	// Offset is the byte position within that block.
	Offset uint16
	// Size is the inode's file_size, which counts three bytes that are not
	// stored: an empty directory has file_size 3.
	Size uint32
}

// FileTree returns every regular file reachable from the root inode with its
// full path, breadth-first, so the order is stable for a given image.
//
// meta must be the whole metadata region -- MetaRegionAll -- because the walk
// resolves references into both the inode table and the directory table, and
// those are two ends of the same blob.
func (im *squashfsImage) FileTree(meta *metaRegion) ([]fileEntry, error) {
	if meta.Start != int64(im.SB.InodeTableStart) {
		return nil, fmt.Errorf("the file tree needs the whole metadata region, but this one starts at %d rather than %d",
			meta.Start, im.SB.InodeTableStart)
	}
	dirRel := int64(im.SB.DirTableStart) - meta.Start
	if dirRel < 0 || dirRel > int64(len(im.Data)) {
		return nil, fmt.Errorf("directory table at %d is not inside the metadata region starting at %d",
			im.SB.DirTableStart, meta.Start)
	}

	type pending struct {
		ref  uint64
		path string
	}
	queue := []pending{{ref: im.SB.RootInodeRef, path: ""}}
	// A directory reached twice would mean a cycle, which a walk cannot
	// survive. Refusing is right: it is a corrupt image, not a layout this
	// delta declines to handle.
	seen := map[uint64]bool{im.SB.RootInodeRef: true}
	var out []fileEntry

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		list, err := im.dirListingAt(meta, cur.ref)
		if err != nil {
			return nil, fmt.Errorf("directory %q: %w", "/"+cur.path, err)
		}
		children, err := im.readDirListing(meta, dirRel, list)
		if err != nil {
			return nil, fmt.Errorf("directory %q: %w", "/"+cur.path, err)
		}
		for _, c := range children {
			path := c.Name
			if cur.path != "" {
				path = cur.path + "/" + c.Name
			}
			pos, err := resolveInodeRef(meta, c.Ref)
			if err != nil {
				return nil, fmt.Errorf("entry %q: %w", "/"+path, err)
			}
			if pos+2 > len(meta.Blob) {
				return nil, fmt.Errorf("entry %q: inode at %d is out of range", "/"+path, pos)
			}
			switch binary.LittleEndian.Uint16(meta.Blob[pos:]) {
			case inodeTypeDir, inodeTypeExtDir:
				if seen[c.Ref] {
					return nil, fmt.Errorf("directory %q at reference %#x has already been walked, so the tree has a cycle",
						"/"+path, c.Ref)
				}
				seen[c.Ref] = true
				queue = append(queue, pending{ref: c.Ref, path: path})
			case inodeTypeFile, inodeTypeExtFile:
				fi, err := im.parseFileInode(meta.Blob, pos)
				if err != nil {
					return nil, fmt.Errorf("entry %q: %w", "/"+path, err)
				}
				out = append(out, fileEntry{Path: path, Inode: fi})
			default:
				// Symlinks, devices, fifos and sockets hold no data
				// blocks, so they are nothing to this walk.
			}
		}
	}
	return out, nil
}

// resolveInodeRef turns an inode reference into a position in the region's blob.
// The reference packs a metadata block offset -- relative to the region's start
// -- in its upper 48 bits and a byte offset within that block in its lower 16.
func resolveInodeRef(meta *metaRegion, ref uint64) (int, error) {
	blk := (ref >> 16) & 0xFFFFFFFFFFFF
	base, ok := meta.index[blk]
	if !ok {
		return 0, fmt.Errorf("reference %#x names metadata block %d, which is not in the region", ref, blk)
	}
	return base + int(ref&0xFFFF), nil
}

// dirListingAt decodes the directory inode at ref, returning where its listing
// lives. It refuses any other inode type, since a caller that got here believes
// it is holding a directory.
func (im *squashfsImage) dirListingAt(meta *metaRegion, ref uint64) (dirListing, error) {
	pos, err := resolveInodeRef(meta, ref)
	if err != nil {
		return dirListing{}, err
	}
	blob := meta.Blob
	if pos < 0 || pos+16 > len(blob) {
		return dirListing{}, fmt.Errorf("inode at %d is out of range", pos)
	}
	le := binary.LittleEndian
	switch le.Uint16(blob[pos:]) {
	case inodeTypeDir:
		// type|mode|uid|gid|mtime|inode_number, then start_block, nlink,
		// file_size (2 bytes here), offset, parent_inode: 32 in total.
		if pos+32 > len(blob) {
			return dirListing{}, fmt.Errorf("basic directory inode at %d is truncated", pos)
		}
		return dirListing{
			StartBlock: le.Uint32(blob[pos+16:]),
			Size:       uint32(le.Uint16(blob[pos+24:])),
			Offset:     le.Uint16(blob[pos+26:]),
		}, nil
	case inodeTypeExtDir:
		// The extended form reorders the fields and widens file_size to
		// four bytes, then carries an index this walk does not need: the
		// index only exists to make a lookup in a large directory skip
		// ahead, and a full walk reads every entry anyway.
		if pos+40 > len(blob) {
			return dirListing{}, fmt.Errorf("extended directory inode at %d is truncated", pos)
		}
		return dirListing{
			StartBlock: le.Uint32(blob[pos+24:]),
			Size:       le.Uint32(blob[pos+20:]),
			Offset:     le.Uint16(blob[pos+34:]),
		}, nil
	default:
		return dirListing{}, fmt.Errorf("inode at %d is type %d, not a directory", pos, le.Uint16(blob[pos:]))
	}
}

// dirChild is one entry in a directory listing.
type dirChild struct {
	Name string
	// Ref is the entry's inode reference, assembled from its header's block
	// and the entry's own offset.
	Ref uint64
}

// readDirListing decodes one directory's entries. It reads the inode types
// through the references rather than the type field each entry carries, because
// that field is only ever used for a d_type hint and nothing guarantees it
// distinguishes a basic inode from an extended one.
func (im *squashfsImage) readDirListing(meta *metaRegion, dirRel int64, list dirListing) ([]dirChild, error) {
	base, ok := meta.index[uint64(dirRel)+uint64(list.StartBlock)]
	if !ok {
		return nil, fmt.Errorf("listing names directory table block %d, which is not in the region", list.StartBlock)
	}
	if list.Size < 3 {
		return nil, fmt.Errorf("listing declares %d bytes, below the 3 an empty directory has", list.Size)
	}
	blob := meta.Blob
	pos := base + int(list.Offset)
	end := pos + int(list.Size) - 3
	if pos < 0 || end > len(blob) || pos > end {
		return nil, fmt.Errorf("listing spans [%d,%d), outside the %d-byte metadata region", pos, end, len(blob))
	}

	le := binary.LittleEndian
	var out []dirChild
	for pos < end {
		if pos+12 > end {
			return nil, fmt.Errorf("a directory header at %d runs past the listing's end at %d", pos, end)
		}
		// Stored one less than the true count, so a header always covers at
		// least one entry.
		count := int(le.Uint32(blob[pos:])) + 1
		hdrBlock := le.Uint32(blob[pos+4:])
		pos += 12
		if count > squashfsDirCountMax {
			return nil, fmt.Errorf("a directory header covers %d entries, over the %d limit", count, squashfsDirCountMax)
		}
		for i := 0; i < count; i++ {
			if pos+8 > end {
				return nil, fmt.Errorf("entry %d of a directory header at %d runs past the listing's end", i, pos)
			}
			entryOff := le.Uint16(blob[pos:])
			nameLen := int(le.Uint16(blob[pos+6:])) + 1
			pos += 8
			if pos+nameLen > end {
				return nil, fmt.Errorf("a %d-byte name at %d runs past the listing's end at %d", nameLen, pos, end)
			}
			name := string(blob[pos : pos+nameLen])
			pos += nameLen
			// A name that is empty or holds a separator would make the
			// paths this walk builds ambiguous, which is the one thing the
			// matcher relies on them not being.
			if name == "" || name == "." || name == ".." {
				return nil, fmt.Errorf("a directory entry is named %q", name)
			}
			for _, b := range []byte(name) {
				if b == '/' || b == 0 {
					return nil, fmt.Errorf("a directory entry name contains %q: %q", b, name)
				}
			}
			out = append(out, dirChild{
				Name: name,
				Ref:  uint64(hdrBlock)<<16 | uint64(entryOff),
			})
		}
	}
	return out, nil
}
