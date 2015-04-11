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
		if (erra != nil || errb != nil) && !(erra == io.ErrUnexpectedEOF && errb == io.ErrUnexpectedEOF && ra == rb) {
			return false
		}
		if !bytes.Equal(bufa[:ra], bufb[:rb]) {
			return false
		}
	}
}

// ErrDirNotSuperset means that there are files in the first directory that are
// missing from the second one
var ErrDirNotSuperset = errors.New("not a superset")

// IsSupersetDirUpdated compares two directories, where the files in the first
// are supposed to be a subset of the ones in the second, and returns whether
// the subset of files in the second that are in the first have changed.
//
// If the second directory is not a superset of the first, returns an error.
//
// Subdirectories are ignored.
func IsSupersetDirUpdated(a, b string) (bool, error) {
	fas, err := filepath.Glob(filepath.Join(a, "*"))
	if err != nil {
		return false, err
	}

	for _, fa := range fas {
		if IsDirectory(fa) {
			continue
		}
		fb := filepath.Join(b, filepath.Base(fa))
		if !FileExists(fb) {
			return true, ErrDirNotSuperset
		}
		if !FilesAreEqual(fa, fb) {
			return true, nil
		}
	}

	return false, nil
}
