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

package patterns_test

import (
	"bytes"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/prompting/patterns"
)

type renderSuite struct{}

var _ = Suite(&renderSuite{})

func (s *renderSuite) TestRender(c *C) {
	pattern := "/{,usr/}lib{,32,64,x32}/{,@{multiarch}/{,atomics/}}ld{-*,64}.so*"
	scanned, err := patterns.Scan(pattern)
	c.Assert(err, IsNil)
	parsed, err := patterns.Parse(scanned)
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
	patterns.RenderAllVariants(parsed, func(i int, buf *bytes.Buffer) {
		expansions = append(expansions, buf.String())
	})

	c.Check(expansions, DeepEquals, expectedExpansions)

	c.Check(expansions, HasLen, parsed.NumVariants())
}
