// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"sort"
	"strconv"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
)

type memoryBackstore struct {
	top memBSBranch
	mu  sync.RWMutex
}

type memBSNode interface {
	put(assertType *AssertionType, key []string, assert Assertion) error
	get(key []string, maxFormat int) (Assertion, error)
	search(hint []string, found func(Assertion), maxFormat int)
	sequenceMemberAfter(prefix []string, after, maxFormat int) (Assertion, error)
}

type memBSBranch map[string]memBSNode

type memBSLeaf map[string]map[int]Assertion

type memBSSeqLeaf struct {
	memBSLeaf
	sequence []int
}

func (br memBSBranch) put(assertType *AssertionType, key []string, assert Assertion) error {
	key0 := key[0]
	down := br[key0]
	if down == nil {
		if len(key) > 2 {
			down = make(memBSBranch)
		} else {
			leaf := make(memBSLeaf)
			if assertType.SequenceForming() {
				down = &memBSSeqLeaf{memBSLeaf: leaf}
			} else {
				down = leaf
			}
		}
		br[key0] = down
	}
	return down.put(assertType, key[1:], assert)
}

func (leaf memBSLeaf) cur(key0 string, maxFormat int) (a Assertion) {
	for formatnum, a1 := range leaf[key0] {
		if formatnum <= maxFormat {
			if a == nil || a1.Revision() > a.Revision() {
				a = a1
			}
		}
	}
	return a
}

func (leaf memBSLeaf) put(assertType *AssertionType, key []string, assert Assertion) error {
	key0 := key[0]
	cur := leaf.cur(key0, assertType.MaxSupportedFormat())
	if cur != nil {
		rev := assert.Revision()
		curRev := cur.Revision()
		if curRev >= rev {
			return &RevisionError{Current: curRev, Used: rev}
		}
	}
	if _, ok := leaf[key0]; !ok {
		leaf[key0] = make(map[int]Assertion)
	}
	leaf[key0][assert.Format()] = assert
	return nil
}

func (leaf *memBSSeqLeaf) put(assertType *AssertionType, key []string, assert Assertion) error {
	mylog.Check(leaf.memBSLeaf.put(assertType, key, assert))

	if len(leaf.memBSLeaf) != len(leaf.sequence) {
		seqnum := assert.(SequenceMember).Sequence()
		inspos := sort.SearchInts(leaf.sequence, seqnum)
		n := len(leaf.sequence)
		leaf.sequence = append(leaf.sequence, seqnum)
		if inspos != n {
			copy(leaf.sequence[inspos+1:n+1], leaf.sequence[inspos:n])
			leaf.sequence[inspos] = seqnum
		}
	}
	return nil
}

// errNotFound is used internally by backends, it is converted to the richer
// NotFoundError only at their public interface boundary
var errNotFound = errors.New("assertion not found")

func (br memBSBranch) get(key []string, maxFormat int) (Assertion, error) {
	key0 := key[0]
	down := br[key0]
	if down == nil {
		return nil, errNotFound
	}
	return down.get(key[1:], maxFormat)
}

func (leaf memBSLeaf) get(key []string, maxFormat int) (Assertion, error) {
	key0 := key[0]
	cur := leaf.cur(key0, maxFormat)
	if cur == nil {
		return nil, errNotFound
	}
	return cur, nil
}

func (br memBSBranch) search(hint []string, found func(Assertion), maxFormat int) {
	hint0 := hint[0]
	if hint0 == "" {
		for _, down := range br {
			down.search(hint[1:], found, maxFormat)
		}
		return
	}
	down := br[hint0]
	if down != nil {
		down.search(hint[1:], found, maxFormat)
	}
}

func (leaf memBSLeaf) search(hint []string, found func(Assertion), maxFormat int) {
	hint0 := hint[0]
	if hint0 == "" {
		for key := range leaf {
			cand := leaf.cur(key, maxFormat)
			if cand != nil {
				found(cand)
			}
		}
		return
	}

	cur := leaf.cur(hint0, maxFormat)
	if cur != nil {
		found(cur)
	}
}

func (br memBSBranch) sequenceMemberAfter(prefix []string, after, maxFormat int) (Assertion, error) {
	prefix0 := prefix[0]
	down := br[prefix0]
	if down == nil {
		return nil, errNotFound
	}
	return down.sequenceMemberAfter(prefix[1:], after, maxFormat)
}

func (left memBSLeaf) sequenceMemberAfter(prefix []string, after, maxFormat int) (Assertion, error) {
	panic("internal error: unexpected sequenceMemberAfter on memBSLeaf")
}

func (leaf *memBSSeqLeaf) sequenceMemberAfter(prefix []string, after, maxFormat int) (Assertion, error) {
	n := len(leaf.sequence)
	dir := 1
	var start int
	if after == -1 {
		// search for the latest in sequence compatible with
		// maxFormat: consider all sequence numbers in
		// sequence backward
		dir = -1
		start = n - 1
	} else {
		// search for the first in sequence with sequence number
		// > after and compatible with maxFormat
		start = sort.SearchInts(leaf.sequence, after)
		if start == n {
			// nothing
			return nil, errNotFound
		}
		if leaf.sequence[start] == after {
			// skip after itself
			start += 1
		}
	}
	for j := start; j >= 0 && j < n; j += dir {
		seqkey := strconv.Itoa(leaf.sequence[j])
		cur := leaf.cur(seqkey, maxFormat)
		if cur != nil {
			return cur, nil
		}
	}
	return nil, errNotFound
}

// NewMemoryBackstore creates a memory backed assertions backstore.
func NewMemoryBackstore() Backstore {
	return &memoryBackstore{
		top: make(memBSBranch),
	}
}

func (mbs *memoryBackstore) Put(assertType *AssertionType, assert Assertion) error {
	mbs.mu.Lock()
	defer mbs.mu.Unlock()

	internalKey := make([]string, 1, 1+len(assertType.PrimaryKey))
	internalKey[0] = assertType.Name
	internalKey = append(internalKey, assert.Ref().PrimaryKey...)
	mylog.Check(mbs.top.put(assertType, internalKey, assert))
	return err
}

func (mbs *memoryBackstore) Get(assertType *AssertionType, key []string, maxFormat int) (Assertion, error) {
	mbs.mu.RLock()
	defer mbs.mu.RUnlock()

	n := len(assertType.PrimaryKey)
	if len(key) > n {
		return nil, fmt.Errorf("internal error: Backstore.Get given a key longer than expected for %q: %v", assertType.Name, key)
	}

	internalKey := make([]string, 1+len(assertType.PrimaryKey))
	internalKey[0] = assertType.Name
	copy(internalKey[1:], key)
	if len(key) < n {
		for kopt := len(key); kopt < n; kopt++ {
			defl := assertType.OptionalPrimaryKeyDefaults[assertType.PrimaryKey[kopt]]
			if defl == "" {
				return nil, fmt.Errorf("internal error: Backstore.Get given a key missing mandatory elements for %q: %v", assertType.Name, key)
			}
			internalKey[kopt+1] = defl
		}
	}

	a := mylog.Check2(mbs.top.get(internalKey, maxFormat))
	if err == errNotFound {
		return nil, &NotFoundError{Type: assertType}
	}
	return a, err
}

func (mbs *memoryBackstore) Search(assertType *AssertionType, headers map[string]string, foundCb func(Assertion), maxFormat int) error {
	mbs.mu.RLock()
	defer mbs.mu.RUnlock()

	hint := make([]string, 1+len(assertType.PrimaryKey))
	hint[0] = assertType.Name
	for i, name := range assertType.PrimaryKey {
		hint[1+i] = headers[name]
	}

	candCb := func(a Assertion) {
		if searchMatch(a, headers) {
			foundCb(a)
		}
	}

	mbs.top.search(hint, candCb, maxFormat)
	return nil
}

func (mbs *memoryBackstore) SequenceMemberAfter(assertType *AssertionType, sequenceKey []string, after, maxFormat int) (SequenceMember, error) {
	if !assertType.SequenceForming() {
		panic(fmt.Sprintf("internal error: SequenceMemberAfter on non sequence-forming assertion type %q", assertType.Name))
	}
	if len(sequenceKey) != len(assertType.PrimaryKey)-1 {
		return nil, fmt.Errorf("internal error: SequenceMemberAfter's sequence key argument length must be exactly 1 less than the assertion type primary key")
	}

	mbs.mu.RLock()
	defer mbs.mu.RUnlock()

	internalPrefix := make([]string, len(assertType.PrimaryKey))
	internalPrefix[0] = assertType.Name
	copy(internalPrefix[1:], sequenceKey)

	a := mylog.Check2(mbs.top.sequenceMemberAfter(internalPrefix, after, maxFormat))
	if err == errNotFound {
		return nil, &NotFoundError{Type: assertType}
	}
	return a.(SequenceMember), err
}
