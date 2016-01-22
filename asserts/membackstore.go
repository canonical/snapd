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
	"fmt"
	"sync"
)

type memoryBackstore struct {
	top memBSLevel
	mu  sync.RWMutex
}

type memBSLevel map[string]interface{}

func (lev memBSLevel) put(key []string, assert Assertion) error {
	key0 := key[0]
	if len(key) > 1 {
		down, ok := lev[key0].(memBSLevel)
		if !ok {
			down = make(memBSLevel)
			lev[key0] = down
		}
		return down.put(key[1:], assert)
	}
	cur, ok := lev[key0].(Assertion)
	if ok {
		rev := assert.Revision()
		curRev := cur.Revision()
		if curRev >= rev {
			return fmt.Errorf("assertion added must have more recent revision than current one (adding %d, currently %d)", rev, curRev)
		}
	}
	lev[key0] = assert
	return nil
}

func (lev memBSLevel) get(key []string) (Assertion, error) {
	key0 := key[0]
	if len(key) > 1 {
		down, ok := lev[key0].(memBSLevel)
		if !ok {
			return nil, ErrNotFound
		}
		return down.get(key[1:])
	}

	cur, ok := lev[key0].(Assertion)
	if !ok {
		return nil, ErrNotFound
	}
	return cur, nil
}

func (lev memBSLevel) search(hint []string, found func(Assertion)) {
	hint0 := hint[0]
	if len(hint) > 1 {
		if hint0 == "" {
			for _, down := range lev {
				down.(memBSLevel).search(hint[1:], found)
			}
			return
		}
		down, ok := lev[hint0].(memBSLevel)
		if ok {
			down.search(hint[1:], found)
		}
		return
	}

	if hint0 == "" {
		for _, a := range lev {
			found(a.(Assertion))
		}
		return
	}

	cur, ok := lev[hint0].(Assertion)
	if ok {
		found(cur)
	}
}

// NewMemoryBackstore creates a memory backed assertions backstore.
func NewMemoryBackstore() Backstore {
	return &memoryBackstore{
		top: make(memBSLevel),
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
