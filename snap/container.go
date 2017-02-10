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
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
)

// Container is the interface to interact with the low-level snap files
type Container interface {
	// Size returns the size of the snap in bytes.
	Size() (int64, error)

	// ReadFile returns the content of a single file from the snap.
	ReadFile(relative string) ([]byte, error)

	// ListDir returns the content of a single directory inside the snap.
	ListDir(path string) ([]string, error)

	// Install copies the snap file to targetPath (and possibly unpacks it to mountDir)
	Install(targetPath, mountDir string) error

	// PreRemove gets a snap ready for removal
	PreRemove() error

	// Unpack unpacks the src parts to the dst directory
	Unpack(src, dst string) error
}

// backend implements a specific snap format
type snapFormat struct {
	magic []byte
	open  func(fn string) (Container, error)
}

// formatHandlers is the registry of known formats, squashfs is the only one atm.
var formatHandlers = []snapFormat{
	{squashfs.Magic, func(p string) (Container, error) {
		return squashfs.New(p), nil
	}},
}

// Open opens a given snap file with the right backend
func Open(path string) (Container, error) {

	if osutil.IsDirectory(path) {
		if osutil.FileExists(filepath.Join(path, "meta", "snap.yaml")) {
			return snapdir.New(path), nil
		}

		return nil, NotSnapError{Path: path}
	}

	// open the file and check magic
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
