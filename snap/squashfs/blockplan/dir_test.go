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

package blockplan_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type dirSuite struct{}

var _ = Suite(&dirSuite{})

// populateNamed lays down the naming shapes the matcher has to survive: nested
// directories, a version number in a path, a file whose name differs from its
// neighbour only by digits, a hard link giving one inode two names, a dedup pair
// giving two inodes the same blocks, a symlink and a sparse file. Together they
// cover every branch of the tree walk that is not an error.
func populateNamed(c *C, dir string) {
	writeFile(c, dir, "meta/snap.yaml", []byte("name: demo\nversion: 1.2.3\n"))
	writeFile(c, dir, "usr/lib/libdemo.so.1.2.3", compressibleText(300000, "lib"))
	writeFile(c, dir, "usr/lib/python3.12/site-packages/demo/__init__.py", []byte("x = 1\n"))
	writeFile(c, dir, "usr/share/doc/demo/README", compressibleText(5000, "doc"))
	// Same bytes as the library: mksquashfs dedups these onto shared extents,
	// so two paths resolve to two inodes over one set of blocks.
	writeFile(c, dir, "usr/lib/libdemo-copy.so.1.2.3", compressibleText(300000, "lib"))
	writeFile(c, dir, "bin/tool", incompressible(200000, 7))

	c.Assert(os.Symlink("tool", filepath.Join(dir, "bin/tool-link")), IsNil)
	c.Assert(os.Link(filepath.Join(dir, "bin/tool"), filepath.Join(dir, "bin/tool-hard")), IsNil)

	c.Assert(os.MkdirAll(filepath.Join(dir, "var"), 0755), IsNil)
	sparse, err := os.Create(filepath.Join(dir, "var/sparse.bin"))
	c.Assert(err, IsNil)
	c.Assert(sparse.Truncate(300000), IsNil)
	c.Assert(sparse.Close(), IsNil)
}

// TestFileTreeNamesEveryFile is the whole contract the matcher rests on: the
// directory walk must give every data-bearing inode a path, and the blocks it
// reaches through those paths must be exactly the blocks the offset-ordered
// extent list holds. A path map missing a file silently costs delta size; one
// pointing at the wrong blocks would cost correctness, which is why the two
// enumerations are cross-checked here rather than trusted separately.
func (s *dirSuite) TestFileTreeNamesEveryFile(c *C) {
	requireTools(c, "mksquashfs", "xz")
	ctx := context.Background()

	img := buildImage(c, "named.squashfs", populateNamed)
	im, err := blockplan.OpenImage(img)
	c.Assert(err, IsNil)
	meta, err := im.MetaRegionAll(ctx)
	c.Assert(err, IsNil)
	tree, err := im.FileTree(meta)
	c.Assert(err, IsNil, Commentf("walking the directory table"))

	got := make(map[string]*blockplan.FileInode, len(tree))
	for _, e := range tree {
		if prev, dup := got[e.Path]; dup {
			c.Errorf("path %q appears twice, for inodes %d and %d", e.Path, prev.Number, e.Inode.Number)
		}
		got[e.Path] = e.Inode
	}
	want := []string{
		"bin/tool",
		"bin/tool-hard",
		"meta/snap.yaml",
		"usr/lib/libdemo-copy.so.1.2.3",
		"usr/lib/libdemo.so.1.2.3",
		"usr/lib/python3.12/site-packages/demo/__init__.py",
		"usr/share/doc/demo/README",
		"var/sparse.bin",
	}
	for _, p := range want {
		c.Check(got[p], NotNil, Commentf("the tree does not name %q", p))
	}
	// The symlink holds no data blocks, so it must not appear: an entry with a
	// nil inode would fault the matcher.
	c.Check(got["bin/tool-link"], IsNil, Commentf("the tree names a symlink, which has no data blocks"))
	if len(got) != len(want) {
		names := make([]string, 0, len(got))
		for p := range got {
			names = append(names, p)
		}
		sort.Strings(names)
		c.Errorf("the tree names %d files, want %d: %v", len(got), len(want), names)
	}

	// The hard link is one inode under two names, which is exactly what the
	// matcher must tolerate: two paths may legitimately resolve to one file.
	if a, b := got["bin/tool"], got["bin/tool-hard"]; a != nil && b != nil {
		c.Check(b.Number, Equals, a.Number,
			Commentf("the hard link resolved to inode %d rather than %d", b.Number, a.Number))
	}

	// Every inode the export table knows must be reachable by name. This is the
	// check that would catch a walk that quietly stopped early -- the one
	// failure mode that costs delta size without ever producing a wrong image.
	inodes, err := im.FileInodes(ctx)
	c.Assert(err, IsNil)
	named := make(map[uint32]bool, len(got))
	for _, fi := range got {
		named[fi.Number] = true
	}
	for _, fi := range inodes {
		c.Check(named[fi.Number], Equals, true,
			Commentf("inode %d bears data blocks but the tree gives it no name", fi.Number))
	}

	// And the blocks reached through the paths have to be the same blocks the
	// generator emits, or an anchor would point outside the data region.
	ext, err := im.Extents(ctx)
	c.Assert(err, IsNil)
	all := make(map[int64]int, len(ext))
	for _, e := range ext {
		all[e.Offset] = e.CSize
	}
	for path, fi := range got {
		comment := Commentf("%s", path)
		blocks, err := im.InodeExtents(fi)
		c.Assert(err, IsNil, comment)
		var uCovered int64
		for _, b := range blocks {
			cSize, ok := all[b.Offset]
			if !ok {
				c.Errorf("%s has a block at %d that the extent list does not hold", path, b.Offset)
			} else {
				c.Check(cSize, Equals, b.CSize,
					Commentf("%s has a %d-byte block at %d, the extent list says %d",
						path, b.CSize, b.Offset, cSize))
			}
			c.Check(b.UOff, Equals, uCovered,
				Commentf("%s: block at %d claims plaintext offset %d, want %d",
					path, b.Offset, b.UOff, uCovered))
			uCovered = b.UOff + int64(b.USize)
		}
		// The plaintext offsets have to account for the whole file, holes
		// included, since that is the coordinate the matcher anchors on.
		if len(blocks) != 0 {
			c.Check(uCovered, Equals, int64(fi.FileSize),
				Commentf("%s: the blocks cover %d plaintext bytes, the inode says %d",
					path, uCovered, fi.FileSize))
		}
	}
	// The sparse file is all hole, so it has no blocks at all -- and its
	// plaintext offsets are the only thing that would have gone wrong silently.
	if fi := got["var/sparse.bin"]; fi != nil {
		blocks, err := im.InodeExtents(fi)
		c.Assert(err, IsNil)
		c.Check(blocks, HasLen, 0, Commentf("a wholly sparse file yielded blocks"))
	}
}

// TestFileTreeSpansManyDirectoryHeaders covers the listing loop's outer step. A
// directory header carries at most 256 entries and mksquashfs starts a new one
// past that, so a walk that read only the first header would silently lose most
// of a large directory -- the same failure the cross-check above catches, but
// this is where it comes from.
func (s *dirSuite) TestFileTreeSpansManyDirectoryHeaders(c *C) {
	requireTools(c, "mksquashfs", "xz")
	ctx := context.Background()

	const files = 700
	img := buildImage(c, "wide.squashfs", func(c *C, dir string) {
		for i := 0; i < files; i++ {
			// Distinct contents, or dedup would collapse them and the test
			// would prove less than it looks.
			writeFile(c, dir, fmt.Sprintf("many/f%03d.txt", i),
				compressibleText(2000+i, fmt.Sprintf("f%d", i)))
		}
	})
	im, err := blockplan.OpenImage(img)
	c.Assert(err, IsNil)
	meta, err := im.MetaRegionAll(ctx)
	c.Assert(err, IsNil)
	tree, err := im.FileTree(meta)
	c.Assert(err, IsNil, Commentf("walking a %d-entry directory", files))

	n := 0
	for _, e := range tree {
		if strings.HasPrefix(e.Path, "many/") {
			n++
		}
	}
	c.Check(n, Equals, files, Commentf("the walk did not find every entry in one directory"))
}

// synthListing wraps a run of listing bytes as a one-block metadata region, with
// the listing that describes it: relative offset 0 is both the inode table's
// first block and -- with a directory table at relative 0 -- the directory
// table's.
func synthListing(entries []byte) (*blockplan.MetaRegion, blockplan.DirListing) {
	return blockplan.NewMetaRegion(entries),
		blockplan.DirListing{StartBlock: 0, Offset: 0, Size: uint32(len(entries)) + 3}
}

// dirEntryBytes assembles a header covering one entry, the shape everything in
// the refusal table starts from.
func dirEntryBytes(name string, count uint32, nameLen int) []byte {
	buf := make([]byte, 12+8+len(name))
	le := binary.LittleEndian
	le.PutUint32(buf[0:], count) // stored one less than the true count
	le.PutUint32(buf[4:], 0)     // inode table block
	le.PutUint32(buf[8:], 1)     // base inode number
	le.PutUint16(buf[12:], 0)    // inode offset within the block
	le.PutUint16(buf[14:], 0)    // inode number delta
	le.PutUint16(buf[16:], blockplan.InodeTypeFile)
	le.PutUint16(buf[18:], uint16(nameLen-1)) // stored one less
	copy(buf[20:], name)
	return buf
}

// TestReadDirListingRefusals covers the bounds checks. Each row is a malformed
// listing that, unchecked, would either read past the metadata region or produce
// a path the matcher would key on -- and since the listing arrives from an image
// the generator is being asked to trust, the difference between a refusal and a
// wrong answer is the check itself.
func (s *dirSuite) TestReadDirListingRefusals(c *C) {
	var im blockplan.Image

	tests := []struct {
		name    string
		entries []byte
		// list overrides the listing synthesized for entries, for the rows
		// where the listing itself is what is malformed.
		list *blockplan.DirListing
		err  string
	}{{
		// The declared name is longer than the bytes that follow it, which is
		// the one overrun that would hand the caller somebody else's metadata
		// as a filename.
		name:    "a name that runs past the listing",
		entries: dirEntryBytes("file.txt", 0, len("file.txt")+40),
		err:     `a 48-byte name at 20 runs past the listing's end at 28`,
	}, {
		name:    "a header claiming five entries in a one-entry listing",
		entries: dirEntryBytes("file.txt", 4, len("file.txt")),
		err:     `entry 1 of a directory header at 28 runs past the listing's end`,
	}, {
		name:    "a truncated header",
		entries: dirEntryBytes("file.txt", 0, len("file.txt"))[:8],
		err:     `a directory header at 0 runs past the listing's end at 8`,
	}, {
		// A name holding a separator would make two different trees produce
		// the same path, which is precisely what the matcher must not be fed.
		name:    "a separator in a name",
		entries: dirEntryBytes("a/b", 0, len("a/b")),
		err:     `a directory entry name contains '/': "a/b"`,
	}, {
		name:    "a listing naming its own parent",
		entries: dirEntryBytes("..", 0, len("..")),
		err:     `a directory entry is named ".."`,
	}, {
		name: "a size below the empty-directory minimum",
		list: &blockplan.DirListing{StartBlock: 0, Offset: 0, Size: 2},
		err:  `listing declares 2 bytes, below the 3 an empty directory has`,
	}, {
		name: "a listing in a block outside the region",
		list: &blockplan.DirListing{StartBlock: 8192, Offset: 0, Size: 3},
		err:  `listing names directory table block 8192, which is not in the region`,
	}}

	for _, tc := range tests {
		comment := Commentf("%s", tc.name)
		meta, list := synthListing(tc.entries)
		if tc.list != nil {
			list = *tc.list
		}
		_, err := im.ReadDirListing(meta, 0, list)
		c.Check(err, ErrorMatches, tc.err, comment)
	}
}

// The two listings that must be read rather than refused.
func (s *dirSuite) TestReadDirListingAcceptsWhatMksquashfsWrites(c *C) {
	var im blockplan.Image

	meta, list := synthListing(dirEntryBytes("file.txt", 0, len("file.txt")))
	got, err := im.ReadDirListing(meta, 0, list)
	c.Assert(err, IsNil, Commentf("a well-formed listing was refused"))
	c.Assert(got, HasLen, 1)
	c.Check(got[0].Name, Equals, "file.txt")

	// file_size 3 with no bytes stored is how an empty directory is expressed,
	// and it has to read as empty rather than as an error.
	meta, list = synthListing(nil)
	got, err = im.ReadDirListing(meta, 0, list)
	c.Assert(err, IsNil, Commentf("an empty directory was refused"))
	c.Check(got, HasLen, 0)
}
