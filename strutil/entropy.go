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

package strutil

import (
	"math"
	"strings"
)

var symbolPools = []string{
	`abcdefghijklmnopqrstuvwxyz`, // lowercase
	`ABCDEFGHIJKLMNOPQRSTUVWXYZ`, // uppercase
	`0123456789`,                 // digits
	`"#%'()+/:;<=>?[\]^{|}~`,     // special characters
	`_-., `,                      // separator characters
	`!@$&*`,                      // replace characters (e.g. S -> $)
}

func getBase(s string) (base int) {
	// find the total size of the rune corpus composed of unique pools that
	// match individual runes form the string
	matchedSymbolPools := make(map[string]struct{}, 0)
	for _, r := range s {
		matched := false
		for _, pool := range symbolPools {
			if strings.ContainsRune(pool, r) {
				matched = true
				if _, ok := matchedSymbolPools[pool]; !ok {
					base += len([]rune(pool))
					matchedSymbolPools[pool] = struct{}{}
				}
				break
			}
		}
		if !matched {
			// Account for non-ASCII characters as a pool of size one.
			// FIXME: A better unicode-aware approach is needed.
			if _, ok := matchedSymbolPools[string(r)]; !ok {
				base += 1
			}
		}
	}
	return base
}

func getPrunedLength(s string) int {
	runes := []rune(s)
	// remove more than two repeating chars
	for i := 0; i < len(runes)-2; i++ {
		for i < len(runes)-2 && runes[i] == runes[i+1] && runes[i] == runes[i+2] {
			runes = removeChar(runes, i)
		}
	}
	// FIXME: Prune common character patterns (e.g. qwerty,1234) from counting towards the length.
	return len(runes)
}

func removeChar(s []rune, idx int) []rune {
	if idx >= len(s) || idx < 0 {
		return s
	}
	return append(s[0:idx], s[idx+1:]...)
}

// Entropy returns a heuristic value of the passed string entropy.
func Entropy(s string) float64 {
	base := getBase(s)
	length := getPrunedLength(s)
	return math.Log2(float64(base)) * float64(length)
}
