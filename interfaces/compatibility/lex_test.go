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

// TestItemString
func (s *CompatSuite) TestItemString(c *C) {
	for i, tc := range []struct {
		item compatibility.Item
		str  string
	}{
		{compatibility.Item{Typ: compatibility.ItemError, ErrMsg: "tokenizer error"},
			`"error: tokenizer error"`},
		{compatibility.Item{Typ: compatibility.ItemError, ErrMsg: "very long tokenizer error"},
			`"error: very long tokenizer ..."`},
		{compatibility.Item{Typ: compatibility.ItemLabel,
			Label: compatibility.CompatField{[]compatibility.CompatDimension{
				{"foo", []compatibility.CompatRange{{0, 0}}}}}},
			`"label: foo-0"`},
		{compatibility.Item{Typ: compatibility.ItemLabel,
			Label: compatibility.CompatField{[]compatibility.CompatDimension{
				{"foo01234567890123456789", []compatibility.CompatRange{{0, 0}}}}}},
			`"label: foo01234567890123456..."`},
		{compatibility.Item{Typ: compatibility.ItemOR}, `"OR"`},
	} {
		c.Logf("tc %d: %+v", i, tc)
		c.Check(tc.item.String(), Equals, tc.str)
	}
}

func (s *CompatSuite) TestLexValidExpressions(c *C) {
	for i, tc := range []struct {
		expression string
		tokens     []compatibility.Item
	}{
		{"", nil},
		{"   ", nil},
		{"  foo", []compatibility.Item{
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"foo", []compatibility.CompatRange{{0, 0}}}}}},
		}},
		{"foo\n\t", []compatibility.Item{
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"foo", []compatibility.CompatRange{{0, 0}}}}}},
		}},
		{"(foo)", []compatibility.Item{
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"foo", []compatibility.CompatRange{{0, 0}}}}}},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
		}},
		{"bar AND(foo OR baz)", []compatibility.Item{
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"bar", []compatibility.CompatRange{{0, 0}}}}}},
			{compatibility.ItemAND, "", compatibility.CompatField{}},
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"foo", []compatibility.CompatRange{{0, 0}}}}}},
			{compatibility.ItemOR, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"baz", []compatibility.CompatRange{{0, 0}}}}}},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
		}},
		{")bar AND ( OR ) xx  ", []compatibility.Item{
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"bar", []compatibility.CompatRange{{0, 0}}}}}},
			{compatibility.ItemAND, "", compatibility.CompatField{}},
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemOR, "", compatibility.CompatField{}},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{[]compatibility.CompatDimension{
					{"xx", []compatibility.CompatRange{{0, 0}}}}}},
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
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{0, 0}}},
						{"bar", []compatibility.CompatRange{{1, 1}}},
					}},
			},
		}},
		{"foo-(1..4)-bar-(8..22)", []compatibility.Item{
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{1, 4}}},
						{"bar", []compatibility.CompatRange{{8, 22}}},
					}},
			},
		}},
		{"(foo-2-0)", []compatibility.Item{
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{2, 2}, {0, 0}}},
					}},
			},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
		}},
		{"( foo )", []compatibility.Item{
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{0, 0}}},
					}},
			},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
		}},
		{"(foo)", []compatibility.Item{
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{0, 0}}},
					}},
			},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
		}},
		{"(foo-2-(3..7))", []compatibility.Item{
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{2, 2}, {3, 7}}},
					}},
			},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
		}},
		{" foo-2-(3..7) AND (b1a2r3 OR boo-(5..9)-7-(1..33))", []compatibility.Item{
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"foo", []compatibility.CompatRange{{2, 2}, {3, 7}}},
					}},
			},
			{compatibility.ItemAND, "", compatibility.CompatField{}},
			{compatibility.ItemLeftParen, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"b1a2r3", []compatibility.CompatRange{{0, 0}}},
					}},
			},
			{compatibility.ItemOR, "", compatibility.CompatField{}},
			{compatibility.ItemLabel, "",
				compatibility.CompatField{
					[]compatibility.CompatDimension{
						{"boo", []compatibility.CompatRange{{5, 9}, {7, 7}, {1, 33}}},
					}},
			},
			{compatibility.ItemRightParen, "", compatibility.CompatField{}},
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
		{"fooX", "unexpected rune: X"},
		{"foo-1#", "unexpected rune: #"},
		{"foo-(1..2)_", "unexpected rune: _"},
		{"foo-(13__15)", "no dots in integer range"},
		{"foo-(13..15#", "range missing closing parenthesis"},
	} {
		c.Logf("tc %d: %+v", i, tc)
		tokens := compatibility.Items(tc.expression)
		c.Check(len(tokens) > 0, Equals, true)
		c.Check(tokens[len(tokens)-1], DeepEquals,
			compatibility.Item{compatibility.ItemError, tc.err, compatibility.CompatField{}})
	}
}

func (s *CompatSuite) TestLexInvalidExpressionsDashes(c *C) {
	for i, tc := range []struct {
		expression string
		err        string
	}{
		{"foo-", "unexpected character after hyphen: EOF"},
		{"foo-(", "not an integer: EOF"},
		{"foo-(2", "no dots in integer range"},
		{"foo-(2..", "not an integer: EOF"},
		{"foo-(2..3", "range missing closing parenthesis"},
		{"foo-(a..b)", "not an integer: a"},
		{"foo-)", "unexpected character after hyphen: )"},
		{"foo-(2..x)", "not an integer: x"},
		{"foo-01", "integers not allowed to start with 0: 01"},
		{"foo-123456789", "integer with more than 8 digits: 123456789"},
	} {
		c.Logf("tc %d: %+v", i, tc)
		tokens := compatibility.Items(tc.expression)
		c.Check(len(tokens) > 0, Equals, true)
		c.Check(tokens[len(tokens)-1], DeepEquals,
			compatibility.Item{compatibility.ItemError, tc.err, compatibility.CompatField{}})
	}
}
