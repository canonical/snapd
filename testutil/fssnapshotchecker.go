// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package testutil

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/check.v1"
)

// fsEntry records attributes of a filesystem entry for comparison
type fsEntry struct {
	mode           os.FileMode
	size           int64
	contentHash    string // empty for dirs
	contentPreview []byte // first maxContentPreviewSize bytes; nil for dirs
}

// maxContentPreviewSize is the maximum number of bytes stored per file
// for content diff display in error messages.
const maxContentPreviewSize = 256

// FsSnapshot maps relative paths to filesystem entry
type FsSnapshot map[string]fsEntry

// FsDiffKind describes the kind of difference between two filesystem entries
type FsDiffKind string

const (
	PresenceDiffKind FsDiffKind = "presence"
	ModeDiffKind     FsDiffKind = "mode"
	SizeDiffKind     FsDiffKind = "size"
	ContentDiffKind  FsDiffKind = "content"
)

// fsSnapshotDiff maps paths to all diff kinds found for that path
type fsSnapshotDiff map[string][]FsDiffKind

// FsIgnoreDiff describes which diff kinds to ignore for a given path
type FsIgnoreDiff struct {
	Kinds []FsDiffKind
	// If IgnoreParents is true, the diff kinds are ignored for all parent paths too
	IgnoreParents bool
	// can be extended to IgnoreChildren, if needed
}

// FsSnapshotIgnoreDiff maps paths to ignore rules for differences found for that path
type FsSnapshotIgnoreDiff map[string]FsIgnoreDiff

func getFileHash(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// CreateFsSnapshot walks the root directory and collects fs entries
func CreateFsSnapshot(rootDir string) (FsSnapshot, error) {
	entries := make(FsSnapshot)
	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(rootDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := fsEntry{
			mode: info.Mode(),
			size: info.Size(),
		}
		if !d.IsDir() {
			hash, err := getFileHash(path)
			if err != nil {
				return err
			}
			entry.contentHash = hash

			previewSize := entry.size
			if previewSize > maxContentPreviewSize {
				previewSize = maxContentPreviewSize
			}
			preview := make([]byte, previewSize)
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.ReadFull(f, preview)
			f.Close()
			if err != nil {
				return err
			}
			entry.contentPreview = preview
		}
		entries[rel] = entry
		return nil
	})
	return entries, err
}

// TODO:GOVERSION: replace this with slices.Contains() once we're on go 1.21+
func contains[T comparable](s []T, e T) bool {
	for _, k := range s {
		if k == e {
			return true
		}
	}
	return false
}

func (fi *FsSnapshotIgnoreDiff) isIgnored(path string, dk FsDiffKind) bool {
	if fi == nil {
		return false
	}
	for p, diff := range *fi {
		if p == path && contains(diff.Kinds, dk) {
			return true
		}
		// If IgnoreParents is set, ignore all its parents too
		if diff.IgnoreParents {
			if strings.HasPrefix(p, path) && contains(diff.Kinds, dk) {
				return true
			}
		}
	}
	return false
}

// compareFsSnapshots compares two filesystem snapshots, returning
// differences that are not covered by the ignore rules.
func compareFsSnapshots(before, after FsSnapshot, ignore *FsSnapshotIgnoreDiff) fsSnapshotDiff {
	diffs := make(fsSnapshotDiff)

	allPaths := make(map[string]struct{})
	for p := range before {
		allPaths[p] = struct{}{}
	}
	for p := range after {
		allPaths[p] = struct{}{}
	}

	for path := range allPaths {
		if ignore.isIgnored(path, PresenceDiffKind) {
			continue
		}
		bEntry, bHas := before[path]
		aEntry, aHas := after[path]

		if (bHas && !aHas) || (!bHas && aHas) {
			diffs[path] = append(diffs[path], PresenceDiffKind)
			continue
		}

		// Both exist - compare attributes
		if bEntry.mode != aEntry.mode && !ignore.isIgnored(path, ModeDiffKind) {
			diffs[path] = append(diffs[path], ModeDiffKind)
		}
		if bEntry.size != aEntry.size && !ignore.isIgnored(path, SizeDiffKind) {
			diffs[path] = append(diffs[path], SizeDiffKind)
		}
		if bEntry.contentHash != aEntry.contentHash && !ignore.isIgnored(path, ContentDiffKind) {
			diffs[path] = append(diffs[path], ContentDiffKind)
		}
	}
	return diffs
}

type fsSnapshotChecker struct {
	*check.CheckerInfo
}

// FsSnapshotsEqual verifies that two FsSnapshot values represent the same
// filesystem state, accounting for any ignored differences.
//
// Usage:
//
//	c.Check(after, testutil.FsSnapshotsEqual, before, &testutil.FsSnapshotIgnoreDiff{...})
//
// The third argument (ignoreDiff) may be nil to require an exact match.
var FsSnapshotsEqual check.Checker = &fsSnapshotChecker{
	CheckerInfo: &check.CheckerInfo{
		Name:   "FsSnapshotsEqual",
		Params: []string{"after", "before", "ignoreDiff"},
	},
}

func (c *fsSnapshotChecker) Check(params []any, names []string) (result bool, error string) {
	after, ok := params[0].(FsSnapshot)
	if !ok {
		return false, "after value must be of type testutil.FsSnapshot"
	}
	before, ok := params[1].(FsSnapshot)
	if !ok {
		return false, "before value must be of type testutil.FsSnapshot"
	}

	var ignore *FsSnapshotIgnoreDiff
	if params[2] != nil {
		ptr, ok := params[2].(*FsSnapshotIgnoreDiff)
		if !ok {
			return false, "ignoreDiff value must be of type *testutil.FsSnapshotIgnoreDiff or nil"
		}
		ignore = ptr
	}

	diffs := compareFsSnapshots(before, after, ignore)
	if len(diffs) == 0 {
		return true, ""
	}

	// Build an error message listing all differences
	paths := make([]string, 0, len(diffs))
	for p := range diffs {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var sb strings.Builder
	sb.WriteString("filesystem snapshots differ:\n")
	sb.WriteString("  <path>: <difference kinds>\n")
	for _, p := range paths {
		dkinds := diffs[p]
		strs := make([]string, len(dkinds))
		bEntry := before[p]
		aEntry := after[p]
		for i, dk := range dkinds {
			switch dk {
			case PresenceDiffKind:
				if _, inAfter := after[p]; inAfter {
					strs[i] = string(dk) + " (added)"
				} else {
					strs[i] = string(dk) + " (removed)"
				}
			case ModeDiffKind:
				strs[i] = fmt.Sprintf("%s (%04o -> %04o)", dk, bEntry.mode.Perm(), aEntry.mode.Perm())
			case SizeDiffKind:
				strs[i] = fmt.Sprintf("%s (%d -> %d)", dk, bEntry.size, aEntry.size)
			case ContentDiffKind:
				strs[i] = formatContentDiff(bEntry, aEntry)
			default:
				strs[i] = string(dk)
			}
		}
		fmt.Fprintf(&sb, "  %s: %s\n", p, strings.Join(strs, ", "))
	}
	return false, sb.String()
}

// formatContentDiff formats a content diff entry. For small files where the
// full content was captured, it shows the actual before -> after text.
func formatContentDiff(bEntry, aEntry fsEntry) string {
	bFull := int64(len(bEntry.contentPreview)) == bEntry.size
	aFull := int64(len(aEntry.contentPreview)) == aEntry.size
	if bFull && aFull {
		return fmt.Sprintf("%s (%q -> %q)", ContentDiffKind, bEntry.contentPreview, aEntry.contentPreview)
	}
	return string(ContentDiffKind)
}
