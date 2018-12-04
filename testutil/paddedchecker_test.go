// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2018 Canonical Ltd
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

package testutil_test

import (
	"errors"

	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type paddedCheckerSuite struct{}

var _ = check.Suite(&paddedCheckerSuite{})

func (*paddedCheckerSuite) TestPaddedChecker(c *check.C) {
	type row struct {
		lhs     string
		checker check.Checker
		rhs     string
	}

	table := []row{
		{" a  b\tc", EqualsPadded, "a b c"},
		{" a  b\nc", check.Not(EqualsPadded), "a b c"},

		{" a  b\tc", EqualsWrapped, "a b c"},
		{" a  b\nc", EqualsWrapped, "a b c"},

		{" a  b\tc    d\t\te", ContainsPadded, "b c d"},
		{" a  b\nc    d\t\te", check.Not(ContainsPadded), "b c d"},

		{" a  b\tc    d\t\te", ContainsWrapped, "b c d"},
		{" a  b\nc    d\t\te", ContainsWrapped, "b c d"},

		{"\tfoo baah ", MatchesPadded, `fo+ b\S+`},
		{"\tfoo\nbaah ", check.Not(MatchesPadded), `fo+ b\S+`},

		{"\tfoo baah ", MatchesWrapped, `fo+ b\S+`},
		{"\tfoo\nbaah ", MatchesWrapped, `fo+ b\S+`},
	}

	for i, test := range table {
		for _, lhs := range []interface{}{test.lhs, []byte(test.lhs), errors.New(test.lhs)} {
			for _, rhs := range []interface{}{test.rhs, []byte(test.rhs)} {
				comm := check.Commentf("%d:%s:%T/%T", i, test.checker.Info().Name, lhs, rhs)
				c.Check(lhs, test.checker, rhs, comm)
			}
		}
	}

	for _, checker := range []check.Checker{EqualsPadded, EqualsWrapped, ContainsPadded, ContainsWrapped, MatchesPadded, MatchesWrapped} {
		testCheck(c, checker, false, "right-hand value must be a string or []byte", "a b c", 42)
		testCheck(c, checker, false, "left-hand value must be a string or []byte or error", 42, "a b c")
	}
	for _, checker := range []check.Checker{MatchesPadded, MatchesWrapped} {
		testCheck(c, checker, false, "right-hand value must be a valid regexp: error parsing regexp: missing argument to repetition operator: `+`", "a b c", "+a b c")
	}
}
