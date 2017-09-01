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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/spdx"
)

func Test(t *testing.T) { TestingT(t) }

type spdxSuite struct{}

var _ = Suite(&spdxSuite{})

func (s *spdxSuite) TestParseHappy(c *C) {
	for _, t := range []string{
		"GPL-2.0",
		"GPL-2.0+",
		"GPL-2.0 AND BSD-2-Clause",
		"GPL-2.0 OR BSD-2-Clause",
		"GPL-2.0 WITH GCC-exception-3.1",
		"(GPL-2.0 AND BSD-2-Clause)",
		"GPL-2.0 AND (BSD-2-Clause OR 0BSD)",
		"GPL-2.0 AND (BSD-2-Clause OR 0BSD) WITH GCC-exception-3.1",
		"((GPL-2.0 AND (BSD-2-Clause OR 0BSD)) OR GPL-3.0) ",
	} {
		err := spdx.ValidateLicense(t)
		c.Check(err, IsNil, Commentf("input: %q", t))
	}
}

func (s *spdxSuite) TestParseError(c *C) {
	for _, t := range []struct {
		inp    string
		errStr string
	}{
		{"FOO", `unknown license: FOO`},
		{"GPL-2.0 GPL-3.0", `unexpected token: "GPL-3.0"`},
		{"(GPL-2.0))", "unbalanced parenthesis"},
		{"(GPL-2.0", `expected closing parenthesis got ""`},
		{"OR", "expected left license with operator OR"},
		{"OR GPL-2.0", "expected left license with operator OR"},
		{"GPL-2.0 OR", "expected right license with operator OR"},
		{"GPL-2.0 WITH BAR", "unknown license exception: BAR"},
		{"GPL-2.0 WITH (foo)", `unknown license exception: foo`},
	} {
		err := spdx.ValidateLicense(t.inp)
		c.Check(err, ErrorMatches, t.errStr, Commentf("input: %q", t.inp))
	}
}
