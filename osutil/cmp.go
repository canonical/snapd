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
)

const defaultChunkSize = 16 * 1024

func filesAreEqualChunked(a, b string, chunkSize int) bool {
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

	return streamsEqualChunked(fa, fb, chunkSize)
}

// FilesAreEqual compares the two files' contents and returns whether
// they are the same.
func FilesAreEqual(a, b string) bool {
	return filesAreEqualChunked(a, b, 0)
}

func streamsEqualChunked(a, b io.Reader, chunkSize int) bool {
	if a == b {
		return true
	}
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	bufa := make([]byte, chunkSize)
	bufb := make([]byte, chunkSize)
	for {
		ra, erra := io.ReadAtLeast(a, bufa, chunkSize)
		rb, errb := io.ReadAtLeast(b, bufb, chunkSize)
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

// StreamsEqual compares two streams and returns true if both
// have the same content.
func StreamsEqual(a, b io.Reader) bool {
	return streamsEqualChunked(a, b, 0)
}

// StreamEqual returns true if both streams have same length and content.
func StreamEqual(readerA, readerB io.Reader, chunkSize int) bool {
	return streamsEqualChunked(readerA, readerB, chunkSize)
}
