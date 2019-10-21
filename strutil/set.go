// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package strutil

// OrderedSet is a set of strings that maintains the order of insertion.
//
// External synchronization is required for safe concurrent access.
type OrderedSet struct {
	positionOf map[string]int
}

// Items returns a slice of strings representing insertion order.
//
// Contains is O(N) in the size of the set.
func (o *OrderedSet) Items() []string {
	if o.positionOf == nil {
		return nil
	}
	items := make([]string, len(o.positionOf))
	for item, idx := range o.positionOf {
		items[idx] = item
	}
	return items
}

// Contains returns true if the set contains a given item.
//
// Contains is O(1) in the size of the set.
func (o *OrderedSet) Contains(item string) bool {
	if o.positionOf == nil {
		return false
	}

	_, ok := o.positionOf[item]
	return ok
}

// IndexOf returns the position of an item in the set.
func (o *OrderedSet) IndexOf(item string) (idx int, ok bool) {
	idx, ok = o.positionOf[item]
	return idx, ok
}

// Del removes an item from the set.
//
// Del is O(1) in the size of the set.
func (o *OrderedSet) Del(item string) {
	if o.positionOf == nil {
		return
	}

	delete(o.positionOf, item)
	if len(o.positionOf) == 0 {
		o.positionOf = nil
	}
}

// Put adds an item into the set.
//
// If the item was not present then it is stored and ordered after all existing
// elements. If the item was already present its position is not changed.
//
// Put is O(1) in the size of the set.
func (o *OrderedSet) Put(item string) {
	if o.positionOf == nil {
		o.positionOf = make(map[string]int)
	}

	if _, ok := o.positionOf[item]; ok {
		return
	}

	o.positionOf[item] = len(o.positionOf)
}

// Size returns the number of elements in the set.
func (o *OrderedSet) Size() int {
	return len(o.positionOf)
}
