// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"sync"
)

type memoryBackstore struct {
	top memBSBranch
	mu  sync.RWMutex
}

type memBSNode interface {
	put(key []string, assert Assertion) error
	get(key []string) (Assertion, error)
	search(hint []string, found func(Assertion))
}

type memBSBranch map[string]memBSNode

type memBSLeaf map[string]Assertion

func (br memBSBranch) put(key []string, assert Assertion) error {
	key0 := key[0]
	down := br[key0]
	if down == nil {
		if len(key) > 2 {
			down = make(memBSBranch)
		} else {
			down = make(memBSLeaf)
		}
		br[key0] = down
	}
	return down.put(key[1:], assert)
}

func (leaf memBSLeaf) put(key []string, assert Assertion) error {
	key0 := key[0]
	cur := leaf[key0]
	if cur != nil {
		rev := assert.Revision()
		curRev := cur.Revision()
		if curRev >= rev {
			return &RevisionError{Current: curRev, Used: rev}
		}
	}
	leaf[key0] = assert
	return nil
}

func (br memBSBranch) get(key []string) (Assertion, error) {
	key0 := key[0]
	down := br[key0]
	if down == nil {
		return nil, ErrNotFound
	}
	return down.get(key[1:])
}

func (leaf memBSLeaf) get(key []string) (Assertion, error) {
	key0 := key[0]
	cur := leaf[key0]
	if cur == nil {
		return nil, ErrNotFound
	}
	return cur, nil
}

func (br memBSBranch) search(hint []string, found func(Assertion)) {
	hint0 := hint[0]
	if hint0 == "" {
		for _, down := range br {
			down.search(hint[1:], found)
		}
		return
	}
	down := br[hint0]
	if down != nil {
		down.search(hint[1:], found)
	}
	return
}

func (leaf memBSLeaf) search(hint []string, found func(Assertion)) {
	hint0 := hint[0]
	if hint0 == "" {
		for _, a := range leaf {
			found(a)
		}
		return
	}

	cur := leaf[hint0]
	if cur != nil {
		found(cur)
	}
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

	internalKey := make([]string, 1+len(assertType.PrimaryKey))
	internalKey[0] = assertType.Name
	for i, name := range assertType.PrimaryKey {
		internalKey[1+i] = assert.Header(name)
	}

	err := mbs.top.put(internalKey, assert)
	return err
}

func (mbs *memoryBackstore) Get(assertType *AssertionType, key []string) (Assertion, error) {
	mbs.mu.RLock()
	defer mbs.mu.RUnlock()

	internalKey := make([]string, 1+len(assertType.PrimaryKey))
	internalKey[0] = assertType.Name
	copy(internalKey[1:], key)

	return mbs.top.get(internalKey)
}

func (mbs *memoryBackstore) Search(assertType *AssertionType, headers map[string]string, foundCb func(Assertion)) error {
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

	mbs.top.search(hint, candCb)
	return nil
}
