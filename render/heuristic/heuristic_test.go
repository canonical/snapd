// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package heuristic_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/render/heuristic"
)

func Test(t *testing.T) { TestingT(t) }

type heuristicSuite struct{}

var _ = Suite(&heuristicSuite{})

func (s *heuristicSuite) TestRuneWidth(c *C) {
	c.Check(heuristic.RuneWidth('a'), Equals, 1)
	c.Check(heuristic.RuneWidth('ф'), Equals, 1)
	c.Check(heuristic.RuneWidth('ひ'), Equals, 2)
	c.Check(heuristic.RuneWidth('カ'), Equals, 2)
	c.Check(heuristic.RuneWidth('漢'), Equals, 2)
	c.Check(heuristic.RuneWidth('한'), Equals, 2)
	c.Check(heuristic.RuneWidth('ע'), Equals, 1)
	c.Check(heuristic.RuneWidth('λ'), Equals, 1)
	c.Check(heuristic.RuneWidth('\n'), Equals, 0)
}

func (s *heuristicSuite) TestTerminalRenderSize(c *C) {
	for _, t := range []struct {
		s    string
		w, h int
	}{
		{s: "", w: 0, h: 1},
		{s: "alphabet", w: 8, h: 1},
		{s: "алфавит", w: 7, h: 1},
		{s: "ひらがな", w: 8, h: 1},
		{s: "カタカナ", w: 8, h: 1},
		{s: "漢字", w: 4, h: 1},
		{s: "한글", w: 4, h: 1},
		{s: "עברית", w: 5, h: 1},
		{s: "Ελληνική", w: 8, h: 1},
		{s: "\n", w: 0, h: 2},
		{s: "\t", w: 8, h: 1},
		{s: "\v", w: 0, h: 2},
		{s: "hi\r", w: 2, h: 1},
		{s: "hi\rt", w: 2, h: 1},
		{s: "hi\rthere", w: 5, h: 1},
		{s: "hi\b", w: 1, h: 1},
		{s: "1 2 3", w: 5, h: 1},
	} {
		comment := Commentf("s: %q", t.s)
		w, h := heuristic.TerminalRenderSize(t.s)
		c.Check(w, Equals, t.w, comment)
		c.Check(h, Equals, t.h, comment)
	}
}
