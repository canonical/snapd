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

package snappy

import (
	"fmt"
	"os"
	"strings"

	"launchpad.net/snappy/pkg/clickdeb"
	"launchpad.net/snappy/pkg/snapfs"
)

// PackageInterface is the interface to interact with the low-level snap files
type PackageInterface interface {
	Verify(allowUnauthenticated bool) error
	Close() error
	UnpackWithDropPrivs(targetDir, rootDir string) error
	ControlMember(name string) ([]byte, error)
	MetaMember(name string) ([]byte, error)
	ExtractHashes(targetDir string) error
}

func byteSliceEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// OpenPackageFile opens a given snap file with the right backend
func OpenPackageFile(path string) (PackageInterface, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// look, libmagic!
	header := make([]byte, 20)
	if _, err := f.Read(header); err != nil {
		return nil, err
	}
	// note that we only support little endian squashfs for now
	if byteSliceEqual(header[:4], []byte{'h', 's', 'q', 's'}) {
		return snapfs.New(path), nil
	}
	if strings.HasPrefix(string(header), "!<arch>\ndebian") {
		return clickdeb.Open(path)
	}

	return nil, fmt.Errorf("unknown header %v", header)
}
