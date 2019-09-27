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

	return StreamsEqual(fa, fb)
}

// StreamsEqual compares two streams and returns true if both
// have the same content.
func StreamsEqual(a, b io.Reader) bool {
	bufa := make([]byte, bufsz)
	bufb := make([]byte, bufsz)
	for {
		ra, erra := io.ReadAtLeast(a, bufa, bufsz)
		rb, errb := io.ReadAtLeast(b, bufb, bufsz)
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

// StreamEqual returns true if both streams have same length and content.
func StreamEqual(readerA, readerB io.Reader, chunkSize int) (bool, error) {
	if readerA == readerB {
		return true, nil
	}
	if chunkSize <= 0 {
		chunkSize = 4096
	}
	bufA := make([]byte, chunkSize)
	bufB := make([]byte, chunkSize)
	for {
		nA, errA := readerA.Read(bufA)
		toReadFromB := nA
		if toReadFromB == 0 {
			// If we read nothing from stream A we want to get a chance to read
			// something from B so that we can detect streams of unequal
			// length.
			toReadFromB = 1
		}
		nB, errB := io.ReadAtLeast(readerB, bufB, toReadFromB)
		// We read the same non-empty amount from each stream.
		if nA == nB && nA > 0 {
			if bytes.Equal(bufA[:nA], bufB[:nB]) {
				continue
			}
		}
		// We read nothing from both streams.
		if nA == nB && nA == 0 {
			break
		}
		// Return an error except for EOF and ErrUnexpectedEOF, since those are
		// just end-of-file indicators. Note that unexpected EOF is not really
		// unexpected for us because we don't assume they streams have equal
		// length.
		if errA != nil && errA != io.EOF {
			return false, errA
		}
		if errB != nil && errB != io.EOF && errB != io.ErrUnexpectedEOF {
			return false, errB
		}
		return false, nil
	}
	return true, nil
}
