// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package bimap

// Bimap provides a bidirectional mapping between a value and an index in a
// slice. Lookups in either direction are O(1).
type Bimap[V comparable, I ~int] struct {
	values  []V
	indexes map[V]I
}

// New returns an empty Bimap.
func New[V comparable, I ~int]() *Bimap[V, I] {
	return &Bimap[V, I]{
		indexes: make(map[V]I),
	}
}

// Add inserts v into the map if it is not already present and returns the index
// associated with it. This method is idempotent.
func (bm *Bimap[V, I]) Add(v V) I {
	if idx, exists := bm.indexes[v]; exists {
		return idx
	}

	index := I(len(bm.values))
	bm.values = append(bm.values, v)
	bm.indexes[v] = index

	return index
}

// IndexOf returns the index previously assigned to v by Add. We return the
// index and true if v exists, false otherwise.
func (bm *Bimap[V, I]) IndexOf(v V) (I, bool) {
	idx, exists := bm.indexes[v]
	return idx, exists
}

// Value returns the value stored at index i. This method panics if the index is
// not associated with a value in the map.
func (bm *Bimap[V, I]) Value(i I) V {
	return bm.values[i]
}

// Values returns a slice containing all values in insertion order. Callers must
// not mutate the returned slice. Future calls to [Add] may invalidate the
// returned slice.
func (bm *Bimap[V, I]) Values() []V {
	return bm.values
}
