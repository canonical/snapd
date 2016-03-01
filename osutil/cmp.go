// -*- Mode: Go; indent-tabs-mode: t -*-

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

package osutil

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
)

const defaultBufsz = 16 * 1024

var bufsz = defaultBufsz

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
		ra, erra := io.ReadAtLeast(fa, bufa, bufsz)
		rb, errb := io.ReadAtLeast(fb, bufb, bufsz)
		if erra == io.EOF && errb == io.EOF {
			return true
		}
		if erra != nil || errb != nil {
			// if both files finished in the middle of a
			// ReadAtLeast, (returning io.ErrUnexpectedEOF), then we
			// still need to check what was read to know whether
			// they're equal.  Otherwise, we know they're not equal
			// (because we count any read error as a being non-equal
			// also).
			tailMightBeEqual := erra == io.ErrUnexpectedEOF && errb == io.ErrUnexpectedEOF
			if !tailMightBeEqual {
				return false
			}
		}
		if !bytes.Equal(bufa[:ra], bufb[:rb]) {
			return false
		}
	}
}

// DirUpdated compares two directories, and returns which files present in both
// have been updated, with the given prefix prepended.
//
// Subdirectories are ignored.
//
// This function is to compare the policies and templates in a (framework) snap
// to be installed, against the policies and templates of one already installed,
// to then determine what changed. The prefix is because policies and templates
// are specified with the framework name.
func DirUpdated(dirA, dirB, pfx string) map[string]bool {
	filesA, _ := filepath.Glob(filepath.Join(dirA, "*"))

	updated := make(map[string]bool)
	for _, fileA := range filesA {
		if IsDirectory(fileA) {
			continue
		}

		name := filepath.Base(fileA)
		fileB := filepath.Join(dirB, name)
		if FileExists(fileB) && !FilesAreEqual(fileA, fileB) {
			updated[pfx+name] = true
		}
	}

	return updated
}
