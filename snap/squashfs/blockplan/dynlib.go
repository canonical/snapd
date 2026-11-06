// -*- Mode: Go; indent-tabs-mode: t -*-

//go:build cgo

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

package blockplan

import (
	"path/filepath"
	"sort"
)

// Where the compression libraries come from.
//
// The compressors that need a library are loaded with dlopen at run time rather
// than linked, because the point is to run wherever a snap is installed: the
// snapd snap already ships the exact liblzo2 and libzstd it needs, on every
// architecture snapd supports, so a device that can install snaps has them
// whether or not the distribution packages them. Linking them instead would put
// a build-time dependency on the build machine and a runtime one on the device,
// for libraries the device already has.
//
// Preferring snapd's copy over the system's is deliberate and not merely a
// fallback ordering: it is the one copy whose version is pinned by something
// other than the local distribution, so two devices running the same snapd
// agree on it. Byte-exact reproduction depends on the library's algorithm not
// having drifted, and SEC_CANARY is what catches it when it has.

// snapdLibGlob is where the snapd snap keeps the shared libraries it bundles.
// The wildcard is the multiarch tuple, which differs per architecture and is
// not worth deriving from GOARCH when the directory itself names it.
const snapdLibGlob = "/snap/snapd/current/usr/lib/*/"

// libraryCandidates lists the paths to try for a library, in order: snapd's
// bundled copies first, then the bare soname for the dynamic loader's own
// search path, which covers a build machine with the library packaged and no
// snapd installed.
//
// Only the first one that opens is used. That matters beyond avoiding wasted
// work: glibc keys loaded objects by soname, so once any copy of liblzo2.so.2
// is open, asking for another path with the same soname returns the copy
// already loaded.
func libraryCandidates(soname string) []string {
	bundled, err := filepath.Glob(snapdLibGlob + soname)
	if err != nil {
		// The only error Glob reports is a malformed pattern, which this
		// one is not; treat it as no matches rather than propagating a
		// programming error into a compressor's setup.
		bundled = nil
	}
	sort.Strings(bundled)
	return append(bundled, soname)
}

// grow returns a slice of at least n bytes from buf, reallocating when it is
// short. The returned slice is what to use; the field keeps the capacity.
//
// It lives here rather than in one codec because both of the dlopen'd ones are
// built the same way: a pool of workers, each holding the buffers one block's
// worth of work needs, reused for every block of every call so that a
// whole-image generate does not allocate per block.
func grow(buf *[]byte, n int) []byte {
	if cap(*buf) < n {
		*buf = make([]byte, n)
	}
	return (*buf)[:n]
}
