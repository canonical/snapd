// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

// LevenshteinDistance returns the Levenshtein edit distance between two
// strings, that is the minimum number of single-rune insertions, deletions or
// substitutions required to turn s into t.
func LevenshteinDistance(s, t string) int {
	sr := []rune(s)
	tr := []rune(t)

	if len(sr) == 0 {
		return len(tr)
	}
	if len(tr) == 0 {
		return len(sr)
	}

	// only two rows are needed at a time
	prev := make([]int, len(tr)+1)
	curr := make([]int, len(tr)+1)
	for j := range prev {
		prev[j] = j
	}

	for i := 1; i <= len(sr); i++ {
		curr[0] = i
		for j := 1; j <= len(tr); j++ {
			cost := 1
			if sr[i-1] == tr[j-1] {
				cost = 0
			}
			dist := prev[j-1] + cost
			if v := curr[j-1] + 1; v < dist {
				dist = v
			}
			if v := prev[j] + 1; v < dist {
				dist = v
			}
			curr[j] = dist
		}
		prev, curr = curr, prev
	}

	return prev[len(tr)]
}
