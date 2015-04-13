/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package helpers

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const bufsz = 16 * 1024

// FilesAreEqual compares the two files' contents and returns whether
// they are the same.
func FilesAreEqual(a, b string) bool {
	fa, err := os.Open(a)
	if err != nil {
		return false
	}
	defer fa.Close()

	fb, err := os.Open(b)
	if err != nil {
		return false
	}
	defer fb.Close()

	fia, err := fa.Stat()
	if err != nil {
		return false
	}

	fib, err := fb.Stat()
	if err != nil {
		return false
	}

	if fia.Size() != fib.Size() {
		return false
	}

	return streamsEqual(fa, fb)
}

func streamsEqual(fa, fb io.Reader) bool {
	bufa := make([]byte, bufsz)
	bufb := make([]byte, bufsz)
	for {
		ra, erra := io.ReadFull(fa, bufa)
		rb, errb := io.ReadFull(fb, bufb)
		if erra == io.EOF && errb == io.EOF {
			return true
		}
		if erra != nil || errb != nil {
			// if both files finished in the middle of a ReadFull,
			// (returning io.ErrUnexpectedEOF), having read the same
			// amount (so ra==rb), then we still need to check what
			// was read to know whether they're equal.  Otherwise,
			// we know they're not equal (because we count any read
			// error as a being non-equal also).
			tailMightBeEqual := erra == io.ErrUnexpectedEOF && errb == io.ErrUnexpectedEOF && ra == rb
			if !tailMightBeEqual {
				return false
			}
		}
		if !bytes.Equal(bufa[:ra], bufb[:rb]) {
			return false
		}
	}
}

// ErrDirNotSuperset means that there are files in the first directory that are
// missing from the second one
var ErrDirNotSuperset = errors.New("not a superset")

// SupersetDirUpdated compares two directories, where the files in the
// first that have the prefix are supposed to be a subset of the ones
// in the second, and returns a map of files that have changed.
//
// If the second directory is not a superset of the first, returns an error.
//
// Subdirectories are ignored.
func SupersetDirUpdated(dirA, pfxA, dirB string) (map[string]bool, error) {
	filesA, err := filepath.Glob(filepath.Join(dirA, pfxA+"*"))
	if err != nil {
		return nil, err
	}

	updated := make(map[string]bool)
	for _, fileA := range filesA {
		if IsDirectory(fileA) {
			continue
		}
		name := filepath.Base(fileA)[len(pfxA):]
		fileB := filepath.Join(dirB, name)
		if !FileExists(fileB) {
			return nil, ErrDirNotSuperset
		}
		if !FilesAreEqual(fileA, fileB) {
			updated[name] = true
		}
	}

	return updated, nil
}
