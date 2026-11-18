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

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type damerauLevenshteinSuite struct{}

var _ = Suite(&damerauLevenshteinSuite{})

func (s *damerauLevenshteinSuite) TestDamerauLevenshteinDistance(c *C) {
	for _, tc := range []struct {
		a, b string
		d    int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"install", "install", 0},
		{"instal", "install", 1},
		{"install", "instal", 1},
		{"kitten", "sitting", 3},
		// transpositions of two adjacent runes are a single edit
		{"debug", "debgu", 1},
		{"ack", "akc", 1},
		{"ab", "ba", 1},
		{"abcd", "badc", 2},
		// but only as long as no substring is edited more than once,
		// which keeps this variant simple;
		// "aks" -> "ack" needs two substitutions
		{"aks", "ack", 2},
		{"żółw", "zolw", 3},
	} {
		comment := Commentf("%q vs %q", tc.a, tc.b)
		c.Check(strutil.DamerauLevenshteinDistance(tc.a, tc.b), Equals, tc.d, comment)
		// distance is symmetric
		c.Check(strutil.DamerauLevenshteinDistance(tc.b, tc.a), Equals, tc.d, comment)
	}
}

type closestMatchSuite struct{}

var _ = Suite(&closestMatchSuite{})

func (s *closestMatchSuite) TestClosestMatch(c *C) {
	for _, tc := range []struct {
		s          string
		candidates []string
		match      string
	}{
		// no candidates at all
		{"install", nil, ""},
		{"install", []string{}, ""},
		// a single candidate close enough
		{"instal", []string{"install"}, "install"},
		{"instal", []string{"remove", "install", "refresh"}, "install"},
		{"akc", []string{"ack", "attach"}, "ack"},
		// clearly unrelated candidates
		{"instal", []string{"whoami", "remove"}, ""},
		{"", []string{"install"}, ""},
		// matching is case-sensitive, callers are expected to
		// normalize the input
		{"INSTAL", []string{"install"}, ""},
		// "ack" and "tasks" are both two edits away, but "ack"
		// needs more than half of its runes edited, so it is not
		// eligible and must not shadow the closer "tasks"
		{"aks", []string{"ack", "tasks"}, "tasks"},
		// exactly half of the runes edited is not close enough
		{"stat", []string{"snap"}, ""},
		{"sna", []string{"snap"}, "snap"},
		// when several candidates are equally close, the first
		// one in the given order wins
		{"cap", []string{"cat", "car"}, "cat"},
		{"cap", []string{"car", "cat"}, "car"},
		// the closer of two related candidates wins
		{"chage", []string{"changes", "change"}, "change"},
	} {
		comment := Commentf("%q from %q", tc.s, tc.candidates)
		c.Check(strutil.ClosestMatch(tc.s, tc.candidates), Equals, tc.match, comment)
	}
}

func (s *closestMatchSuite) TestClosestMatchEmptyCandidates(c *C) {
	// an empty candidate is never a match and must not cause division
	// by zero
	c.Check(strutil.ClosestMatch("", []string{""}), Equals, "")
	c.Check(strutil.ClosestMatch("abc", []string{"", "abc"}), Equals, "abc")
}
