// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package spdx_test

import (
	"bytes"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/spdx"
)

func (s *spdxSuite) TestScannerHappy(c *C) {
	for _, t := range []struct {
		inp    string
		tokens []string
	}{
		{"0BSD", []string{"0BSD"}},
		{"0BSD OR GPL-2.0", []string{"0BSD", "OR", "GPL-2.0"}},
		{"(0BSD OR GPL-2.0)", []string{"(", "0BSD", "OR", "GPL-2.0", ")"}},
		{"(A (B C))", []string{"(", "A", "(", "B", "C", ")", ")"}},
	} {
		i := 0
		scanner := spdx.NewScanner(bytes.NewBufferString(t.inp))
		for scanner.Scan() {
			c.Check(scanner.Text(), Equals, t.tokens[i])
			i++
		}
		c.Check(len(t.tokens), Equals, i)
		c.Check(scanner.Err(), IsNil)
	}
}
