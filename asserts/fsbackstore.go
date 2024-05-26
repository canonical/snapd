// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
)

// the default filesystem based backstore for assertions

const (
	assertionsLayoutVersion = "v0"
	assertionsRoot          = "asserts-" + assertionsLayoutVersion
)

type filesystemBackstore struct {
	top string
	mu  sync.RWMutex
}

// OpenFSBackstore opens a filesystem backed assertions backstore under path.
func OpenFSBackstore(path string) (Backstore, error) {
	top := filepath.Join(path, assertionsRoot)
	mylog.Check(ensureTop(top))

	return &filesystemBackstore{top: top}, nil
}

// guarantees that result assertion is of the expected type (both in the AssertionType and go type sense)
func (fsbs *filesystemBackstore) readAssertion(assertType *AssertionType, diskPrimaryPath string) (Assertion, error) {
	encoded := mylog.Check2(readEntry(fsbs.top, assertType.Name, diskPrimaryPath))
	if os.IsNotExist(err) {
		return nil, errNotFound
	}

	assert := mylog.Check2(Decode(encoded))

	if assert.Type() != assertType {
		return nil, fmt.Errorf("assertion that is not of type %q under their storage tree", assertType.Name)
	}
	// because of Decode() construction assert has also the expected go type
	return assert, nil
}

func (fsbs *filesystemBackstore) pickLatestAssertion(assertType *AssertionType, diskPrimaryPaths []string, maxFormat int) (a Assertion, er error) {
	for _, diskPrimaryPath := range diskPrimaryPaths {
		fn := filepath.Base(diskPrimaryPath)
		parts := strings.SplitN(fn, ".", 2)
		formatnum := 0
		if len(parts) == 2 {
			formatnum = mylog.Check2(strconv.Atoi(parts[1]))
		}
		if formatnum <= maxFormat {
			a1 := mylog.Check2(fsbs.readAssertion(assertType, diskPrimaryPath))

			if a == nil || a1.Revision() > a.Revision() {
				a = a1
			}
		}
	}
	if a == nil {
		return nil, errNotFound
	}
	return a, nil
}

// diskPrimaryPathComps computes the components of the path for an assertion.
// The path will look like this: (all <comp> are query escaped)
// <primaryPath0>/<primaryPath1>...[/0:<optPrimaryPath0>[/1:<optPrimaryPath1>]...]/<active>
// The components #:<value> for the optional primary path values
// appear only if their value is not the default.
// This makes it so that assertions with default values have the same
// paths as for snapd versions without those optional primary keys
// yet.
func diskPrimaryPathComps(assertType *AssertionType, primaryPath []string, active string) []string {
	n := len(primaryPath)
	comps := make([]string, 0, n+1)
	// safety against '/' etc
	noptional := -1
	for i, comp := range primaryPath {
		defl := assertType.OptionalPrimaryKeyDefaults[assertType.PrimaryKey[i]]
		qvalue := url.QueryEscape(comp)
		if defl != "" {
			noptional++
			if comp == defl {
				continue
			}
			qvalue = fmt.Sprintf("%d:%s", noptional, qvalue)
		}
		comps = append(comps, qvalue)
	}
	comps = append(comps, active)
	return comps
}

func (fsbs *filesystemBackstore) currentAssertion(assertType *AssertionType, primaryPath []string, maxFormat int) (Assertion, error) {
	var a Assertion
	namesCb := func(relpaths []string) error {
		a = mylog.Check2(fsbs.pickLatestAssertion(assertType, relpaths, maxFormat))
		if err == errNotFound {
			return nil
		}
		return err
	}

	comps := diskPrimaryPathComps(assertType, primaryPath, "active*")
	assertTypeTop := filepath.Join(fsbs.top, assertType.Name)
	mylog.Check(findWildcard(assertTypeTop, comps, 0, namesCb))

	if a == nil {
		return nil, errNotFound
	}

	return a, nil
}

func (fsbs *filesystemBackstore) Put(assertType *AssertionType, assert Assertion) error {
	fsbs.mu.Lock()
	defer fsbs.mu.Unlock()

	primaryPath := assert.Ref().PrimaryKey

	curAssert := mylog.Check2(fsbs.currentAssertion(assertType, primaryPath, assertType.MaxSupportedFormat()))
	if err == nil {
		curRev := curAssert.Revision()
		rev := assert.Revision()
		if curRev >= rev {
			return &RevisionError{Current: curRev, Used: rev}
		}
	} else if err != errNotFound {
		return err
	}

	formatnum := assert.Format()
	activeFn := "active"
	if formatnum > 0 {
		activeFn = fmt.Sprintf("active.%d", formatnum)
	}
	diskPrimaryPath := filepath.Join(diskPrimaryPathComps(assertType, primaryPath, activeFn)...)
	mylog.Check(atomicWriteEntry(Encode(assert), false, fsbs.top, assertType.Name, diskPrimaryPath))

	return nil
}

func (fsbs *filesystemBackstore) Get(assertType *AssertionType, key []string, maxFormat int) (Assertion, error) {
	fsbs.mu.RLock()
	defer fsbs.mu.RUnlock()

	if len(key) > len(assertType.PrimaryKey) {
		return nil, fmt.Errorf("internal error: Backstore.Get given a key longer than expected for %q: %v", assertType.Name, key)
	}

	a := mylog.Check2(fsbs.currentAssertion(assertType, key, maxFormat))
	if err == errNotFound {
		return nil, &NotFoundError{Type: assertType}
	}
	return a, err
}

func (fsbs *filesystemBackstore) search(assertType *AssertionType, diskPattern []string, foundCb func(Assertion), maxFormat int) error {
	assertTypeTop := filepath.Join(fsbs.top, assertType.Name)
	candCb := func(diskPrimaryPaths []string) error {
		a := mylog.Check2(fsbs.pickLatestAssertion(assertType, diskPrimaryPaths, maxFormat))
		if err == errNotFound {
			return nil
		}

		foundCb(a)
		return nil
	}
	mylog.Check(findWildcard(assertTypeTop, diskPattern, 0, candCb))

	return nil
}

func (fsbs *filesystemBackstore) searchOptional(assertType *AssertionType, kopt, pattPos, firstOpt int, diskPattern []string, headers map[string]string, foundCb func(Assertion), maxFormat int) error {
	if kopt == len(assertType.PrimaryKey) {
		candCb := func(a Assertion) {
			if searchMatch(a, headers) {
				foundCb(a)
			}
		}

		diskPattern[pattPos] = "active*"
		return fsbs.search(assertType, diskPattern[:pattPos+1], candCb, maxFormat)
	}
	k := assertType.PrimaryKey[kopt]
	keyVal := headers[k]
	switch keyVal {
	case "":
		diskPattern[pattPos] = fmt.Sprintf("%d:*", kopt-firstOpt)
		mylog.Check(fsbs.searchOptional(assertType, kopt+1, pattPos+1, firstOpt, diskPattern, headers, foundCb, maxFormat))

		fallthrough
	case assertType.OptionalPrimaryKeyDefaults[k]:
		return fsbs.searchOptional(assertType, kopt+1, pattPos, firstOpt, diskPattern, headers, foundCb, maxFormat)
	default:
		diskPattern[pattPos] = fmt.Sprintf("%d:%s", kopt-firstOpt, url.QueryEscape(keyVal))
		return fsbs.searchOptional(assertType, kopt+1, pattPos+1, firstOpt, diskPattern, headers, foundCb, maxFormat)
	}
}

func (fsbs *filesystemBackstore) Search(assertType *AssertionType, headers map[string]string, foundCb func(Assertion), maxFormat int) error {
	fsbs.mu.RLock()
	defer fsbs.mu.RUnlock()

	n := len(assertType.PrimaryKey)
	nopt := len(assertType.OptionalPrimaryKeyDefaults)
	diskPattern := make([]string, n+1)
	for i, k := range assertType.PrimaryKey[:n-nopt] {
		keyVal := headers[k]
		if keyVal == "" {
			diskPattern[i] = "*"
		} else {
			diskPattern[i] = url.QueryEscape(keyVal)
		}
	}
	pattPos := n - nopt

	return fsbs.searchOptional(assertType, pattPos, pattPos, pattPos, diskPattern, headers, foundCb, maxFormat)
}

// errFound marks the case an assertion was found
var errFound = errors.New("found")

func (fsbs *filesystemBackstore) SequenceMemberAfter(assertType *AssertionType, sequenceKey []string, after, maxFormat int) (SequenceMember, error) {
	if !assertType.SequenceForming() {
		panic(fmt.Sprintf("internal error: SequenceMemberAfter on non sequence-forming assertion type %s", assertType.Name))
	}
	if len(sequenceKey) != len(assertType.PrimaryKey)-1 {
		return nil, fmt.Errorf("internal error: SequenceMemberAfter's sequence key argument length must be exactly 1 less than the assertion type primary key")
	}

	fsbs.mu.RLock()
	defer fsbs.mu.RUnlock()

	n := len(assertType.PrimaryKey)
	diskPattern := make([]string, n+1)
	for i, k := range sequenceKey {
		diskPattern[i] = url.QueryEscape(k)
	}
	seqWildcard := "#>" // ascending sequence wildcard
	if after == -1 {
		// find the latest in sequence
		// use descending sequence wildcard
		seqWildcard = "#<"
	}
	diskPattern[n-1] = seqWildcard
	diskPattern[n] = "active*"

	var a Assertion
	candCb := func(diskPrimaryPaths []string) error {
		a = mylog.Check2(fsbs.pickLatestAssertion(assertType, diskPrimaryPaths, maxFormat))
		if err == errNotFound {
			return nil
		}

		return errFound
	}

	assertTypeTop := filepath.Join(fsbs.top, assertType.Name)
	mylog.Check(findWildcard(assertTypeTop, diskPattern, after, candCb))
	if err == errFound {
		return a.(SequenceMember), nil
	}

	return nil, &NotFoundError{Type: assertType}
}
