// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package snapfile

import (
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
)

// backend implements a specific snap format
type snapFormat struct {
	matches func(string) bool
	open    func(string) (snap.Container, error)
}

// formatHandlers is the registry of known formats that work with Open
var formatHandlers = []snapFormat{
	// standard squashfs snap file format
	{
		squashfs.FileHasSquashfsHeader,
		func(p string) (snap.Container, error) { return squashfs.New(p), nil },
	},
	// snap directory format, i.e. snap try <dir>
	{
		snapdir.IsSnapDir,
		func(p string) (snap.Container, error) { return snapdir.New(p), nil },
	},
}

// Open opens a given snap file with the right backend.
func Open(path string) (snap.Container, error) {
	for _, h := range formatHandlers {
		if h.matches(path) {
			return h.open(path)
		}
	}

	return nil, snap.NotSnapError{Path: path}
}
