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

package snap

import (
	"bytes"
	"fmt"
	"os"
)

// File is the interface to interact with the low-level snap files
type File interface {
	// Install copies the snap file to targetPath (and possibly unpacks it to mountDir)
	Install(targetPath, mountDir string) error

	// MetaMember returns data from a meta/ directory file member
	MetaMember(name string) ([]byte, error)

	//Unpack unpacks the src parts to the dst directory
	Unpack(src, dst string) error

	// Info returns information about the given snap file
	Info() (*Info, error)
}

// backend implements a specific snap format
type snapFormat struct {
	magic []byte
	open  func(fn string) (File, error)
}

var formatHandlers []snapFormat

// RegisterFormat registers a snap file format to the system
func RegisterFormat(magic []byte, open func(fn string) (File, error)) {
	formatHandlers = append(formatHandlers, snapFormat{magic, open})
}

// Open opens a given snap file with the right backend
func Open(path string) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open snap: %v", err)
	}
	defer f.Close()

	header := make([]byte, 20)
	if _, err := f.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("cannot read snap: %v", err)
	}

	for _, h := range formatHandlers {
		if bytes.HasPrefix(header, h.magic) {
			return h.open(path)
		}
	}

	return nil, fmt.Errorf("cannot open snap: unknown header: %q", header)
}
