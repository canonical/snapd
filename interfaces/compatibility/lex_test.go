// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package compatibility_test

import (
	"github.com/snapcore/snapd/interfaces/compatibility"
	. "gopkg.in/check.v1"
)

func (s *CompatSuite) TestLexValidExpressions(c *C) {
	for i, tc := range []struct {
		expression string
		tokens     []compatibility.Item
	}{
		{"", nil},
		{"   ", nil},
		{"  foo", []compatibility.Item{{compatibility.ItemString, "foo"}}},
		{"foo\n\t", []compatibility.Item{{compatibility.ItemString, "foo"}}},
		{"(foo)", []compatibility.Item{
			{compatibility.ItemLeftParen, ""},
			{compatibility.ItemString, "foo"},
			{compatibility.ItemRightParen, ""},
		}},
		{"bar AND(foo OR baz)", []compatibility.Item{
			{compatibility.ItemString, "bar"},
			{compatibility.ItemAND, ""},
			{compatibility.ItemLeftParen, ""},
			{compatibility.ItemString, "foo"},
			{compatibility.ItemOR, ""},
			{compatibility.ItemString, "baz"},
			{compatibility.ItemRightParen, ""},
		}},
		{")bar AND ( OR ) xx  ", []compatibility.Item{
			{compatibility.ItemRightParen, ""},
			{compatibility.ItemString, "bar"},
			{compatibility.ItemAND, ""},
			{compatibility.ItemLeftParen, ""},
			{compatibility.ItemOR, ""},
			{compatibility.ItemRightParen, ""},
			{compatibility.ItemString, "xx"},
		}},
	} {
		c.Logf("tc %d: %+v", i, tc)
		c.Check(compatibility.Items(tc.expression), DeepEquals, tc.tokens)
	}
}

func (s *CompatSuite) TestLexValidExpressionsDashes(c *C) {
	for i, tc := range []struct {
		expression string
		tokens     []compatibility.Item
	}{
		{"foo-bar-1", []compatibility.Item{
			{compatibility.ItemString, "foo"},
			{compatibility.ItemString, "bar"},
			{compatibility.ItemInteger, "1"},
		}},
		{"foo-(1..4)-bar-(8..22)", []compatibility.Item{
			{compatibility.ItemString, "foo"},
			{compatibility.ItemRangeLeftInt, "1"},
			{compatibility.ItemRangeRightInt, "4"},
			{compatibility.ItemString, "bar"},
			{compatibility.ItemRangeLeftInt, "8"},
			{compatibility.ItemRangeRightInt, "22"},
		}},
		{"(foo-2-0)", []compatibility.Item{
			{compatibility.ItemLeftParen, ""},
			{compatibility.ItemString, "foo"},
			{compatibility.ItemInteger, "2"},
			{compatibility.ItemInteger, "0"},
			{compatibility.ItemRightParen, ""},
		}},
		{"(foo-2-(3..7))", []compatibility.Item{
			{compatibility.ItemLeftParen, ""},
			{compatibility.ItemString, "foo"},
			{compatibility.ItemInteger, "2"},
			{compatibility.ItemRangeLeftInt, "3"},
			{compatibility.ItemRangeRightInt, "7"},
			{compatibility.ItemRightParen, ""},
		}},
		{" foo-2-(3..7) AND (b1a2r3 OR boo-(5..9)-7-(1..33))", []compatibility.Item{
			{compatibility.ItemString, "foo"},
			{compatibility.ItemInteger, "2"},
			{compatibility.ItemRangeLeftInt, "3"},
			{compatibility.ItemRangeRightInt, "7"},
			{compatibility.ItemAND, ""},
			{compatibility.ItemLeftParen, ""},
			{compatibility.ItemString, "b1a2r3"},
			{compatibility.ItemOR, ""},
			{compatibility.ItemString, "boo"},
			{compatibility.ItemRangeLeftInt, "5"},
			{compatibility.ItemRangeRightInt, "9"},
			{compatibility.ItemInteger, "7"},
			{compatibility.ItemRangeLeftInt, "1"},
			{compatibility.ItemRangeRightInt, "33"},
			{compatibility.ItemRightParen, ""},
		}},
	} {
		c.Logf("tc %d: %+v", i, tc)
		c.Check(compatibility.Items(tc.expression), DeepEquals, tc.tokens)
	}
}

func (s *CompatSuite) TestLexInvalidExpressions(c *C) {
	for i, tc := range []struct {
		expression string
		err        string
	}{
		{" _foo", "unexpected rune: _"},
		{"foo AN", "unexpected rune: A"},
		{"fooX", "unexpected rune after string: X"},
		{"foo OR xx (ANDb", "unexpected rune after AND: b"},
	} {
		c.Logf("tc %d: %+v", i, tc)
		tokens := compatibility.Items(tc.expression)
		c.Check(len(tokens) > 0, Equals, true)
		c.Check(tokens[len(tokens)-1], Equals,
			compatibility.Item{compatibility.ItemError, tc.err})
	}
}

func (s *CompatSuite) TestLexInvalidExpressionsDashes(c *C) {
	for i, tc := range []struct {
		expression string
		err        string
	}{
		{"foo-", "no rune after dash"},
		{"foo-(", "no range left value after ("},
		{"foo-(2", "no .. after range left value"},
		{"foo-(2..", "no range right value after .."},
		{"foo-(2..3", "no ) after range right value"},
		{"foo-(a..b)", "not an integer: a"},
		{"foo-)", "unexpected rune after dash: )"},
		{"foo-(2..x)", "not an integer: x"},
		{"foo-01", "integers not allowed to start with 0: 01"},
		{"foo-123456789", "integer with more than 8 digits: 123456789"},
	} {
		c.Logf("tc %d: %+v", i, tc)
		tokens := compatibility.Items(tc.expression)
		c.Check(len(tokens) > 0, Equals, true)
		c.Check(tokens[len(tokens)-1], Equals,
			compatibility.Item{compatibility.ItemError, tc.err})
	}
}
