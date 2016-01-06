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

package asserts

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
)

// the default filesystem based backstore for assertions

const (
	assertionsLayoutVersion = "v0"
	assertionsRoot          = "asserts-" + assertionsLayoutVersion
)

type filesystemBackstore struct {
	top string
}

func newFilesystemBackstore(path string) *filesystemBackstore {
	return &filesystemBackstore{top: filepath.Join(path, assertionsRoot)}
}

func (fsbs *filesystemBackstore) Init(_ BuilderFromComps) error {
	return nil
}

// guarantees that result assertion is of the expected type (both in the AssertionType and go type sense)
func (fsbs *filesystemBackstore) readAssertion(assertType AssertionType, diskPrimaryPath string) (Assertion, error) {
	encoded, err := readEntry(fsbs.top, string(assertType), diskPrimaryPath)
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, failed to read assertion: %v", err)
	}
	assert, err := Decode(encoded)
	if err != nil {
		return nil, fmt.Errorf("broken assertion storage, failed to decode assertion: %v", err)
	}
	if assert.Type() != assertType {
		return nil, fmt.Errorf("assertion that is not of type %q under their storage tree", assertType)
	}
	// because of Decode() construction assert has also the expected go type
	return assert, nil
}

func buildDiskPrimaryPath(primaryPath []string) string {
	comps := make([]string, len(primaryPath))
	// safety against '/' etc
	for i, comp := range primaryPath {
		comps[i] = url.QueryEscape(comp)
	}
	return filepath.Join(comps...)
}

func (fsbs *filesystemBackstore) Put(assertType AssertionType, primaryPath []string, assert Assertion) error {
	diskPrimaryPath := buildDiskPrimaryPath(primaryPath)
	curAssert, err := fsbs.readAssertion(assertType, diskPrimaryPath)
	if err == nil {
		curRev := curAssert.Revision()
		rev := assert.Revision()
		if curRev >= rev {
			// XXX use structured error and formatting one level up?
			return fmt.Errorf("assertion added must have more recent revision than current one (adding %d, currently %d)", rev, curRev)
		}
	} else if err != ErrNotFound {
		return err
	}
	err = atomicWriteEntry(Encode(assert), false, fsbs.top, string(assertType), diskPrimaryPath)
	if err != nil {
		return fmt.Errorf("broken assertion storage, failed to write assertion: %v", err)
	}
	return nil
}

func (fsbs *filesystemBackstore) Get(assertType AssertionType, primaryPath []string) (Assertion, error) {
	return fsbs.readAssertion(assertType, buildDiskPrimaryPath(primaryPath))
}

func (fsbs *filesystemBackstore) search(assertType AssertionType, diskPattern []string, foundCb func(Assertion)) error {
	assertTypeTop := filepath.Join(fsbs.top, string(assertType))
	candCb := func(diskPrimaryPath string) error {
		a, err := fsbs.readAssertion(assertType, diskPrimaryPath)
		if err == ErrNotFound {
			return fmt.Errorf("broken assertion storage, disappearing entry: %s/%s", assertType, diskPrimaryPath)
		}
		if err != nil {
			return err
		}
		foundCb(a)
		return nil
	}
	err := findWildcard(assertTypeTop, diskPattern, candCb)
	if err != nil {
		return fmt.Errorf("broken assertion storage, searching for %s: %v", assertType, err)
	}
	return nil
}

func (fsbs *filesystemBackstore) SearchByHeaders(assertType AssertionType, headers map[string]string, pathHint []string, foundCb func(Assertion)) error {
	diskPattern := make([]string, len(pathHint))
	for i, comp := range pathHint {
		if comp == "" {
			diskPattern[i] = "*"
		} else {
			diskPattern[i] = url.QueryEscape(comp)
		}
	}

	candCb := func(a Assertion) {
		if searchMatch(a, headers) {
			foundCb(a)
		}
	}
	return fsbs.search(assertType, diskPattern, candCb)
}

func (fsbs *filesystemBackstore) SearchBySuffix(assertType AssertionType, primaryPathWithoutLast []string, suffixOflast string, foundCb func(Assertion)) error {
	diskPattern := make([]string, len(primaryPathWithoutLast)+1)
	for i, comp := range primaryPathWithoutLast {
		diskPattern[i] = url.QueryEscape(comp)
	}
	diskPattern[len(diskPattern)-1] = "*" + suffixOflast

	return fsbs.search(assertType, diskPattern, foundCb)
}
