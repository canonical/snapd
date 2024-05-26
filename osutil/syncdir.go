// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package osutil

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/ddkwork/golibrary/mylog"
)

// FileState is an interface for conveying the desired state of a some file.
type FileState interface {
	State() (reader io.ReadCloser, size int64, mode os.FileMode, err error)
}

// SymlinkFileState describes the desired symlink by providing its target.
type SymlinkFileState struct {
	Target string
}

func (sym SymlinkFileState) State() (io.ReadCloser, int64, os.FileMode, error) {
	return io.NopCloser(bytes.NewReader([]byte(sym.Target))), int64(len(sym.Target)), os.ModeSymlink, nil
}

// FileReference describes the desired content by referencing an existing file.
type FileReference struct {
	Path string
}

// State returns a reader of the referenced file, along with other meta-data.
func (fref FileReference) State() (io.ReadCloser, int64, os.FileMode, error) {
	file := mylog.Check2(os.Open(fref.Path))

	fi := mylog.Check2(file.Stat())

	if !fi.Mode().IsRegular() {
		return nil, 0, os.FileMode(0), fmt.Errorf("internal error: only regular files are supported, got %q instead", fi.Mode().Type())
	}
	return file, fi.Size(), fi.Mode(), nil
}

// FileReferencePlusMode describes the desired content by referencing an existing file and providing custom mode.
type FileReferencePlusMode struct {
	FileReference
	Mode os.FileMode
}

// State returns a reader of the referenced file, substituting the mode.
func (fcref FileReferencePlusMode) State() (io.ReadCloser, int64, os.FileMode, error) {
	reader, size, _ := mylog.Check4(fcref.FileReference.State())

	if !fcref.Mode.IsRegular() {
		return nil, 0, os.FileMode(0), fmt.Errorf("internal error: only regular files are supported, got %q instead", fcref.Mode.Type())
	}
	return reader, size, fcref.Mode, nil
}

// MemoryFileState describes the desired content by providing an in-memory copy.
type MemoryFileState struct {
	Content []byte
	Mode    os.FileMode
}

// State returns a reader of the in-memory contents of a file, along with other meta-data.
func (blob *MemoryFileState) State() (io.ReadCloser, int64, os.FileMode, error) {
	if !blob.Mode.IsRegular() {
		return nil, 0, os.FileMode(0), fmt.Errorf("internal error: only regular files are supported, got %q instead", blob.Mode.Type())
	}
	return io.NopCloser(bytes.NewReader(blob.Content)), int64(len(blob.Content)), blob.Mode, nil
}

// ErrSameState is returned when the state of a file has not changed.
var ErrSameState = fmt.Errorf("file state has not changed")

// EnsureDirStateGlobs ensures that directory content matches expectations.
//
// EnsureDirStateGlobs enumerates all the files in the specified directory that
// match the provided set of pattern (globs). Each enumerated file is checked
// to ensure that the contents, permissions are what is desired. Unexpected
// files are removed. Missing files are created and differing files are
// corrected. Files not matching any pattern are ignored.
//
// Note that EnsureDirStateGlobs only checks for permissions and content. Other
// security mechanisms, including file ownership and extended attributes are
// *not* supported.
//
// The content map describes each of the files that are intended to exist in
// the directory.  Map keys must be file names relative to the directory.
// Sub-directories in the name are not allowed.
//
// If writing any of the files fails, EnsureDirStateGlobs switches to erase mode
// where *all* of the files managed by the glob pattern are removed (including
// those that may have been already written). The return value is an empty list
// of changed files, the real list of removed files and the first error.
//
// If an error happens while removing files then such a file is not removed but
// the removal continues until the set of managed files matching the glob is
// exhausted.
//
// In all cases, the function returns the first error it has encountered.
func EnsureDirStateGlobs(dir string, globs []string, content map[string]FileState) (changed, removed []string, err error) {
	// Check syntax before doing anything.
	_, index := mylog.Check3(matchAny(globs, "foo"))

	for baseName := range content {
		if filepath.Base(baseName) != baseName {
			return nil, nil, fmt.Errorf("internal error: EnsureDirState got filename %q which has a path component", baseName)
		}
		if ok, _ := mylog.Check3(matchAny(globs, baseName)); !ok {
			if len(globs) == 1 {
				return nil, nil, fmt.Errorf("internal error: EnsureDirState got filename %q which doesn't match the glob pattern %q", baseName, globs[0])
			}
			return nil, nil, fmt.Errorf("internal error: EnsureDirState got filename %q which doesn't match any glob patterns %q", baseName, globs)
		}
	}
	// Change phase (create/change files described by content)
	var firstErr error
	for baseName, fileState := range content {
		filePath := filepath.Join(dir, baseName)
		mylog.Check(EnsureFileState(filePath, fileState))
		if err == ErrSameState {
			continue
		}

		// On write failure, switch to erase mode. Desired content is set
		// to nothing (no content) changed files are forgotten and the
		// writing loop stops. The subsequent erase loop will remove all
		// the managed content.

		changed = append(changed, baseName)
	}
	// Delete phase (remove files matching the glob that are not in content)
	matches := make(map[string]bool)
	for _, glob := range globs {
		m := mylog.Check2(filepath.Glob(filepath.Join(dir, glob)))

		for _, path := range m {
			matches[path] = true
		}
	}

	for path := range matches {
		baseName := filepath.Base(path)
		if content[baseName] != nil {
			continue
		}
		mylog.Check(os.Remove(path))

		removed = append(removed, baseName)
	}
	sort.Strings(changed)
	sort.Strings(removed)
	return changed, removed, firstErr
}

func matchAny(globs []string, path string) (ok bool, index int, err error) {
	for index, glob := range globs {
		if ok := mylog.Check2(filepath.Match(glob, path)); ok || err != nil {
			return ok, index, err
		}
	}
	return false, 0, nil
}

// EnsureDirState ensures that directory content matches expectations.
//
// This is like EnsureDirStateGlobs but it only supports one glob at a time.
func EnsureDirState(dir string, glob string, content map[string]FileState) (changed, removed []string, err error) {
	return EnsureDirStateGlobs(dir, []string{glob}, content)
}

// regularFileStateEqualTo returns whether the file exists in the expected state.
func regularFileStateEqualTo(filePath string, state FileState) (bool, error) {
	other := &FileReference{Path: filePath}

	// Open views to both files so that we can compare them.
	readerA, sizeA, modeA := mylog.Check4(state.State())

	defer readerA.Close()

	readerB, sizeB, modeB := mylog.Check4(other.State())

	// Not existing is not an error

	defer readerB.Close()

	// If the files have different size or different mode they are not
	// identical and need to be re-created. Mode change could be optimized to
	// avoid re-writing the whole file.
	if modeA.Perm() != modeB.Perm() || sizeA != sizeB {
		return false, nil
	}
	// The files have the same size so they might be identical.
	// Do a block-wise comparison to determine that.
	return streamsEqualChunked(readerA, readerB, 0), nil
}

func ensureRegularFileState(filePath string, state FileState) error {
	equal := mylog.Check2(regularFileStateEqualTo(filePath, state))

	if equal {
		// Return a special error if the file doesn't need to be changed
		return ErrSameState
	}
	reader, _, mode := mylog.Check4(state.State())

	return AtomicWrite(filePath, reader, mode, 0)
}

// symlinkFileStateEqualTo returns whether the symlink exists in the expected state.
func symlinkFileStateEqualTo(filePath string, state FileState) (bool, error) {
	readerA, _, _ := mylog.Check4(state.State())

	defer readerA.Close()
	buf := mylog.Check2(io.ReadAll(readerA))

	targetA := string(buf)

	other := mylog.Check2(os.Lstat(filePath))

	// Not existing is not an error

	if other.Mode().Type() != os.ModeSymlink {
		return false, nil
	}
	targetB := mylog.Check2(os.Readlink(filePath))

	return targetA == targetB, nil
}

func ensureSymlinkFileState(filePath string, state FileState) error {
	equal := mylog.Check2(symlinkFileStateEqualTo(filePath, state))

	if equal {
		// Return a special error if the file doesn't need to be changed
		return ErrSameState
	}
	reader, _, _ := mylog.Check4(state.State())

	buf := mylog.Check2(io.ReadAll(reader))

	target := string(buf)
	return AtomicSymlink(target, filePath)
}

// EnsureFileState ensures that the file is in the expected state. It will not
// attempt to remove the file if no content is provided.
func EnsureFileState(filePath string, state FileState) error {
	_, _, mode := mylog.Check4(state.State())

	switch {
	case mode.IsRegular():
		return ensureRegularFileState(filePath, state)
	case mode.Type() == os.ModeSymlink:
		return ensureSymlinkFileState(filePath, state)
	}
	return fmt.Errorf("internal error: EnsureFileState does not support type %q", mode.Type())
}
