// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// Intersection computes the intersection of a set of slices, treating each
// slice as a set. It does not mutate any of the input slices and returns a new
// slice. It is recursive.
func Intersection(slices ...[]string) []string {
	// handle trivial cases
	switch len(slices) {
	case 0:
		return nil
	case 1:
		return slices[0]
	case 2:
		// actually perform the intersection
		l1 := slices[0]
		l2 := slices[1]
		guessLen := len(l1)
		if len(l1) > len(l2) {
			guessLen = len(l2)
		}
		alreadyAdded := map[string]bool{}
		result := make([]string, 0, guessLen)
		for _, item := range l1 {
			if !alreadyAdded[item] && ListContains(l2, item) {
				result = append(result, item)
				alreadyAdded[item] = true
			}
		}
		return result
	}

	// all other cases require some recursion operating on smaller chunks

	// we take advantage of the fact that intersection is commutative and
	// iteratively perform an intersection between a running intersection of
	// all previous lists and the next list in the total set of slices

	// TODO: this could be sped up with maps or any number of things, but
	// hopefully this is only ever used on a few lists that are small in size
	// so we can get away with this inefficient implementation
	result := slices[0]
	for _, s := range slices[1:] {
		result = Intersection(result, s)
	}
	return result
}
