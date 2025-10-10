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
	"math"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/compatibility"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type CompatSuite struct {
	testutil.BaseTest
}

var _ = Suite(&CompatSuite{})

func (s *CompatSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *CompatSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *CompatSuite) TestValidCompatFields(c *C) {
	for i, tc := range []string{
		"foo",
		"foo-0",
		"foo-4",
		"foo-4-(1..3)",
		"foo-(1..3)-4",
		"foo3e4-4-(1..3)-bar-2025-8-2",
		"foo3e4-4-(1..3)-bar-2025-8-2-libxxlongname-56-(4..90)",
		"(foo-3-4 OR bar-1-1-baz) AND boo32",
		"(foo-3-4 AND bar-1-1-baz) OR boo32",
	} {
		c.Logf("tc %d: %+v", i, tc)

		err := compatibility.IsValidExpression(tc, nil)
		c.Assert(err, IsNil)
	}
}

func (s *CompatSuite) TestValidCompatFieldsWithSpec(c *C) {
	for i, tc := range []struct {
		compat string
		spec   *compatibility.CompatSpec
	}{
		{"foo",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{0, math.MaxUint}}}}},
		},
		{"foo-2",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{0, 3}}}}},
		},
		{"foo-(2..6)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{2, 6}}}}},
		},
		{"foo-(2..6)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
		},
		{"foo-(2..6)-bar-7",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}},
				{Tag: "bar", Values: []compatibility.CompatRange{{7, 10}}},
			}},
		},
		{"foo-(2..6) AND foo-1 AND foo-10",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
		},
		{"foo-(2..6) AND (foo-1 OR foo-10 OR foo-(2..10))",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
		},
	} {
		c.Logf("tc %d: %+v", i, tc)

		err := compatibility.IsValidExpression(tc.compat, tc.spec)
		c.Assert(err, IsNil)
	}
}

func (s *CompatSuite) TestInvalidCompatFields(c *C) {
	for i, tc := range []struct {
		compat string
		err    string
	}{
		{"", `compatibility label "": empty compatibility string`},
		{"3foo", `compatibility label "3foo": while parsing: unexpected rune: 3`},
		{"-foo", `compatibility label "-foo": while parsing: unexpected rune: -`},
		{"foo-", `compatibility label "foo-": while parsing: unexpected character after hyphen: EOF`},
		{"foo-2-foo-5", `compatibility label "foo-2-foo-5": while parsing: repeated string in label: foo`},
		{"foo-(0..0)-foo-5-other", `compatibility label "foo-(0..0)-foo-5-other": while parsing: repeated string in label: foo`},
		{"foo-01", `compatibility label "foo-01": while parsing: integers not allowed to start with 0: 01`},
		{"foo-(01..5)", `compatibility label "foo-(01..5)": while parsing: integers not allowed to start with 0: 01`},
		{"foo-(1..05)", `compatibility label "foo-(1..05)": while parsing: integers not allowed to start with 0: 05`},
		{"foo-bar-", `compatibility label "foo-bar-": while parsing: unexpected character after hyphen: EOF`},
		// More than 32 characters in tag
		{"a12345678901234567890123456789012", `compatibility label "a12345678901234567890123456789012": while parsing: string is longer than 32 characters: a12345678901234567890123456789012`},
		{"fooBar", `compatibility label "fooBar": while parsing: unexpected rune: B`},
		// More than 8 digits
		{"foo-012345678", `compatibility label "foo-012345678": while parsing: integers not allowed to start with 0: 012345678`},
		{"foo-(10..012345678)", `compatibility label "foo-(10..012345678)": while parsing: integers not allowed to start with 0: 012345678`},
		{"foo-bar-baz-other", `compatibility label "foo-bar-baz-other": only 3 strings allowed`},
		{"foo-1-2-3-4", `compatibility label "foo-1-2-3-4": only 3 integer/integer ranges allowed per string`},
		{"bar-3-(2..1)", `compatibility label "bar-3-(2..1)": while parsing: negative range specified: (2..1)`},
		{"bar-3-(2 ..99)", `compatibility label "bar-3-(2 ..99)": while parsing: no dots in integer range`},
		{"foo-4 OR bar AND blah", `compatibility label "foo-4 OR bar AND blah": unexpected item after OR expression: "AND"`},
		{"foo-4 OR ", `compatibility label "foo-4 OR ": unexpected token "EOF"`},
		{" OR foo-4", `compatibility label " OR foo-4": unexpected token "OR"`},
		{"( OR foo-4)", `compatibility label "( OR foo-4)": unexpected token "OR"`},
	} {
		c.Logf("tc %d: %+v", i, tc)

		err := compatibility.IsValidExpression(tc.compat, nil)
		c.Assert(err, NotNil)
		c.Check(err.Error(), Equals, tc.err)
	}
}

func (s *CompatSuite) TestInvalidCompatFieldsWithSpec(c *C) {
	for i, tc := range []struct {
		compat string
		spec   *compatibility.CompatSpec
		err    string
	}{
		{"foo",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "bar", Values: []compatibility.CompatRange{{1, 10}}}}},
			`compatibility label "foo": string does not match interface spec (foo != bar)`},
		{"foo-3-bar",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
			`compatibility label "foo-3-bar": unexpected number of strings (should be 1)`},
		{"foo-3-8",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
			`compatibility label "foo-3-8": unexpected number of integers (should be 1 for "foo")`},
		{"foo-(0..3)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 5}}}}},
			`compatibility label "foo-(0..3)": range (0..3) is not included in valid range (1..5)`},
		{"foo-(3..6)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 5}}}}},
			`compatibility label "foo-(3..6)": range (3..6) is not included in valid range (1..5)`},
		{"foo-(3..6)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{0, 2}}}}},
			`compatibility label "foo-(3..6)": range (3..6) is not included in valid range (0..2)`},
		{"foo-(3..6)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{7, 10}}}}},
			`compatibility label "foo-(3..6)": range (3..6) is not included in valid range (7..10)`},
		{"foo",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
			`compatibility label "foo": range (0..0) is not included in valid range (1..10)`},
		{"(foo-1 OR foo-2) AND foo-(1..100)",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
			`compatibility label "(foo-1 OR foo-2) AND foo-(1..100)": range (1..100) is not included in valid range (1..10)`},
		{"(foo-1 OR foo-77) AND foo-1",
			&compatibility.CompatSpec{Dimensions: []compatibility.CompatDimension{
				{Tag: "foo", Values: []compatibility.CompatRange{{1, 10}}}}},
			`compatibility label "(foo-1 OR foo-77) AND foo-1": range (77..77) is not included in valid range (1..10)`},
	} {
		c.Logf("tc %d: %+v", i, tc)

		err := compatibility.IsValidExpression(tc.compat, tc.spec)
		c.Assert(err, NotNil)
		c.Check(err.Error(), Equals, tc.err)
	}
}

func (s *CompatSuite) TestCompatibility(c *C) {
	for i, tc := range []struct {
		compat1, compat2 string
		result           bool
	}{
		{"foo", "bar", false},
		{"foo", "foo", true},
		{"foo-bar", "foo-xxx", false},
		{"foo-bar", "foo", false},
		{"foo-bar", "bar-foo", false},
		{"foo-1-2", "foo-1", false},
		{"foo-2-bar-1", "foo-2-bar-1-2", false},
		{"foo-0", "foo", true},
		{"foo", "foo-(0..0)", true},
		{"foo-0", "foo-(0..0)", true},
		{"foo-1", "foo-(1..1)", true},
		{"foo-(1..1)", "foo-(1..1)", true},
		{"foo-1-2", "foo-1-2", true},
		{"foo-1-(2..3)", "foo-1-2", true},
		{"foo-1-(2..3)", "foo-1-3", true},
		{"foo-1-(2..3)", "foo-1-1", false},
		{"foo-1-(2..3)", "foo-1-4", false},
		{"foo-1-(2..5)-bar", "foo-1-4-bar", true},
		{"foo-1-(2..5)-bar", "foo-1-4-bar-0", true},
		{"foo-1-(2..5)-bar-5-6", "foo-1-4-bar-5-(4..10)", true},
		{"foo-1-(2..3)", "foo-1-(0..1)", false},
		{"foo-1-(2..3)", "foo-1-(0..2)", true},
		{"foo-1-(2..3)", "foo-1-(0..4)", true},
		{"foo-1-(2..3)", "foo-1-(3..4)", true},
		{"foo-1-(2..3)", "foo-1-(4..7)", false},
	} {
		c.Logf("tc %d: %+v", i, tc)

		c.Check(compatibility.CheckCompatibility(tc.compat1, tc.compat2), Equals, tc.result)
	}
}

func (s *CompatSuite) TestCompatibilityExpressions(c *C) {
	for i, tc := range []struct {
		compat1, compat2 string
		result           bool
	}{
		{"foo", "foo OR bar", true},
		{"foo", "foo AND bar", false},
		{"foo OR bar", "foo AND bar", true},
		{"foo AND bar", "foo AND bar", true},
		{"foo AND bar", "foo AND bar AND baz", false},
		{"foo OR bar", "foo AND bar AND baz", false},
		{"foo AND bar", "foo OR bar OR baz", true},
		{"(foo AND bar) OR (oof AND rab)", "foo OR bar", true},
		{"(foo AND bar) OR (oof AND rab)", "foo OR oof", false},
		{"(foo AND bar) OR (oof AND rab)", "foo AND oof", false},
		{"foo-3 OR bar-rab-6", "foo-(1..3) AND bar-rab-(6..99)", true},
		{"foo-3 OR bar-rab-6", "foo-(1..3) AND bar-rab-1", false},
		{"foo-0-blah-5 AND (bar OR baz)", "foo-blah-(4..5) OR baz", true},
		{"foo-0-blah-5 AND (bar OR baz)", "(foo-blah-(4..5) OR baz) AND boo", false},
		{"foo-0-blah-5 AND (bar OR baz)", "(foo-blah-(4..5) OR blah OR baz) AND (boo OR bar)", true},
	} {
		c.Logf("tc %d: %+v", i, tc)

		c.Check(compatibility.CheckCompatibility(tc.compat1, tc.compat2), Equals, tc.result)
	}
}
