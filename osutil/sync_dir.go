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
)

// FileState describes the expected content and meta data of a single file.
type FileState struct {
	Content []byte
	Mode    os.FileMode
}

var errSameState = fmt.Errorf("file state has not changed")

// EnsureDirState ensures that directory content matches expectations.
//
// EnsureDirState enumerates all the files in the specified directory that
// match the provided pattern (glob). Each enumerated file is checked to ensure
// that the contents, permissions are what is desired. Unexpected files are
// removed. Missing files are created and differing files are corrected.  Files
// not matching the pattern are ignored.
//
// The content map describes each of the files that are intended to exist in
// the directory.  Map keys must be file names relative to the directory.
// Sub-directories in the name are not allowed.
//
// The function stops at the first encountered error but reports all of the
// changes performed so far. Information about the performed changes is
// returned to the caller for any extra processing that might be required (e.g.
// to run some helper program).
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
	for baseName, fileState := range content {
		filePath := filepath.Join(dir, baseName)
		err := writeFile(filePath, fileState)
		if err == errSameState {
			continue
		}
		if err != nil {
			return changed, removed, err
		}
		changed = append(changed, baseName)
	}
	matches, err := filepath.Glob(filepath.Join(dir, glob))
	if err != nil {
		return changed, removed, err
	}
	for _, filePath := range matches {
		baseName := filepath.Base(filePath)
		if content[baseName] != nil {
			continue
		}
		err := os.Remove(filePath)
		if err != nil {
			return changed, removed, err
		}
		removed = append(removed, baseName)
	}
	return changed, removed, nil
}

func writeFile(filePath string, fileState *FileState) error {
	stat, err := os.Stat(filePath)
	if err != nil {
		return AtomicWriteFile(filePath, fileState.Content, fileState.Mode, 0)
	}
	if stat.Mode().Perm() == fileState.Mode.Perm() && stat.Size() == int64(len(fileState.Content)) {
		content, err := ioutil.ReadFile(filePath)
		if err != nil {
			return err
		}
		if bytes.Equal(content, fileState.Content) {
			// Return a special error if the file doesn't need to be changed
			return errSameState
		}
	}
	return AtomicWriteFile(filePath, fileState.Content, fileState.Mode, 0)
}
