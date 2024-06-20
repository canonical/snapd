// -*- Mode: Go; indent-tabs-mode: t -*-

/*",
 * Copyright (C) 2024 Canonical Ltd
 *",
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *",
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *",
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, Text: see <http://www.gnu.org/licenses/>.
 *",
 */

package patterns

import (
	"bytes"

	. "gopkg.in/check.v1"
)

type renderSuite struct{}

var _ = Suite(&renderSuite{})

func (s *renderSuite) TestRenderAllVariants(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"
	scanned, err := scan(pattern)
	c.Assert(err, IsNil)
	parsed, err := parse(scanned)
	c.Assert(err, IsNil)

	expectedExpansions := []string{
		"/lib/ld-*.so*",
		"/lib/ld64.so*",
		"/lib/@multiarch/ld-*.so*",
		"/lib/@multiarch/ld64.so*",
		"/lib/@multiarch/atomics/ld-*.so*",
		"/lib/@multiarch/atomics/ld64.so*",
		"/lib32/ld-*.so*",
		"/lib32/ld64.so*",
		"/lib32/@multiarch/ld-*.so*",
		"/lib32/@multiarch/ld64.so*",
		"/lib32/@multiarch/atomics/ld-*.so*",
		"/lib32/@multiarch/atomics/ld64.so*",
		"/lib64/ld-*.so*",
		"/lib64/ld64.so*",
		"/lib64/@multiarch/ld-*.so*",
		"/lib64/@multiarch/ld64.so*",
		"/lib64/@multiarch/atomics/ld-*.so*",
		"/lib64/@multiarch/atomics/ld64.so*",
		"/libx32/ld-*.so*",
		"/libx32/ld64.so*",
		"/libx32/@multiarch/ld-*.so*",
		"/libx32/@multiarch/ld64.so*",
		"/libx32/@multiarch/atomics/ld-*.so*",
		"/libx32/@multiarch/atomics/ld64.so*",
		"/usr/lib/ld-*.so*",
		"/usr/lib/ld64.so*",
		"/usr/lib/@multiarch/ld-*.so*",
		"/usr/lib/@multiarch/ld64.so*",
		"/usr/lib/@multiarch/atomics/ld-*.so*",
		"/usr/lib/@multiarch/atomics/ld64.so*",
		"/usr/lib32/ld-*.so*",
		"/usr/lib32/ld64.so*",
		"/usr/lib32/@multiarch/ld-*.so*",
		"/usr/lib32/@multiarch/ld64.so*",
		"/usr/lib32/@multiarch/atomics/ld-*.so*",
		"/usr/lib32/@multiarch/atomics/ld64.so*",
		"/usr/lib64/ld-*.so*",
		"/usr/lib64/ld64.so*",
		"/usr/lib64/@multiarch/ld-*.so*",
		"/usr/lib64/@multiarch/ld64.so*",
		"/usr/lib64/@multiarch/atomics/ld-*.so*",
		"/usr/lib64/@multiarch/atomics/ld64.so*",
		"/usr/libx32/ld-*.so*",
		"/usr/libx32/ld64.so*",
		"/usr/libx32/@multiarch/ld-*.so*",
		"/usr/libx32/@multiarch/ld64.so*",
		"/usr/libx32/@multiarch/atomics/ld-*.so*",
		"/usr/libx32/@multiarch/atomics/ld64.so*",
	}

	expansions := make([]string, 0, len(expectedExpansions))
	RenderAllVariants(parsed, func(i int, buf *bytes.Buffer) {
		expansions = append(expansions, buf.String())
	})

	c.Check(expansions, DeepEquals, expectedExpansions)

	c.Check(expansions, HasLen, parsed.NumVariants())
}

func (s *renderSuite) TestNextEx(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"
	scanned, err := scan(pattern)
	c.Assert(err, IsNil)
	parsed, err := parse(scanned)
	c.Assert(err, IsNil)

	expected := []struct {
		length     int
		truncateTo int
	}{
		// Starts with /lib/ld-*.so*
		{13, 7},  // /lib/ld64.so*
		{24, 5},  // /lib/@multiarch/ld-*.so*
		{24, 18}, // /lib/@multiarch/ld64.so*
		{32, 16}, // /lib/@multiarch/atomics/ld-*.so*
		{32, 26}, // /lib/@multiarch/atomics/ld64.so*
		{15, 4},  // /lib32/ld-*.so*
		{15, 9},  // /lib32/ld64.so*
		{26, 7},  // /lib32/@multiarch/ld-*.so*
		{26, 20}, // /lib32/@multiarch/ld64.so*
		{34, 18}, // /lib32/@multiarch/atomics/ld-*.so*
		{34, 28}, // /lib32/@multiarch/atomics/ld64.so*
		{15, 4},  // /lib64/ld-*.so*
		{15, 9},  // /lib64/ld64.so*
		{26, 7},  // /lib64/@multiarch/ld-*.so*
		{26, 20}, // /lib64/@multiarch/ld64.so*
		{34, 18}, // /lib64/@multiarch/atomics/ld-*.so*
		{34, 28}, // /lib64/@multiarch/atomics/ld64.so*
		{16, 4},  // /libx32/ld-*.so*
		{16, 10}, // /libx32/ld64.so*
		{27, 8},  // /libx32/@multiarch/ld-*.so*
		{27, 21}, // /libx32/@multiarch/ld64.so*
		{35, 19}, // /libx32/@multiarch/atomics/ld-*.so*
		{35, 29}, // /libx32/@multiarch/atomics/ld64.so*
		{17, 1},  // /usr/lib/ld-*.so*
		{17, 11}, // /usr/lib/ld64.so*
		{28, 9},  // /usr/lib/@multiarch/ld-*.so*
		{28, 22}, // /usr/lib/@multiarch/ld64.so*
		{36, 20}, // /usr/lib/@multiarch/atomics/ld-*.so*
		{36, 30}, // /usr/lib/@multiarch/atomics/ld64.so*
		{19, 8},  // /usr/lib32/ld-*.so*
		{19, 13}, // /usr/lib32/ld64.so*
		{30, 11}, // /usr/lib32/@multiarch/ld-*.so*
		{30, 24}, // /usr/lib32/@multiarch/ld64.so*
		{38, 22}, // /usr/lib32/@multiarch/atomics/ld-*.so*
		{38, 32}, // /usr/lib32/@multiarch/atomics/ld64.so*
		{19, 8},  // /usr/lib64/ld-*.so*
		{19, 13}, // /usr/lib64/ld64.so*
		{30, 11}, // /usr/lib64/@multiarch/ld-*.so*
		{30, 24}, // /usr/lib64/@multiarch/ld64.so*
		{38, 22}, // /usr/lib64/@multiarch/atomics/ld-*.so*
		{38, 32}, // /usr/lib64/@multiarch/atomics/ld64.so*
		{20, 8},  // /usr/libx32/ld-*.so*
		{20, 14}, // /usr/libx32/ld64.so*
		{31, 12}, // /usr/libx32/@multiarch/ld-*.so*
		{31, 25}, // /usr/libx32/@multiarch/ld64.so*
		{39, 23}, // /usr/libx32/@multiarch/atomics/ld-*.so*
		{39, 33}, // /usr/libx32/@multiarch/atomics/ld64.so*
	}

	conf := parsed.Config()
	for _, next := range expected {
		length, truncateTo, moreRemain := conf.NextEx(parsed)
		c.Check(length, Equals, next.length)
		c.Check(truncateTo, Equals, next.truncateTo)
		c.Check(moreRemain, Equals, true)
	}

	length, truncateTo, moreRemain := conf.NextEx(parsed)
	c.Check(length, Equals, 0)
	c.Check(truncateTo, Equals, 0)
	c.Check(moreRemain, Equals, false)
}
