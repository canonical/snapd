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

package intset

import (
	"math/bits"
)

// IntSet represents a set of non-negative integer values. The zero value is
// ready for use and represents an empty set.
type IntSet[T ~int] struct {
	words []uint64
}

// wordAndBit returns the word index (value / 64) and bit offset (value % 64)
// for the provided value. These indexes are used to lookup our value in the
// set.
func wordAndBit[T ~int](value T) (word T, bit T) {
	if value < 0 {
		panic("intset: negative values not supported")
	}
	return value / 64, value % 64
}

// Add adds value to the set. Add will panic if the value is negative.
func (is *IntSet[T]) Add(value T) {
	word, bit := wordAndBit(value)

	// grow the slice if the target word does not yet exist
	if int(word) >= len(is.words) {
		cp := make([]uint64, word+1)
		copy(cp, is.words)
		is.words = cp
	}

	is.words[word] |= 1 << bit
}

// Contains returns true if value is present in the set.
func (is *IntSet[T]) Contains(value T) bool {
	if value < 0 {
		return false
	}

	word, bit := wordAndBit(value)
	if int(word) >= len(is.words) {
		return false
	}

	return is.words[word]&(1<<bit) != 0
}

// Remove removes value from the set.
func (is *IntSet[T]) Remove(value T) {
	if value < 0 {
		return
	}

	word, bit := wordAndBit(value)
	if int(word) < len(is.words) {
		is.words[word] &^= 1 << bit
	}
}

// All returns a slice containing all values in the set.
func (is *IntSet[T]) All() []T {
	result := make([]T, 0, is.Count())
	is.Range(func(value T) bool {
		result = append(result, value)
		return true
	})
	return result
}

// Diff returns a new [IntSet] containing elements that are set in this set but
// not in the given set.
func (is *IntSet[T]) Diff(other *IntSet[T]) *IntSet[T] {
	diff := make([]uint64, len(is.words))

	for i, w := range is.words {
		if i < len(other.words) {
			diff[i] = w &^ other.words[i]
		} else {
			diff[i] = w
		}
	}

	// trim trailing zeroes
	n := len(diff)
	for n > 0 && diff[n-1] == 0 {
		n--
	}

	return &IntSet[T]{words: diff[:n]}
}

// Range calls fn for every value present in the set. If fn returns false,
// iteration stops early.
//
// TODO:GOVERSION: consider using the new range functionality from go 1.23 once possible
func (is *IntSet[T]) Range(fn func(value T) bool) {
	for wi, word := range is.words {
		for word != 0 {
			// find the index of the least significant set bit
			zeroes := bits.TrailingZeros64(word)
			value := T(wi*64 + zeroes)

			// quit early if the given function returns false
			if !fn(value) {
				return
			}

			// clear the least significant set bit. once this word doesn't have
			// any more bits set, we'll break out of this loop.
			word &= word - 1
		}
	}
}

// Count returns the total number of values contained in the set.
func (is *IntSet[T]) Count() int {
	var total int
	for _, w := range is.words {
		total += bits.OnesCount64(w)
	}
	return total
}

// Equal returns true if this set and the given set contain the same set of
// values.
func (is *IntSet[T]) Equal(other *IntSet[T]) bool {
	if is == other {
		return true
	}

	// here we cannot determine that the sets are unequal just because the
	// length of words is not the same. one of the sets might have a value in
	// words that once contained something but has been cleared. this could
	// result in an empty word, which is the same as a non-existent word.
	for i := 0; i < max(len(is.words), len(other.words)); i++ {
		var word uint64
		if i < len(is.words) {
			word = is.words[i]
		}

		var otherWord uint64
		if i < len(other.words) {
			otherWord = other.words[i]
		}

		if word != otherWord {
			return false
		}
	}

	return true
}

// max returns the larger of the two given values.
//
// TODO:GOVERSION: remove once we are on go>=1.21
func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
