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
	"sort"
	"strings"
)

// This is the generator's answer to the one question a patch run cannot avoid:
// which source bytes should this changed target run be diffed against?
//
// The obvious answer is "wherever the last matched block left off", and it is
// what the generator falls back to. It works because consecutive revisions keep a
// near-monotonic layout, and it fails exactly where a delta is worth the most:
// when a file moves in the layout, when a directory is renamed, when a version
// number in a path changes, or when so much churned that the copies bracketing a
// run are far from the source's version of it. In those cases the run is diffed
// against unrelated bytes, the patch comes out no smaller than the literals it
// would replace, and the cost model correctly declines it -- so the delta grows
// with no warning that a better window was sitting right there.
//
// A path is the durable identity here. Inode numbers are assigned in scan order,
// so inserting one file renumbers every file after it; data offsets move for the
// same reason. A name does not. So both images are walked into path -> blocks
// maps, and a run's anchor becomes "the same plaintext offset of the file of the
// same name", with a fuzzy pass for the renames and version bumps that a snap
// revision is full of.
//
// None of this reaches the delta. It changes which window a patch is computed
// against, and nothing else: the applier still receives absolute offsets and
// still has no idea files exist.

// fileLayout is one file's identity and its blocks in file order.
type fileLayout struct {
	Path string
	// USize is the file's plaintext length, which is what the fuzzy pass
	// compares -- two revisions of the same file are close in size far more
	// reliably than they are in compressed size.
	USize int64
	// Blocks are the file's data blocks in ascending plaintext offset. A
	// wholly sparse file has none.
	Blocks []fileBlock
}

// uOffAt returns the image offset of the block holding the file's plaintext at
// uOff, which is the anchor a window is grown from.
func (f *fileLayout) uOffAt(uOff int64) (int64, bool) {
	if len(f.Blocks) == 0 {
		return 0, false
	}
	i := sort.Search(len(f.Blocks), func(k int) bool {
		b := f.Blocks[k]
		return b.UOff+int64(b.USize) > uOff
	})
	if i == len(f.Blocks) {
		// The source file is shorter than the target's offset, which is what a
		// file that grew looks like. Its last block is the closest thing to a
		// correspondence there is, and the window grows forward from there
		// across whatever followed the file -- which, if it grew, is where its
		// new bytes came from.
		return f.Blocks[len(f.Blocks)-1].Offset, true
	}
	return f.Blocks[i].Offset, true
}

// blockOwner says which file a target block belongs to, and where in that file.
type blockOwner struct {
	file *fileLayout
	uOff int64
}

// anchorKind records how a run's source anchor was found, so a generate can
// report what the path map is actually buying.
type anchorKind int

const (
	// anchorNone means no correspondence was found and the caller's own
	// fallback stands.
	anchorNone anchorKind = iota
	// anchorPath is an exact path match: the same file in both revisions.
	anchorPath
	// anchorFuzzy is a path that differs only in its digits -- a version
	// bump -- and whose size is close enough to believe.
	anchorFuzzy
)

// String names the anchor for the generator's summary.
func (k anchorKind) String() string {
	switch k {
	case anchorPath:
		return "path"
	case anchorFuzzy:
		return "fuzzy"
	default:
		return "cursor"
	}
}

// minFuzzySizeSimilarity is how close two files' plaintext sizes must be, as a
// percentage of the larger, for a digits-only path difference to be believed.
//
// 50 is deliberately permissive, and the asymmetry justifies it: a wrong guess
// costs a patch that comes out no better than the literals it would replace,
// which the cost model then declines -- the same outcome as having no anchor at
// all. A missed guess costs the whole saving. So the bar sits where a candidate
// is merely plausible rather than where it is certain.
const minFuzzySizeSimilarity = 50

// pathMatcher answers, for a target block, where the source keeps its version of
// that block's plaintext.
type pathMatcher struct {
	// owner covers every target data block. Blocks shared by several inodes
	// -- mksquashfs dedups identical files -- are attributed to the first
	// path the walk reached, which is enough: the content is the same.
	owner map[int64]blockOwner

	byPath map[string]*fileLayout
	// byNorm buckets source files by their path with every digit run
	// replaced, so the fuzzy pass is a map lookup rather than a scan over
	// thousands of paths. Random access to the source image is what allows
	// this: both maps are built once, before a single run is costed.
	byNorm map[string][]*fileLayout
}

// newPathMatcher builds the correspondence between two images. The metadata
// regions are the ones the caller already walked, since the tree resolves
// references into them.
//
// A failure here is not fatal to a delta -- the caller falls back to offset
// proximity -- so the only errors returned are ones that mean an image is not
// what it claims to be.
func newPathMatcher(src, tgt *squashfsImage, srcMeta, tgtMeta *metaRegion) (*pathMatcher, error) {
	m := newEmptyPathMatcher()

	srcTree, err := src.FileTree(srcMeta)
	if err != nil {
		return nil, err
	}
	for _, e := range srcTree {
		f, err := layoutOf(src, e)
		if err != nil {
			return nil, err
		}
		m.addSource(f)
	}

	tgtTree, err := tgt.FileTree(tgtMeta)
	if err != nil {
		return nil, err
	}
	for _, e := range tgtTree {
		f, err := layoutOf(tgt, e)
		if err != nil {
			return nil, err
		}
		m.addTarget(f)
	}
	return m, nil
}

// newEmptyPathMatcher sizes the indexes for a snap: a snapcraft revision holds
// around 9,000 files, and both images are indexed before a run is costed.
func newEmptyPathMatcher() *pathMatcher {
	return &pathMatcher{
		owner:  make(map[int64]blockOwner, 8192),
		byPath: make(map[string]*fileLayout, 8192),
		byNorm: make(map[string][]*fileLayout, 8192),
	}
}

// layoutOf reads one file's identity and blocks out of the image it lives in.
func layoutOf(im *squashfsImage, e fileEntry) (*fileLayout, error) {
	blocks, err := im.inodeExtents(e.Inode)
	if err != nil {
		return nil, err
	}
	return &fileLayout{Path: e.Path, USize: int64(e.Inode.FileSize), Blocks: blocks}, nil
}

// addSource indexes a source file under both the exact and the normalized path.
//
// A path appearing twice would mean the walk built an ambiguous tree, which
// readDirListing's name checks already rule out. Keeping the first is right
// regardless: a hard link's two names describe one file.
func (m *pathMatcher) addSource(f *fileLayout) {
	if _, dup := m.byPath[f.Path]; dup {
		return
	}
	m.byPath[f.Path] = f
	norm := normalizeDigits(f.Path)
	m.byNorm[norm] = append(m.byNorm[norm], f)
}

// addTarget records which file each of a target file's blocks belongs to, which
// is the direction a run is looked up in: a run knows its target offsets.
func (m *pathMatcher) addTarget(f *fileLayout) {
	for _, b := range f.Blocks {
		if _, taken := m.owner[b.Offset]; !taken {
			m.owner[b.Offset] = blockOwner{file: f, uOff: b.UOff}
		}
	}
}

// anchor returns the source offset whose plaintext should correspond to the
// target block at tgtOff, and how it was arrived at. A false-ish anchorNone
// leaves the choice to the caller.
func (m *pathMatcher) anchor(tgtOff int64) (int64, anchorKind) {
	if m == nil {
		return 0, anchorNone
	}
	own, ok := m.owner[tgtOff]
	if !ok {
		return 0, anchorNone
	}
	if f, ok := m.byPath[own.file.Path]; ok {
		if off, ok := f.uOffAt(own.uOff); ok {
			return off, anchorPath
		}
		// The source file exists under this name but holds no blocks, which
		// means it was sparse or empty. There is nothing to diff against, so
		// fall through to the fuzzy pass rather than returning an anchor
		// pointing at another file's bytes.
	}
	if f := m.fuzzyMatch(own.file); f != nil {
		if off, ok := f.uOffAt(own.uOff); ok {
			return off, anchorFuzzy
		}
	}
	return 0, anchorNone
}

// fuzzyMatch finds the source file a target file is a later version of: same
// path but for the digits in it, and a plausible size. Among candidates it takes
// the fewest differing components first and the closest size second, because a
// path differing in one component is a version bump while one differing in three
// is a coincidence.
func (m *pathMatcher) fuzzyMatch(tf *fileLayout) *fileLayout {
	var best *fileLayout
	bestDiffs, bestSim := 0, 0
	for _, cand := range m.byNorm[normalizeDigits(tf.Path)] {
		if len(cand.Blocks) == 0 {
			continue
		}
		// The bucket makes this true by construction; asking anyway is what
		// keeps the normalizer and the component-wise definition of a fuzzy
		// path match from drifting apart into a wrong answer.
		score := pathsMatchFuzzy(cand.Path, tf.Path)
		if score == 0 {
			continue
		}
		// pathsMatchFuzzy returns 1 for identical and 1+n for n differing
		// components. An exact path is the caller's business, not this pass's.
		diffs := score - 1
		if diffs == 0 {
			continue
		}
		sim := sizeSimilarity(cand.USize, tf.USize)
		if sim < minFuzzySizeSimilarity {
			continue
		}
		if best == nil || diffs < bestDiffs || (diffs == bestDiffs && sim > bestSim) {
			best, bestDiffs, bestSim = cand, diffs, sim
		}
	}
	return best
}

// pathsMatchFuzzy scores two paths as versions of each other: 0 if they are not,
// 1 if they are identical, and 1+n if n of their components differ only in the
// digits inside them. Paths of different depth never match, so a file that moved
// between directories is not called a version of itself.
func pathsMatchFuzzy(pathA, pathB string) int {
	partsA := strings.Split(pathA, "/")
	partsB := strings.Split(pathB, "/")
	if len(partsA) != len(partsB) {
		return 0
	}
	fuzzy := 0
	for i, a := range partsA {
		b := partsB[i]
		if a == b {
			continue
		}
		if normalizeDigits(a) != normalizeDigits(b) {
			return 0
		}
		fuzzy++
	}
	return 1 + fuzzy
}

// normalizeDigits replaces every run of digits with a single '#', which is the
// bucket key a fuzzy path match agrees with: pathsMatchFuzzy accepts exactly the
// pairs of paths that are equal under this, since digits never cross a separator
// and the substitution preserves component count.
//
// It is a hand-rolled loop rather than a regexp because it runs once per file in
// both images -- around 19,000 times on a pair of snapcraft revisions.
func normalizeDigits(path string) string {
	out := make([]byte, 0, len(path))
	inDigits := false
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c >= '0' && c <= '9' {
			if !inDigits {
				out = append(out, '#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		out = append(out, c)
	}
	return string(out)
}

// sizeSimilarity is the smaller of two sizes as a percentage of the larger: 100
// for equal, 50 for a factor of two, 0 when either is zero and the other is not.
// It ranks candidates, so it has to distinguish "nearly the same file" from
// "half the size" rather than answer yes or no.
func sizeSimilarity(a, b int64) int {
	if a == b {
		return 100
	}
	if a <= 0 || b <= 0 {
		return 0
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return int(100 * lo / hi)
}
