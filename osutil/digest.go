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
	"io"
	"os"

	"github.com/ddkwork/golibrary/mylog"
)

const (
	hashDigestBufSize = 2 * 1024 * 1024
)

// FileDigest computes a hash digest of the file using the given hash.
// It also returns the file size.
func FileDigest(filename string, hash crypto.Hash) ([]byte, uint64, error) {
	f := mylog.Check2(os.Open(filename))

	defer f.Close()
	h := hash.New()
	size := mylog.Check2(io.CopyBuffer(h, f, make([]byte, hashDigestBufSize)))

	return h.Sum(nil), uint64(size), nil
}
