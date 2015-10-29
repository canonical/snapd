// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package pkg

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/ubuntu-core/snappy/pkg/clickdeb"
	"github.com/ubuntu-core/snappy/pkg/snapfs"
)

// File is the interface to interact with the low-level snap files.
type File interface {
	// Verify verfies the integrity of the file.
	// FIXME: use flags here instead of a boolean
	Verify(allowUnauthenticated bool) error
	// UnpackWithDropPrivs unpacks the given the snap to the given
	// targetdir relative to the given rootDir.
	// FIXME: name leaks implementation details, should be Unpack()
	UnpackWithDropPrivs(targetDir, rootDir string) error
	Unpack(src, dstDir string) error
	// ControlMember returns the content of snap meta data files.
	ControlMember(name string) ([]byte, error)
	// MetaMember returns the content of snap meta data files.
	// FIXME: redundant
	MetaMember(name string) ([]byte, error)
	// ExtractHashes extracs the hashes from the snap and puts
	// them into the filesystem for verification.
	ExtractHashes(targetDir string) error

	// NeedsAutoMountUnit determines if it's required to setup
	// an automount unit for the snap when the snap is activated
	NeedsAutoMountUnit() bool
}

// Open opens a given snap file with the right backend.
func Open(path string) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open snap: %v", err)
	}
	defer f.Close()

	// look, libmagic!
	header := make([]byte, 20)
	if _, err := f.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("cannot read snap: %v", err)
	}
	// Note that we only support little endian squashfs. There
	// is nothing else with squashfs 4.0.
	if bytes.HasPrefix(header, []byte{'h', 's', 'q', 's'}) {
		return snapfs.New(path), nil
	}
	if strings.HasPrefix(string(header), "!<arch>\ndebian") {
		return clickdeb.Open(path)
	}

	return nil, fmt.Errorf("cannot open snap: unknown header: %q", header)
}
