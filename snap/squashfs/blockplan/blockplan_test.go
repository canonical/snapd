// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package blockplan_test

import (
	"fmt"
	"math/rand"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/squashfs/blockplan"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

// --- fixtures shared by every suite in the package ---

// testBlockSize is the squashfs data block size snapd packs with, and so the
// dictionary size every block in a snap was compressed with.
const testBlockSize = 131072

// requireTools skips unless every named tool can be found. A machine without
// them is not a failing machine: it is one that cannot make or apply a delta,
// which this package reports as a refusal and the store answers by downloading
// the snap whole.
func requireTools(c *C, names ...string) {
	for _, n := range names {
		if !blockplan.HaveTool(n) {
			c.Skip(fmt.Sprintf("%s is not available", n))
		}
	}
}

// incompressible returns bytes no compressor can shrink, so mksquashfs stores
// the block raw -- the case where cSize == uSize and the compressed bit is set.
func incompressible(n int, seed int64) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	return b
}

// compressibleText returns highly compressible bytes, so the block is stored
// compressed and has cSize far below uSize.
func compressibleText(n int, tag string) []byte {
	out := make([]byte, 0, n+64)
	for len(out) < n {
		out = append(out, "the quick brown fox jumps over the lazy dog "+tag+"\n"...)
	}
	return out[:n]
}

// firstDiff is the offset of the first differing byte, or the shorter length.
// Block plans deal in megabytes, where the offset of a mismatch says far more
// than a dump of both sides would.
func firstDiff(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
