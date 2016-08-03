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
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
)

// FileState describes the expected content and meta data of a single file.
type FileState struct {
	Content []byte
	Mode    os.FileMode
}

// ErrSameState is returned when the state of a file has not changed.
var ErrSameState = fmt.Errorf("file state has not changed")

// EnsureDirState ensures that directory content matches expectations.
//
// EnsureDirState enumerates all the files in the specified directory that
// match the provided pattern (glob). Each enumerated file is checked to ensure
// that the contents, permissions are what is desired. Unexpected files are
// removed. Missing files are created and differing files are corrected.  Files
// not matching the pattern are ignored.
//
// Note that EnsureDirState only checks for permissions and content. Other
// security mechanisms, including file ownership and extended attributes are
// *not* supported.
//
// The content map describes each of the files that are intended to exist in
// the directory.  Map keys must be file names relative to the directory.
// Sub-directories in the name are not allowed.
//
// If writing any of the files fails, EnsureDirState switches to erase mode
// where *all* of the files managed by the glob pattern are removed (including
// those that may have been already written). The return value is an empty list
// of changed files, the real list of removed files and the first error.
//
// If an error happens while removing files then such a file is not removed but
// the removal continues until the set of managed files matching the glob is
// exhausted.
//
// In all cases, the function returns the first error it has encountered.
func EnsureDirState(dir, glob string, content map[string]*FileState) (changed, removed []string, err error) {
	if _, err := filepath.Match(glob, "foo"); err != nil {
		panic(fmt.Sprintf("EnsureDirState got invalid pattern %q: %s", glob, err))
	}
	for baseName := range content {
		if filepath.Base(baseName) != baseName {
			panic(fmt.Sprintf("EnsureDirState got filename %q which has a path component", baseName))
		}
		if ok, _ := filepath.Match(glob, baseName); !ok {
			panic(fmt.Sprintf("EnsureDirState got filename %q which doesn't match the glob pattern %q", baseName, glob))
		}
	}
	// Change phase (create/change files described by content)
	var firstErr error
	for baseName, fileState := range content {
		filePath := filepath.Join(dir, baseName)
		err := EnsureFileState(filePath, fileState)
		if err == ErrSameState {
			continue
		}
		if err != nil {
			// On write failure, switch to erase mode. Desired content is set
			// to nothing (no content) changed files are forgotten and the
			// writing loop stops. The subsequent erase loop will remove all
			// the managed content.
			firstErr = err
			content = nil
			changed = nil
			break
		}
		changed = append(changed, baseName)
	}
	// Delete phase (remove files matching the glob that are not in content)
	matches, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		sort.Strings(changed)
		return changed, nil, err
	}
	for _, filePath := range matches {
		baseName := filepath.Base(filePath)
		if content[baseName] != nil {
			continue
		}
		err := os.Remove(filePath)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed = append(removed, baseName)
	}
	sort.Strings(changed)
	sort.Strings(removed)
	return changed, removed, firstErr
}

// EnsureFileState ensures that the file is in the expected state. It will not attempt
// to remove the file if no content is provided.
func EnsureFileState(filePath string, fileState *FileState) error {
	stat, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return AtomicWriteFile(filePath, fileState.Content, fileState.Mode, 0)
	}
	if err != nil {
		return err
	}
	if stat.Mode().Perm() == fileState.Mode.Perm() && stat.Size() == int64(len(fileState.Content)) {
		content, err := ioutil.ReadFile(filePath)
		if err != nil {
			return err
		}
		if bytes.Equal(content, fileState.Content) {
			// Return a special error if the file doesn't need to be changed
			return ErrSameState
		}
	}
	return AtomicWriteFile(filePath, fileState.Content, fileState.Mode, 0)
}
