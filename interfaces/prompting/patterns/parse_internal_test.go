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

package patterns

import (
	. "gopkg.in/check.v1"
)

type parseSuite struct{}

var _ = Suite(&parseSuite{})

func (s *parseSuite) TestParse(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"

	handMadeTree := seq{
		literal("/"),
		alt{
			literal(""),
			literal("usr/"),
		},
		literal("lib"),
		alt{
			literal(""),
			literal("32"),
			literal("64"),
			literal("x32"),
		},
		literal("/"),
		alt{
			literal(""),
			seq{
				literal("@multiarch/"),
				alt{
					literal(""),
					literal("atomics/"),
				},
			},
		},
		literal("ld"),
		alt{
			literal("-*"),
			literal("64"),
		},
		literal(".so*"),
	}

	tokens, err := scan(pattern)
	c.Assert(err, IsNil)
	tree, err := parse(tokens)
	c.Assert(err, IsNil)
	c.Check(tree, DeepEquals, handMadeTree)
}
