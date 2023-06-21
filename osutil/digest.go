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
	"crypto"
	"fmt"
	"io"
	"os"
)

const (
	hashDigestBufSize = 2 * 1024 * 1024
)

// FileDigest computes a hash digest of the file using the given hash.
// It also returns the file size.
func FileDigest(filename string, hash crypto.Hash) ([]byte, uint64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	h := hash.New()
	size, err := io.CopyBuffer(h, f, make([]byte, hashDigestBufSize))
	if err != nil {
		return nil, 0, err
	}
	return h.Sum(nil), uint64(size), nil
}

// PartialFileDigest computes a hash digest of the file starting from an offset using the given hash.
// It also returns the size of the data that were hashed.
func PartialFileDigest(filename string, hash crypto.Hash, offset uint64) ([]byte, uint64, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}

	if offset >= uint64(fi.Size()) {
		return nil, 0, fmt.Errorf("offset exceeds file size")
	}

	if _, err = f.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, 0, err
	}

	h := hash.New()
	size, err := io.CopyBuffer(h, f, make([]byte, hashDigestBufSize))
	if err != nil {
		return nil, 0, err
	}
	return h.Sum(nil), uint64(size), nil
}
