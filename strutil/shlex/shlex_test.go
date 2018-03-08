// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package shlex_test

import (
	"testing"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil/shlex"
)

func Test(t *testing.T) { check.TestingT(t) }

type shlexSuite struct{}

var _ = check.Suite(&shlexSuite{})

func (shlexSuite) TestSimple(c *check.C) {

	for i, tc := range []struct {
		in  string
		out []string
		err string
	}{{
		in:  ``,
		out: []string{},
	}, {
		in:  `       `,
		out: []string{},
	}, {
		in:  `--foo --bar --baz`,
		out: []string{"--foo", "--bar", "--baz"},
	}, {
		in:  `   ""  `,
		out: []string{""},
	}, {
		in:  `--foo ""`,
		out: []string{"--foo", ""},
	}, {
		in:  `--foo="" --bar='' --baz ""`,
		out: []string{"--foo=", "--bar=", "--baz", ""},
	}, {
		in:  `--foo="a b c"      -a "FOO' BAR BAZ"`,
		out: []string{"--foo=a b c", "-a", "FOO' BAR BAZ"},
	}, {
		in:  `foo "a b c" 'd e f'`,
		out: []string{"foo", "a b c", "d e f"},
	}, {
		in: `"foo "bar`,
		out: []string{
			"foo bar",
		},
	}, {
		in:  `"foo "bar\`,
		err: "no escaped character",
	}, {
		//
		in:  `"foo "bar baz"`,
		err: "no closing quotation",
	}, {
		//
		in:  `foo\ baz"`,
		err: "no closing quotation",
	}, {
		//
		in:  `"foo "bar baz" bar'`,
		err: "no closing quotation",
	}, {
		in:  `\`,
		err: "no escaped character",
	}, {
		in:  ` "foo`,
		err: "no closing quotation",
	}} {
		c.Logf("trying TC %v: %q", i, tc.in)
		split, err := shlex.SplitLine(tc.in)
		if tc.err != "" {
			c.Check(err, check.ErrorMatches, tc.err)
			c.Check(split, check.IsNil)
		} else {
			c.Assert(err, check.IsNil)
			c.Assert(split, check.DeepEquals, tc.out)
		}
	}
}
