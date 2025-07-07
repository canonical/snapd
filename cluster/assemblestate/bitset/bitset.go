package bitset

import (
	"math/bits"
)

// Bitset represents a set of integer values. The zero value is ready for use
// and represents an empty set.
type Bitset[T ~int] struct {
	words []uint64
}

// wordAndBit returns the word index (value / 64) and bit offset (value % 64)
// for the provided value. These indexes are used to lookup our value in the
// bitset.
func wordAndBit[T ~int](value T) (word T, bit T) {
	return value / 64, value % 64
}

// Set adds value to the bitset.
func (b *Bitset[T]) Set(value T) {
	word, bit := wordAndBit(value)

	// grow the slice if the target word does not yet exist
	if int(word) >= len(b.words) {
		cp := make([]uint64, word+1)
		copy(cp, b.words)
		b.words = cp
	}

	b.words[word] |= 1 << bit
}

// Has reports whether value is present in the bitset.
func (bs *Bitset[T]) Has(value T) bool {
	word, bit := wordAndBit(value)

	if int(word) >= len(bs.words) {
		return false
	}

	return bs.words[word]&(1<<bit) != 0
}

// Clear removes value from the bitset.
func (b *Bitset[T]) Clear(value T) {
	word, bit := wordAndBit(value)

	if int(word) < len(b.words) {
		b.words[word] &^= 1 << bit
	}
}

// All returns a slice containing every value currently set.
func (b *Bitset[T]) All() []T {
	result := make([]T, 0, b.Count())
	b.Range(func(value T) bool {
		result = append(result, value)
		return true
	})
	return result
}

// Diff returns a new bitset containing elements that are set in this bitset but
// not in the given bitset.
func (b *Bitset[T]) Diff(other *Bitset[T]) *Bitset[T] {
	diff := make([]uint64, len(b.words))

	for i, w := range b.words {
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

	return &Bitset[T]{words: diff[:n]}
}

// Range calls fn for every value present in the bitset. If fn returns false,
// iteration stops early.
func (b *Bitset[T]) Range(fn func(value T) bool) {
	for wi, word := range b.words {
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

// Count returns the total number of values contained in the bitset.
func (b *Bitset[T]) Count() int {
	var total int
	for _, w := range b.words {
		total += bits.OnesCount64(w)
	}
	return total
}

// Equals returns true if this bitset and the given bitset are identical.
func (b *Bitset[T]) Equals(other *Bitset[T]) bool {
	for i := 0; i < max(len(b.words), len(other.words)); i++ {
		var word uint64
		if i < len(b.words) {
			word = b.words[i]
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

func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
