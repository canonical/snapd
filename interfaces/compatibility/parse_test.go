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

func (s *CompatSuite) TestOperatorString(c *C) {
	var exp compatibility.Expression
	op := &compatibility.Operator{Oper: compatibility.Item{compatibility.ItemOR, "", compatibility.CompatField{}}}
	exp = op
	c.Check(exp.String(), Equals, `"OR"`)
	op = &compatibility.Operator{Oper: compatibility.Item{compatibility.ItemAND, "", compatibility.CompatField{}}}
	exp = op
	c.Check(exp.String(), Equals, `"AND"`)
}

func (s *CompatSuite) TestParserValidExpressions(c *C) {
	for i, tc := range []struct {
		expression string
		tree       *compatibility.Node
		labels     []compatibility.CompatField
	}{
		{"foo",
			&compatibility.Node{Exp: &compatibility.CompatField{
				[]compatibility.CompatDimension{{"foo", []compatibility.CompatRange{{0, 0}}}}}},
			[]compatibility.CompatField{{Dimensions: []compatibility.CompatDimension{{Tag: "foo", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}}}}},
		},
		{"foo-7-4 OR bar",
			&compatibility.Node{
				Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemOR, "", compatibility.CompatField{}}},
				Left: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "foo", Values: []compatibility.CompatRange{{7, 7}, {4, 4}}}},
					},
				},
				Right: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "bar", Values: []compatibility.CompatRange{{0, 0}}}},
					},
				},
			},
			[]compatibility.CompatField{
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "foo", Values: []compatibility.CompatRange{{Min: 7, Max: 7}, {Min: 4, Max: 4}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "bar", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
			},
		},
		{"((foo AND bar-(1..10)))",
			&compatibility.Node{
				Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemAND, "", compatibility.CompatField{}}},
				Left: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "foo", Values: []compatibility.CompatRange{{0, 0}}}},
					},
				},
				Right: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "bar", Values: []compatibility.CompatRange{{1, 10}}}},
					},
				},
			},
			[]compatibility.CompatField{
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "foo", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "bar", Values: []compatibility.CompatRange{{Min: 1, Max: 10}}},
				}},
			},
		},
		{"(foo AND bar) OR baz-(5..6)-(5..56)",
			&compatibility.Node{
				Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemOR, "", compatibility.CompatField{}}},
				Left: &compatibility.Node{
					Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemAND, "", compatibility.CompatField{}}},
					Left: &compatibility.Node{
						Exp: &compatibility.CompatField{
							Dimensions: []compatibility.CompatDimension{
								{Tag: "foo", Values: []compatibility.CompatRange{{0, 0}}}},
						},
					},
					Right: &compatibility.Node{
						Exp: &compatibility.CompatField{
							Dimensions: []compatibility.CompatDimension{
								{Tag: "bar", Values: []compatibility.CompatRange{{0, 0}}}},
						},
					}},
				Right: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "baz", Values: []compatibility.CompatRange{{5, 6}, {5, 56}}}},
					},
				},
			},
			[]compatibility.CompatField{
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "foo", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "bar", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "baz", Values: []compatibility.CompatRange{{Min: 5, Max: 6}, {Min: 5, Max: 56}}},
				}},
			},
		},
		{"foo-0-blah-5 AND (bar OR baz)",
			&compatibility.Node{
				Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemAND, "", compatibility.CompatField{}}},
				Left: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "foo", Values: []compatibility.CompatRange{{0, 0}}},
							{Tag: "blah", Values: []compatibility.CompatRange{{5, 5}}},
						},
					},
				},
				Right: &compatibility.Node{
					Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemOR, "", compatibility.CompatField{}}},
					Left: &compatibility.Node{
						Exp: &compatibility.CompatField{
							Dimensions: []compatibility.CompatDimension{
								{Tag: "bar", Values: []compatibility.CompatRange{{0, 0}}}},
						},
					},
					Right: &compatibility.Node{
						Exp: &compatibility.CompatField{
							Dimensions: []compatibility.CompatDimension{
								{Tag: "baz", Values: []compatibility.CompatRange{{0, 0}}}},
						},
					}},
			},
			[]compatibility.CompatField{
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "foo", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
					{Tag: "blah", Values: []compatibility.CompatRange{{Min: 5, Max: 5}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "bar", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "baz", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
			},
		},
		{"foo OR bar OR baz",
			&compatibility.Node{
				Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemOR, "", compatibility.CompatField{}}},
				Left: &compatibility.Node{
					Exp: &compatibility.Operator{compatibility.Item{compatibility.ItemOR, "", compatibility.CompatField{}}},
					Left: &compatibility.Node{
						Exp: &compatibility.CompatField{
							Dimensions: []compatibility.CompatDimension{
								{Tag: "foo", Values: []compatibility.CompatRange{{0, 0}}}},
						},
					},
					Right: &compatibility.Node{
						Exp: &compatibility.CompatField{
							Dimensions: []compatibility.CompatDimension{
								{Tag: "bar", Values: []compatibility.CompatRange{{0, 0}}}},
						},
					}},
				Right: &compatibility.Node{
					Exp: &compatibility.CompatField{
						Dimensions: []compatibility.CompatDimension{
							{Tag: "baz", Values: []compatibility.CompatRange{{0, 0}}}},
					},
				},
			},
			[]compatibility.CompatField{
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "foo", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "bar", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
				{Dimensions: []compatibility.CompatDimension{
					{Tag: "baz", Values: []compatibility.CompatRange{{Min: 0, Max: 0}}},
				}},
			},
		},
	} {
		c.Logf("tc %d: %+v", i, tc)
		node, labels, err := compatibility.Parse(tc.expression)
		c.Check(err, IsNil)
		c.Check(node, DeepEquals, tc.tree)
		c.Check(labels, DeepEquals, tc.labels)
	}
}

func (s *CompatSuite) TestParserInvalidExpressions(c *C) {
	for i, tc := range []struct {
		expression string
		errMsg     string
	}{
		{"", "empty compatibility string"},
		{"()", `unexpected token "\)"`},
		{"(foo", `expected right parenthesis, found "EOF"`},
		{"foo)", `unexpected string at the end: "\)"`},
		{"foo(", `unexpected token "\("`},
		{"foo (", `unexpected token "\("`},
		{"foo bar", `unexpected token "label: bar-0"`},
		{"foo OR OR bar", `unexpected token "OR"`},
		{"AND", `unexpected token "AND"`},
		{"OR", `unexpected token "OR"`},
		{"foo OR", `unexpected token "EOF"`},
		{"OR foo", `unexpected token "OR"`},
		{"(OR foo)", `unexpected token "OR"`},
		{"foo-", "while parsing: unexpected character after hyphen: EOF"},
		{"foo-1-foo-2", "while parsing: repeated string in label: foo"},
		{"a12345678901234567890123456789012", "while parsing: string is longer than 32 characters: a12345678901234567890123456789012"},
		{"foo-(1..)", `while parsing: not an integer: \)`},
		{"foo-(55..1)", `while parsing: negative range specified: \(55\.\.1\)`},
		{"foo OR bar AND baz", `unexpected item after OR expression: "AND"`},
		{"blah AND (foo OR bar AND baz)", `unexpected item after OR expression: "AND"`},
		{"(blah AND foo OR bar) AND baz", `unexpected item after AND expression: "OR"`},
		{"foo OR xx (", `unexpected item after OR expression: "\("`},
		{"AND)", `unexpected token "AND"`},
		{"OR)", `unexpected token "OR"`},
		{"foo(bar)", `unexpected token "\("`},
		{"foo-1(bar)", `unexpected token "\("`},
		{"foo-(1..2)(", `unexpected token "\("`},
	} {
		c.Logf("tc %d: %+v", i, tc)
		node, labels, err := compatibility.Parse(tc.expression)
		c.Check(err, ErrorMatches, tc.errMsg)
		c.Check(node, IsNil)
		c.Check(len(labels), Equals, 0)
	}
}
