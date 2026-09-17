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
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// --- fixtures shared by every suite in the package ---

// testBlockSize is the squashfs data block size snapd packs with, and so the
// dictionary size every block in a snap was compressed with.
const testBlockSize = 131072

// requireTools skips unless every named tool can be found. A machine without
// them is not a failing machine: it is one that cannot make or apply a delta,
// which this package reports as a refusal and the store answers by downloading
// the snap whole.
func requireTools(c *C, names ...string) {
	for _, n := range names {
		if !blockplan.HaveTool(n) {
			c.Skip(fmt.Sprintf("%s is not available", n))
		}
	}
}

// incompressible returns bytes no compressor can shrink, so mksquashfs stores
// the block raw -- the case where cSize == uSize and the compressed bit is set.
func incompressible(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

// compressibleText returns highly compressible bytes, so the block is stored
// compressed and has cSize far below uSize.
func compressibleText(n int, tag string) []byte {
	out := make([]byte, 0, n+64)
	for len(out) < n {
		out = append(out, "the quick brown fox jumps over the lazy dog "+tag+"\n"...)
	}
	return out[:n]
}

// firstDiff is the offset of the first differing byte, or the shorter length.
// Block plans deal in megabytes, where the offset of a mismatch says far more
// than a dump of both sides would.
func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// --- image fixtures ---
//
// These build real squashfs images with mksquashfs rather than using checked-in
// ones, so the tests exercise the same producer the store does and stay honest
// when squashfs-tools changes.

// snapdMksquashfsArgs is exactly how snapd packs an app snap -- see Build in
// snap/squashfs/squashfs.go. The delta format is designed against these options,
// so the fixtures must use them and nothing else.
var snapdMksquashfsArgs = []string{
	"-noappend", "-comp", "xz", "-no-fragments", "-no-progress", "-all-root", "-no-xattrs",
}

// buildImage populates a directory through populate and packs it the way snapd
// does, returning the image path. extra is appended, which is how a test asks
// for something snapd's own options do not already cover.
func buildImage(c *C, name string, populate func(c *C, dir string), extra ...string) string {
	return buildImageArgs(c, name, populate, append(append([]string{}, snapdMksquashfsArgs...), extra...)...)
}

// buildImageArgs packs a tree under an explicit argument list. The refusal tests
// need this rather than extra arguments, because mksquashfs refuses two
// conflicting -comp options outright and silently keeps -no-fragments whatever
// follows it.
func buildImageArgs(c *C, name string, populate func(c *C, dir string), args ...string) string {
	requireTools(c, "mksquashfs")

	root := c.MkDir()
	tree := filepath.Join(root, "tree")
	c.Assert(os.MkdirAll(tree, 0755), IsNil)
	populate(c, tree)

	img := filepath.Join(root, name)
	cmd, err := blockplan.ToolCommand(context.Background(), "mksquashfs",
		append([]string{tree, img}, args...)...)
	c.Assert(err, IsNil)
	out, err := cmd.CombinedOutput()
	c.Assert(err, IsNil, Commentf("mksquashfs %v failed: %s", args, out))
	return img
}

// writeFile is a populate helper; mode 0644 throughout, since -all-root
// normalizes ownership anyway.
func writeFile(c *C, dir, name string, data []byte) {
	full := filepath.Join(dir, name)
	c.Assert(os.MkdirAll(filepath.Dir(full), 0755), IsNil)
	c.Assert(os.WriteFile(full, data, 0644), IsNil)
}

// populateMixed lays down every block shape the extent walk has to handle:
// several full compressed blocks plus a partial tail, a raw block, a wholly
// sparse file (extended inode, zero size words), a duplicate that mksquashfs
// dedups onto shared extents, and a hard link so one inode has two names.
func populateMixed(c *C, dir string) {
	writeFile(c, dir, "multi.txt", compressibleText(400000, "multi"))
	writeFile(c, dir, "raw.bin", incompressible(200000, 1))
	writeFile(c, dir, "sub/small.txt", []byte("hello world\n"))
	// Same bytes as multi.txt: dedup makes both inodes share extents.
	writeFile(c, dir, "sub/dup.txt", compressibleText(400000, "multi"))

	f, err := os.Create(filepath.Join(dir, "sparse.bin"))
	c.Assert(err, IsNil)
	c.Assert(f.Truncate(300000), IsNil)
	c.Assert(f.Close(), IsNil)

	c.Assert(os.Link(filepath.Join(dir, "sub/small.txt"), filepath.Join(dir, "sub/link.txt")), IsNil)
}
