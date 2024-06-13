// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package patterns_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting/patterns"
)

type parseSuite struct{}

var _ = Suite(&parseSuite{})

func (s *parseSuite) TestParse(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"

	handMadeTree := patterns.Seq{
		patterns.Literal("/"),
		patterns.Alt{
			patterns.Literal(""),
			patterns.Literal("usr/"),
		},
		patterns.Literal("lib"),
		patterns.Alt{
			patterns.Literal(""),
			patterns.Literal("32"),
			patterns.Literal("64"),
			patterns.Literal("x32"),
		},
		patterns.Literal("/"),
		patterns.Alt{
			patterns.Literal(""),
			patterns.Seq{
				patterns.Literal("@multiarch/"),
				patterns.Alt{
					patterns.Literal(""),
					patterns.Literal("atomics/"),
				},
			},
		},
		patterns.Literal("ld"),
		patterns.Alt{
			patterns.Literal("-*"),
			patterns.Literal("64"),
		},
		patterns.Literal(".so*"),
	}

	tokens, err := patterns.Scan(pattern)
	c.Assert(err, IsNil)
	tree, err := patterns.Parse(tokens)
	c.Assert(err, IsNil)
	c.Check(tree, DeepEquals, handMadeTree)
}
