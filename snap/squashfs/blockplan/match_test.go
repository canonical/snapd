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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

type matchSuite struct{}

var _ = Suite(&matchSuite{})

// TestNormalizeDigitsAgreesWithFuzzyMatch pins the equivalence the bucket index
// rests on: two paths land in the same bucket exactly when pathsMatchFuzzy
// accepts them. If they ever diverged, the matcher would silently stop finding
// version bumps -- a bucket miss is not an error, it is just a worse delta -- so
// the agreement is asserted rather than assumed.
func (s *matchSuite) TestNormalizeDigitsAgreesWithFuzzyMatch(c *C) {
	pairs := []struct {
		a, b  string
		match bool
	}{
		{"usr/lib/libdemo.so.1.2.3", "usr/lib/libdemo.so.1.2.3", true},
		{"usr/lib/libdemo.so.1.2.3", "usr/lib/libdemo.so.1.2.4", true},
		// A digit run collapses to one placeholder, so a version going from one
		// digit to two still matches -- which is the 9 -> 10 case every project
		// eventually hits.
		{"usr/lib/libdemo.so.1.2.9", "usr/lib/libdemo.so.1.2.10", true},
		{"usr/lib/python3.12/x.py", "usr/lib/python3.13/x.py", true},
		{"usr/lib/python3.12/x.py", "usr/lib/python3.12/y.py", false},
		// Same name, different depth: a file that moved between directories is
		// not something either side is prepared to call a match.
		{"usr/lib/x.so", "usr/local/lib/x.so", false},
		// A digit appearing where there was none is not a version bump: only a
		// digit run changing value is, so tool and tool2 stay distinct files.
		{"bin/tool", "bin/tool2", false},
		{"bin/tool2", "bin/tool3", true},
		{"bin/tool", "bin/other", false},
		{"a/b/c", "a/b/c", true},
		{"", "", true},
	}
	for _, p := range pairs {
		gotFuzzy := blockplan.PathsMatchFuzzy(p.a, p.b) != 0
		c.Check(gotFuzzy, Equals, p.match, Commentf("%q vs %q", p.a, p.b))

		normA, normB := blockplan.NormalizeDigits(p.a), blockplan.NormalizeDigits(p.b)
		c.Check(normA == normB, Equals, gotFuzzy,
			Commentf("%q and %q normalize to %q and %q, but the fuzzy match says %v",
				p.a, p.b, normA, normB, gotFuzzy))
	}
}

// TestSizeSimilarity covers the ranking metric. It has to answer how close two
// files are rather than whether they are the same size, because it is what picks
// between several candidates for a renamed file.
func (s *matchSuite) TestSizeSimilarity(c *C) {
	cases := []struct {
		a, b int64
		want int
	}{
		{100, 100, 100},
		{0, 0, 100},
		{0, 100, 0},
		{100, 0, 0},
		{50, 100, 50},
		{100, 50, 50},
		{99, 100, 99},
		{75, 100, 75},
	}
	for _, t := range cases {
		c.Check(blockplan.SizeSimilarity(t.a, t.b), Equals, t.want, Commentf("%d against %d", t.a, t.b))
	}
}

// TestFileLayoutUOffAt covers the step from a plaintext offset to a source
// offset, holes and short files included.
func (s *matchSuite) TestFileLayoutUOffAt(c *C) {
	// A file with a hole in the middle: the second block's plaintext starts at
	// 200 even though it directly follows the first on disk.
	f := &blockplan.FileLayout{
		Path:  "x",
		USize: 300,
		Blocks: []blockplan.FileBlock{
			blockplan.NewFileBlock(1000, 40, 100, 0),
			blockplan.NewFileBlock(1040, 50, 100, 200),
		},
	}
	cases := []struct {
		uOff int64
		want int64
	}{
		{0, 1000},
		{99, 1000},
		// Inside the hole. The next stored block is the honest answer: the hole
		// itself has no bytes to diff against.
		{150, 1040},
		{200, 1040},
		{299, 1040},
		// Past the end, which is what a file that grew looks like from the
		// target's side. The last block is as close as the source gets.
		{5000, 1040},
	}
	for _, t := range cases {
		got, ok := f.UOffAt(t.uOff)
		c.Assert(ok, Equals, true, Commentf("plaintext offset %d found nothing", t.uOff))
		c.Check(got, Equals, t.want, Commentf("plaintext offset %d", t.uOff))
	}

	// A wholly sparse or empty file has nothing to anchor on, and saying so is
	// what sends the caller to its fallback rather than to offset zero.
	empty := &blockplan.FileLayout{Path: "hole", USize: 4096}
	off, ok := empty.UOffAt(0)
	c.Check(ok, Equals, false, Commentf("a file with no blocks yielded an anchor at %d", off))
}

// TestAnchorPrefersExactPath drives the decision table directly, without an
// image: which of the two indexes answers, and when neither does.
func (s *matchSuite) TestAnchorPrefersExactPath(c *C) {
	block := func(off, uOff int64) blockplan.FileBlock {
		return blockplan.NewFileBlock(off, 100, 1000, uOff)
	}
	src := []*blockplan.FileLayout{
		{Path: "usr/lib/libdemo.so.1.2.3", USize: 2000,
			Blocks: []blockplan.FileBlock{block(500, 0), block(600, 1000)}},
		{Path: "bin/tool", USize: 1000, Blocks: []blockplan.FileBlock{block(700, 0)}},
		// Same normalized path as the target's libother.so.8 below, but a fifth
		// of the size: a coincidence, not a version bump.
		{Path: "usr/lib/libother.so.9", USize: 400, Blocks: []blockplan.FileBlock{block(800, 0)}},
		// Present under the right name but holding no blocks, which is what a
		// file truncated to a hole looks like.
		{Path: "var/emptied.bin", USize: 0},
	}
	// Every target file here holds one block, whose image offset is the offset
	// an anchor is asked for and whose plaintext offset is where in the file
	// that block sits.
	tgt := []*blockplan.FileLayout{
		// Exact path, second block: must land on the source's second block.
		{Path: "usr/lib/libdemo.so.1.2.3", USize: 2000,
			Blocks: []blockplan.FileBlock{block(10000, 1000)}},
		// A version bump of the same library.
		{Path: "usr/lib/libdemo.so.1.2.4", USize: 2100,
			Blocks: []blockplan.FileBlock{block(20000, 0)}},
		// Same shape as libother.so.9 but nowhere near its size.
		{Path: "usr/lib/libother.so.8", USize: 4000,
			Blocks: []blockplan.FileBlock{block(30000, 0)}},
		// A file the source does not have at all.
		{Path: "usr/lib/libnew.so", USize: 1000,
			Blocks: []blockplan.FileBlock{block(40000, 0)}},
		// Named in the source, but that copy has no blocks to diff against.
		{Path: "var/emptied.bin", USize: 9000,
			Blocks: []blockplan.FileBlock{block(50000, 0)}},
	}
	m := blockplan.NewPathMatcherFor(src, tgt)

	cases := []struct {
		tgtOff   int64
		wantOff  int64
		wantKind blockplan.AnchorKind
	}{
		{10000, 600, blockplan.AnchorPath},
		{20000, 500, blockplan.AnchorFuzzy},
		{30000, 0, blockplan.AnchorNone},
		{40000, 0, blockplan.AnchorNone},
		{50000, 0, blockplan.AnchorNone},
		// A block belonging to no file -- the walk missed its inode -- has no
		// correspondence to offer.
		{99999, 0, blockplan.AnchorNone},
	}
	for _, t := range cases {
		cmt := Commentf("target offset %d", t.tgtOff)
		gotOff, gotKind := m.Anchor(t.tgtOff)
		c.Check(gotKind, Equals, t.wantKind, cmt)
		if gotKind != blockplan.AnchorNone {
			c.Check(gotOff, Equals, t.wantOff, cmt)
		}
	}

	// A nil matcher stands for an image whose directory table would not walk,
	// and has to answer rather than fault: generation continues without it.
	var absent *blockplan.PathMatcher
	_, kind := absent.Anchor(10000)
	c.Check(kind, Equals, blockplan.AnchorNone)
}
