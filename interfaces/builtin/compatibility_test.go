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

package builtin_test

import (
	"math"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/testutil"
)

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
	for i, tc := range []struct {
		compat string
		cfield *builtin.CompatField
	}{
		{"foo", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{0, 0}}}}}},
		{"foo-0", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{0, 0}}}}}},
		{"foo-4", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{4, 4}}}}}},
		{"foo-4-(1..3)", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{4, 4}, {1, 3}}}}}},
		{"foo-(1..3)-4", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo", Values: []builtin.CompatRange{{1, 3}, {4, 4}}}}}},
		{"foo3e4-4-(1..3)-bar-2025-8-2", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo3e4", Values: []builtin.CompatRange{{4, 4}, {1, 3}}},
			{Tag: "bar", Values: []builtin.CompatRange{{2025, 2025}, {8, 8}, {2, 2}}},
		}}},
		{"foo3e4-4-(1..3)-bar-2025-8-2-libxxlongname-56-(4..90)", &builtin.CompatField{Dimensions: []builtin.CompatDimension{
			{Tag: "foo3e4", Values: []builtin.CompatRange{{4, 4}, {1, 3}}},
			{Tag: "bar", Values: []builtin.CompatRange{{2025, 2025}, {8, 8}, {2, 2}}},
			{Tag: "libxxlongname", Values: []builtin.CompatRange{{56, 56}, {4, 90}}},
		}}},
	} {
		c.Logf("tc %d: %+v", i, tc)

		cfield, err := builtin.DecodeCompatField(tc.compat, nil)
		c.Assert(err, IsNil)
		c.Check(cfield, DeepEquals, tc.cfield)
	}
}

func (s *CompatSuite) TestValidCompatFieldsWithSpec(c *C) {
	for i, tc := range []struct {
		compat string
		cfield *builtin.CompatField
		spec   *builtin.CompatSpec
	}{
		{"foo",
			&builtin.CompatField{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{0, 0}}}}},
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{0, math.MaxUint}}}}},
		},
		{"foo-2",
			&builtin.CompatField{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{2, 2}}}}},
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{0, 3}}}}},
		},
		{"foo-(2..6)",
			&builtin.CompatField{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{2, 6}}}}},
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{2, 6}}}}},
		},
		{"foo-(2..6)",
			&builtin.CompatField{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{2, 6}}}}},
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 10}}}}},
		},
		{"foo-(2..6)-bar-7",
			&builtin.CompatField{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{2, 6}}},
				{Tag: "bar", Values: []builtin.CompatRange{{7, 7}}},
			}},
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 10}}},
				{Tag: "bar", Values: []builtin.CompatRange{{7, 10}}},
			}},
		},
	} {
		c.Logf("tc %d: %+v", i, tc)

		cfield, err := builtin.DecodeCompatField(tc.compat, nil)
		c.Assert(err, IsNil)
		c.Check(cfield, DeepEquals, tc.cfield)
	}
}

func (s *CompatSuite) TestInvalidCompatFields(c *C) {
	for i, tc := range []struct {
		compat string
		err    string
	}{
		{"", `compatibility label "": bad string ""`},
		{"3foo", `compatibility label "3foo": bad string "3foo"`},
		{"-foo", `compatibility label "-foo": bad string ""`},
		{"foo-", `compatibility label "foo-": "" is not a valid string`},
		{"foo-2-foo-5", `compatibility label "foo-2-foo-5": string "foo" appears more than once`},
		{"foo-(0..0)-foo-5-other", `compatibility label "foo-(0..0)-foo-5-other": string "foo" appears more than once`},
		{"foo-01", `compatibility label "foo-01": "01" is not a valid string`},
		{"foo-(01..5)", `compatibility label "foo-(01..5)": "(01..5)" is not a valid string`},
		{"foo-(1..05)", `compatibility label "foo-(1..05)": "(1..05)" is not a valid string`},
		{"foo-bar-", `compatibility label "foo-bar-": "" is not a valid string`},
		// More than 32 characters in tag
		{"a12345678901234567890123456789012", `compatibility label "a12345678901234567890123456789012": bad string "a12345678901234567890123456789012"`},
		{"fooBar", `compatibility label "fooBar": bad string "fooBar"`},
		// More than 8 digits
		{"foo-012345678", `compatibility label "foo-012345678": "012345678" is not a valid string`},
		{"foo-(10..012345678)", `compatibility label "foo-(10..012345678)": "(10..012345678)" is not a valid string`},
		{"foo-bar-baz-other", `compatibility label "foo-bar-baz-other": only 3 strings allowed`},
		{"foo-1-2-3-4", `compatibility label "foo-1-2-3-4": only 3 integer/integer ranges allowed per string`},
		{"bar-3-(2..1)", `compatibility label "bar-3-(2..1)": invalid range "(2..1)"`},
		{"bar-3-(2 ..99)", `compatibility label "bar-3-(2 ..99)": "(2 ..99)" is not a valid string`},
	} {
		c.Logf("tc %d: %+v", i, tc)

		cfield, err := builtin.DecodeCompatField(tc.compat, nil)
		c.Check(cfield, IsNil)
		c.Assert(err, NotNil)
		c.Check(err.Error(), Equals, tc.err)
	}
}

func (s *CompatSuite) TestInvalidCompatFieldsWithSpec(c *C) {
	for i, tc := range []struct {
		compat string
		spec   *builtin.CompatSpec
		err    string
	}{
		{"foo",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "bar", Values: []builtin.CompatRange{{1, 10}}}}},
			`compatibility label "foo": string does not match interface spec (foo != bar)`},
		{"foo-3-bar",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 10}}}}},
			`compatibility label "foo-3-bar": unexpected number of strings (should be 1)`},
		{"foo-3-8",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 10}}}}},
			`compatibility label "foo-3-8": unexpected number of integers (should be 1 for "foo")`},
		{"foo-(0..3)",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 5}}}}},
			`compatibility label "foo-(0..3)": range (0..3) is not included in valid range (1..5)`},
		{"foo-(3..6)",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 5}}}}},
			`compatibility label "foo-(3..6)": range (3..6) is not included in valid range (1..5)`},
		{"foo-(3..6)",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{0, 2}}}}},
			`compatibility label "foo-(3..6)": range (3..6) is not included in valid range (0..2)`},
		{"foo-(3..6)",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{7, 10}}}}},
			`compatibility label "foo-(3..6)": range (3..6) is not included in valid range (7..10)`},
		{"foo",
			&builtin.CompatSpec{Dimensions: []builtin.CompatDimension{
				{Tag: "foo", Values: []builtin.CompatRange{{1, 10}}}}},
			`compatibility label "foo": range (0..0) is not included in valid range (1..10)`},
	} {
		c.Logf("tc %d: %+v", i, tc)

		cfield, err := builtin.DecodeCompatField(tc.compat, tc.spec)
		c.Check(cfield, IsNil)
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

		field1, err := builtin.DecodeCompatField(tc.compat1, nil)
		c.Assert(err, IsNil)
		field2, err := builtin.DecodeCompatField(tc.compat2, nil)
		c.Assert(err, IsNil)
		c.Check(builtin.CheckCompatibility(*field1, *field2), Equals, tc.result)
		// Must be symmetric
		c.Check(builtin.CheckCompatibility(*field2, *field1), Equals, tc.result)
	}
}
