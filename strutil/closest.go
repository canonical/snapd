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

import "unicode/utf8"

// DamerauLevenshteinDistance returns the Damerau-Levenshtein distance between
// two strings, that is the minimum number of single-rune insertions, deletions,
// substitutions or transpositions of two adjacent runes required to turn s
// into t. It is the optimal string alignment (restricted) variant, in which a
// substring is not edited more than once.
func DamerauLevenshteinDistance(s, t string) int {
	sr := []rune(s)
	tr := []rune(t)

	if len(sr) == 0 {
		return len(tr)
	}
	if len(tr) == 0 {
		return len(sr)
	}

	// only three rows are needed at a time, the third one for the row
	// before the previous one, which is consulted for transpositions
	prev2 := make([]int, len(tr)+1)
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
			// substitution, insertion or deletion of a single rune
			dist := prev[j-1] + cost
			if v := curr[j-1] + 1; v < dist {
				dist = v
			}
			if v := prev[j] + 1; v < dist {
				dist = v
			}
			// transposition of two adjacent runes
			if i > 1 && j > 1 && sr[i-1] == tr[j-2] && sr[i-2] == tr[j-1] {
				if v := prev2[j-2] + 1; v < dist {
					dist = v
				}
			}
			curr[j] = dist
		}
		prev2, prev, curr = prev, curr, prev2
	}

	return prev[len(tr)]
}

// ClosestMatch returns a closest match of s from a given set of candidates, or
// an empty string if no candidates were found to be sufficiently close. A
// candidate is only returned when the edit distance relative to its length is
// small enough so that clearly unrelated matches do not yield a suggestion.
// Candidates are matched case-sensitively and, when several of them are
// equally close, the first one in the given order is returned.
func ClosestMatch(s string, candidates []string) string {
	best := ""
	bestDist := -1
	for _, cand := range candidates {
		dist := DamerauLevenshteinDistance(s, cand)
		// keep candidates where fewer than half of the runes need to
		// be edited, that is 2*dist < len; clearly unrelated ones are
		// not useful matches, and must not shadow a closer eligible
		// one
		if l := utf8.RuneCountInString(cand); 2*dist >= l {
			continue
		}
		if bestDist < 0 || dist < bestDist {
			best, bestDist = cand, dist
		}
	}
	if bestDist < 0 {
		return ""
	}
	return best
}
